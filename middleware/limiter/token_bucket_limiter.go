package limiter

import (
	"context"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"golang.org/x/time/rate"
)

/*
TokenBucketLimiter 实现令牌桶限流。

原理：桶以固定速率补充令牌，容量为 burst；每个请求消耗一个令牌，
令牌不足时拒绝或等待。相比窗口类算法，令牌桶既能长期平滑限流，
又允许在容量范围内出现短时突发，是最常用的服务端限流模型。

底层使用 golang.org/x/time/rate，它本身是并发安全的；
本类型额外维护 limit/burst 的原子副本，用于无锁地读取配置。
*/
type TokenBucketLimiter struct {
	// isEnabled 表示限流是否启用。
	isEnabled atomic.Bool
	// limit 为每秒补充的令牌数（atomic 副本）。
	limit atomic.Int64
	// burst 为桶容量（atomic 副本）。
	burst atomic.Int64
	// limiter 为真正执行令牌发放的底层限流器。
	limiter *rate.Limiter
}

// 编译期断言：令牌桶限流器必须满足 Limiter 接口。
var _ Limiter = (*TokenBucketLimiter)(nil)

/*
NewTokenBucketLimiter 创建令牌桶限流器。

参数：
  - limit：每秒补充的令牌数；小于等于 0 表示不限流；
  - burst：桶容量，即允许的最大突发量；小于 1 时按 1 处理。

限流器创建后即处于启用状态。
*/
func NewTokenBucketLimiter(limit, burst int) *TokenBucketLimiter {
	if burst < 1 {
		burst = 1
	}

	instance := &TokenBucketLimiter{
		limiter: newRateLimiter(limit, burst),
	}
	instance.limit.Store(int64(limit))
	instance.burst.Store(int64(burst))
	instance.Enable()

	return instance
}

// newRateLimiter 依据业务参数创建底层限流器。
//
// limit 小于等于 0 时使用 rate.Inf：语义为“不限流”，
// 而不是“一个令牌也不发”，避免调用方误传 0 导致服务整体不可用。
func newRateLimiter(limit, burst int) *rate.Limiter {
	if limit <= 0 {
		return rate.NewLimiter(rate.Inf, burst)
	}

	return rate.NewLimiter(rate.Limit(limit), burst)
}

// Enable 启用限流。
func (own *TokenBucketLimiter) Enable() {
	own.isEnabled.Store(true)
}

// Disable 临时禁用限流，中间件将直接放行。
func (own *TokenBucketLimiter) Disable() {
	own.isEnabled.Store(false)
}

// IsEnabled 返回限流是否启用。
func (own *TokenBucketLimiter) IsEnabled() bool {
	return own.isEnabled.Load()
}

// SetLimit 设置每秒补充的令牌数，小于等于 0 表示不限流。
func (own *TokenBucketLimiter) SetLimit(limit int) {
	if limit <= 0 {
		own.limit.Store(int64(limit))
		own.limiter.SetLimit(rate.Inf)
		return
	}

	own.limit.Store(int64(limit))
	own.limiter.SetLimit(rate.Limit(limit))
}

// SetBurst 设置桶容量，小于 1 时按 1 处理。
func (own *TokenBucketLimiter) SetBurst(burst int) {
	if burst < 1 {
		burst = 1
	}

	own.burst.Store(int64(burst))
	own.limiter.SetBurst(burst)
}

// GetLimit 返回每秒补充的令牌数。
func (own *TokenBucketLimiter) GetLimit() int {
	return int(own.limit.Load())
}

// GetBurst 返回桶容量。
func (own *TokenBucketLimiter) GetBurst() int {
	return int(own.burst.Load())
}

// GetConfig 返回当前配置。
func (own *TokenBucketLimiter) GetConfig() (limit, burst int) {
	return int(own.limit.Load()), int(own.burst.Load())
}

// GetMiddleware 返回令牌桶限流中间件。
//
// 参数 waitTimeout 语义与其它限流器一致：
// 0 表示立即失败，负数表示使用默认的 3 秒等待。
func (own *TokenBucketLimiter) GetMiddleware(waitTimeout time.Duration) leego.MiddlewareFunc {
	if waitTimeout < 0 {
		waitTimeout = _DEFAULT_WAIT_TIMEOUT
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			if !own.IsEnabled() {
				next(ctx)
				return
			}

			var allowed bool
			if waitTimeout == 0 {
				allowed = own.Allow()
			} else {
				allowed = own.AllowWithTimeout(ctx.Request.Context(), waitTimeout)
			}

			if !allowed {
				writeTooManyRequests(ctx)
				return
			}

			next(ctx)
		}
	}
}

// Allow 非阻塞地判断是否放行，放行时消耗一个令牌。
func (own *TokenBucketLimiter) Allow() bool {
	return own.limiter.Allow()
}

// AllowWithTimeout 在给定超时内等待令牌。
func (own *TokenBucketLimiter) AllowWithTimeout(ctx context.Context, timeout time.Duration) bool {
	if timeout <= 0 {
		return own.Allow()
	}

	if ctx == nil {
		ctx = context.Background()
	}

	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return own.limiter.Wait(waitContext) == nil
}

// Wait 阻塞等待令牌，直到放行或上下文结束。
func (own *TokenBucketLimiter) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	return own.limiter.Wait(ctx)
}
