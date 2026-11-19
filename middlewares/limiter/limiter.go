// Package limiter 提供三种常用的服务端限流算法实现。
//
// 三种算法各有取舍，可按业务特征选择：
//
//	固定窗口（FixedWindow）：实现最简单、开销最低，但窗口边界会出现
//	  “双倍突发”（相邻两个窗口的请求在边界处叠加）；
//	滑动窗口（SlidingWindow）：把窗口切成多个小桶，边界更平滑，
//	  代价是每个请求要做一次桶推进计算；
//	令牌桶（TokenBucket）：允许一定程度的突发（burst），
//	  并且可以长期平滑输出，是最贴近真实流量整形的模型。
//
// 三个限流器都实现了 Limiter 接口，并默认处于“启用”状态：
// 创建即可生效，需要临时放行时可调用 Disable，之后再 Enable。
package limiter

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
)

// 限流器公共错误定义。
var (
	// ErrNoDeadline 表示等待配额时传入的上下文没有截止时间。
	//
	// 没有截止时间的等待可能永久阻塞，因此在入口处直接拒绝。
	ErrNoDeadline = errors.New("limiter: context has no deadline")
)

// Limiter 定义限流器统一接口。
type Limiter interface {
	// Allow 非阻塞地判断本次请求是否放行，放行时立即消耗一个配额。
	Allow() bool
	// AllowWithTimeout 在给定超时内等待配额，超时或上下文取消时返回 false。
	AllowWithTimeout(ctx context.Context, timeout time.Duration) bool
	// Wait 阻塞等待配额，直到放行、上下文取消或超时（返回对应错误）。
	Wait(ctx context.Context) error
	// AdaptToMiddleware 将限流器适配为 leego.MiddlewareFunc。
	AdaptToMiddleware() leego.MiddlewareFunc
	// Enable 启用限流（创建时默认启用）。
	Enable()
	// Disable 临时禁用限流，使中间件直接放行。
	Disable()
	// IsEnabled 返回限流是否启用。
	IsEnabled() bool
}

// waitForQuota 在给定上下文内轮询 allow，直到放行或上下文结束。
//
// 采用“先尝试一次再轮询”的策略：绝大多数请求都能在第一次检查时直接通过，
// 只有真正触发限流的请求才会进入轮询分支，避免无谓的定时器开销。
func waitForQuota(ctx context.Context, allow func() bool) error {
	return waitForQuotaWith(ctx, allow, NewDefaultConfig())
}

// waitForQuotaWith 使用指定配置轮询等待配额。
//
// 与 waitForQuota 的差别只在轮询间隔：限流器把自己的 Config 传进来，
// 让“等多久检查一次”成为进程级可调参数，而不是写死的常量。
func waitForQuotaWith(ctx context.Context, allow func() bool, options Config) error {
	if allow() {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		return ErrNoDeadline
	}

	options = options.normalize()
	timer := time.NewTimer(options.PollInterval)
	for {
		select {
		case <-ctx.Done():
			// 显式停止定时器：上下文结束时可能仍有未消费的滴答，
			// 取出一次以允许运行时回收，避免遗留的 timer 引用上下文。
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			return ctx.Err()
		case <-timer.C:
			if allow() {
				return nil
			}

			timer.Reset(options.PollInterval)
		}
	}
}

// writeTooManyRequests 输出统一的 429 响应。
//
// 同时设置 Retry-After 头，便于客户端与网关确定重试时机。
func writeTooManyRequests(ctx *leego.Context) {
	ctx.SetResponseHeader("Retry-After", "1")
	ctx.ResponseError(http.StatusTooManyRequests, http.StatusText(http.StatusTooManyRequests))
}
