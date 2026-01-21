package limiter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// FixedWindowLimiter 定义固定窗口限流器的数据结构
type FixedWindowLimiter struct {
	limit      int32
	count      int32
	isEnabled  int32
	windowSize time.Duration
	lastTime   time.Time
	lock       sync.RWMutex
}

var _ Limiter = (*FixedWindowLimiter)(nil)

/*
NewFixedWindowLimiter 新建一个固定窗口限流器
  - limit: 限制数量
  - windowSize: 窗口大小
*/
func NewFixedWindowLimiter(limit int, windowSize time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:      int32(limit),
		windowSize: windowSize,
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
	atomic.StoreInt32(&own.limit, int32(limit))
}

// SetWindowSize 设置窗口大小
func (own *FixedWindowLimiter) SetWindowSize(size time.Duration) {
	if size <= 0 {
		size = time.Second // 默认1秒
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	// 计算新窗口的起始时间
	now := time.Now()
	elapsed := now.Sub(own.lastTime)

	// 如果当前窗口已经过期，重置
	if elapsed >= own.windowSize {
		own.count = 0
		own.lastTime = now
	} else if own.windowSize != 0 {
		// 调整计数以匹配新的窗口大小
		ratio := float64(size) / float64(own.windowSize)
		adjustedCount := float64(own.count) * ratio
		own.count = int32(adjustedCount)
		if own.count < 0 {
			own.count = 0
		}
	}

	own.windowSize = size
}

// GetConfig 获取配置
func (own *FixedWindowLimiter) GetConfig() (limit int, windowSize time.Duration) {
	own.lock.RLock()
	limit = int(own.limit)
	windowSize = own.windowSize
	own.lock.RUnlock()
	return
}

// GetMiddleware 获取中间件函数
func (own *FixedWindowLimiter) GetMiddleware(timeout time.Duration) leego.MiddlewareFunc {
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
func (own *FixedWindowLimiter) Allow() bool {
	return own.allow()
}

// AllowWithTimeout 带超时的允许检查
func (own *FixedWindowLimiter) AllowWithTimeout(ctx context.Context, timeout time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return own.Wait(timeoutCtx) == nil
}

// Wait 带上下文的允许检查，直到允许或取消上下文
func (own *FixedWindowLimiter) Wait(ctx context.Context) error {
	// 先尝试一次
	if own.allow() {
		return nil
	}

	// 如果没有设置超时，直接返回错误
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("limiter: context has no deadline")
	}

	// 阻塞等待允许或超时
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
			if own.allow() {
				return nil
			}
		}
	}
}

func (own *FixedWindowLimiter) allow() bool {
	// 快速路径尝试
	if own.fastAllow() {
		return true
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	// 检查是否需要重置窗口
	now := time.Now()
	if now.Sub(own.lastTime) >= own.windowSize {
		atomic.StoreInt32(&own.count, 0)
		own.lastTime = now
	}

	// 检查是否超过限制
	current := atomic.LoadInt32(&own.count)
	if current >= atomic.LoadInt32(&own.limit) {
		return false
	}

	atomic.AddInt32(&own.count, 1)
	return true
}

// fastAllow 使用原子操作的快速路径
func (own *FixedWindowLimiter) fastAllow() bool {
	current := atomic.LoadInt32(&own.count)
	if current >= atomic.LoadInt32(&own.limit) {
		return false
	}
	return atomic.CompareAndSwapInt32(&own.count, current, current+1)
}
