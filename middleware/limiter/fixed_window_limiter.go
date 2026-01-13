package limiter

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// FixedWindowLimiter 定义固定窗口限流器的数据结构
type FixedWindowLimiter struct {
	count      int
	limit      int
	isEnabled  int32
	windowSize time.Duration
	lastTime   time.Time
	lock       sync.RWMutex
}

/*
NewDefaultFixedWindowLimiter 新建一个默认的固定窗口限流器
  - limit：限流阈值
  - 默认固定窗口大小为 1s
*/
func NewDefaultFixedWindowLimiter(limit int) *FixedWindowLimiter {
	return NewFixedWindowLimiterWithWindowSize(limit, time.Second)
}

// NewFixedWindowLimiterWithWindowSize 新建一个固定窗口限流器（可自定义窗口大小）
func NewFixedWindowLimiterWithWindowSize(limit int, size time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:      limit,
		windowSize: size,
		lastTime:   time.Now(),
	}
}

// Enable 启用中间件
func (own *FixedWindowLimiter) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用中间件
func (own *FixedWindowLimiter) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)
}

// SetLimit 设置限流阈值
func (own *FixedWindowLimiter) SetLimit(limit int) {
	own.lock.Lock()
	own.limit = limit
	own.lock.Unlock()
}

// SetWindowSize 设置窗口大小
func (own *FixedWindowLimiter) SetWindowSize(size time.Duration) {
	if size <= 0 {
		size = time.Second // 默认1秒
	}

	own.lock.Lock()
	own.count = 0
	own.lastTime = time.Now()
	own.windowSize = size
	own.lock.Unlock()
}

// GetMiddleware 获取中间件函数
func (own *FixedWindowLimiter) GetMiddleware() leego.MiddlewareFunc {
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 未启用，直接跳过
			if atomic.LoadInt32(&own.isEnabled) == 0 {
				next(ctx)
				return
			}

			// 请求过多，触发限流
			if !own.Allow() {
				ctx.ResponseError(http.StatusTooManyRequests, response.GetStatusLowerText(http.StatusTooManyRequests))
				return
			}

			// 调用下一个处理器
			next(ctx)
		}
	}
}

// Allow 检查是否允许通过
func (own *FixedWindowLimiter) Allow() bool {
	now := time.Now()

	own.lock.Lock()
	defer own.lock.Unlock()

	if now.Sub(own.lastTime) >= own.windowSize {
		own.count = 0
		own.lastTime = now
	}

	if own.count >= own.limit {
		return false
	}

	own.count++
	return true
}
