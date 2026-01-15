package limiter

import (
	"context"
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

var _ Limiter = (*FixedWindowLimiter)(nil)

// NewFixedWindowLimiter 新建一个固定窗口限流器（可自定义窗口大小）
func NewFixedWindowLimiter(limit int, size time.Duration) *FixedWindowLimiter {
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

// IsEnabled 检查是否启用
func (own *FixedWindowLimiter) IsEnabled() bool {
	return atomic.LoadInt32(&own.isEnabled) == 1
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

// GetConfig 获取配置
func (own *FixedWindowLimiter) GetConfig() (limit int, windowSize time.Duration) {
	own.lock.RLock()
	limit = own.limit
	windowSize = own.windowSize
	own.lock.RUnlock()
	return
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

// Allow 非阻塞，立即返回是否允许（10s超时）
func (own *FixedWindowLimiter) Allow() bool {
	return own.allow()
}

// Wait 阻塞，直到获取令牌或取消上下文
func (own *FixedWindowLimiter) Wait(ctx context.Context) error {
	// 先尝试一次
	if own.Allow() {
		return nil
	}

	// // 如果没有设置超时，直接返回
	// if _, ok := ctx.Deadline(); !ok {
	// 	return fmt.Errorf("limiter: not set context.Context timeout")
	// }

	// // 使用更高效的等待策略
	// waitTime := own.bucketSize / 2 // 等待半个桶的时间
	// for {
	// 	select {
	// 	case <-ctx.Done():
	// 		return ctx.Err()
	// 	case <-time.After(waitTime):
	// 		if own.Allow() {
	// 			return nil
	// 		}
	// 	}
	// }

	return nil
}

func (own *FixedWindowLimiter) allow() bool {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 检查是否需要重置窗口
	now := time.Now()
	if now.Sub(own.lastTime) >= own.windowSize {
		own.count = 0
		own.lastTime = now
	}

	// 检查是否超过限制
	if own.count >= own.limit {
		return false
	}

	own.count++
	return true
}
