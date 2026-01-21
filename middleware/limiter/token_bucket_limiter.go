package limiter

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
	"golang.org/x/time/rate"
)

// TokenBucketLimiter 定义令牌桶限流器的结构类型
type TokenBucketLimiter struct {
	isEnabled int32
	limit     int32
	burst     int32
	limiter   *rate.Limiter
}

var _ Limiter = (*TokenBucketLimiter)(nil)

/*
NewTokenBucketLimiter 新建一个令牌桶限流器
  - limit: 每秒令牌数量
  - burst: 令牌最大容量
*/
func NewTokenBucketLimiter(limit, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{limiter: rate.NewLimiter(rate.Limit(limit), burst)}
}

// Enable 启用中间件
func (own *TokenBucketLimiter) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用中间件
func (own *TokenBucketLimiter) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)
}

// IsEnabled 检查是否启用
func (own *TokenBucketLimiter) IsEnabled() bool {
	return atomic.LoadInt32(&own.isEnabled) == 1
}

// SetLimit 设置每秒令牌数量
func (own *TokenBucketLimiter) SetLimit(limit int) {
	atomic.StoreInt32(&own.limit, int32(limit))
	own.limiter.SetLimit(rate.Limit(limit))
}

// SetBurst 设置令牌最大容量
func (own *TokenBucketLimiter) SetBurst(burst int) {
	atomic.StoreInt32(&own.burst, int32(burst))
	own.limiter.SetBurst(burst)
}

// GetConfig 获取限流器配置
func (own *TokenBucketLimiter) GetConfig() (limit, burst int) {
	l := atomic.LoadInt32(&own.limit)
	b := atomic.LoadInt32(&own.burst)
	return int(l), int(b)
}

// GetMiddleware 获取中间件函数
func (own *TokenBucketLimiter) GetMiddleware(timeout time.Duration) leego.MiddlewareFunc {
	if timeout < 0 {
		timeout = 3 * time.Second
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 未启用，直接跳过
			if atomic.LoadInt32(&own.isEnabled) == 0 {
				next(ctx)
				return
			}

			// 限流检查
			if !own.AllowWithTimeout(ctx.Request.Context(), timeout) {
				ctx.ResponseError(http.StatusTooManyRequests, response.GetStatusLowerText(http.StatusTooManyRequests))
				return
			}

			// 调用下一个处理器
			next(ctx)
		}
	}
}

// Allow 非阻塞的允许检查，立即返回是否允许
func (own *TokenBucketLimiter) Allow() bool {
	return own.limiter.Allow()
}

// AllowWithTimeout 带超时的允许检查
func (own *TokenBucketLimiter) AllowWithTimeout(ctx context.Context, timeout time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return own.Wait(timeoutCtx) == nil
}

// Wait 带上下文的允许检查，直到允许或取消上下文
func (own *TokenBucketLimiter) Wait(ctx context.Context) error {
	return own.limiter.Wait(ctx)
}
