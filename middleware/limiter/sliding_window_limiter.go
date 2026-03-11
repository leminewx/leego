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

// SlidingWindowLimiter 定义滑动窗口限流器的数据结构
type SlidingWindowLimiter struct {
	limit      int32
	isEnabled  int32
	total      int32         // 窗口内总请求数（缓存）
	index      int           // 当前桶索引
	buckets    []int         // 时间桶数组
	lastSlide  time.Time     // 上次滑动时间
	windowSize time.Duration // 窗口大小
	bucketSize time.Duration // 每个桶的时间间隔
	lock       sync.RWMutex
}

var _ Limiter = (*SlidingWindowLimiter)(nil)

/*
NewSlidingWindowLimiter 新建一个滑动窗口限流器
  - limit: 限制数量
  - bucketCount: 桶的数量
  - windowSize: 窗口大小
*/
func NewSlidingWindowLimiter(limit int, bucketCount int, windowSize time.Duration) *SlidingWindowLimiter {
	if bucketCount <= 0 {
		bucketCount = 10
	}
	if windowSize < time.Second {
		windowSize = time.Second
	}

	return &SlidingWindowLimiter{
		buckets:    make([]int, bucketCount),
		limit:      int32(limit),
		lastSlide:  time.Now(),
		windowSize: windowSize,
		bucketSize: windowSize / time.Duration(bucketCount),
	}
}

// Enable 启用中间件
func (own *SlidingWindowLimiter) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用中间件
func (own *SlidingWindowLimiter) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)
}

// IsEnabled 检查是否启用
func (own *SlidingWindowLimiter) IsEnabled() bool {
	return atomic.LoadInt32(&own.isEnabled) == 1
}

// SetLimit 设置限流阈值
func (own *SlidingWindowLimiter) SetLimit(limit int) {
	atomic.StoreInt32(&own.limit, int32(limit))

}

// SetBucketCount 设置桶的数量
func (own *SlidingWindowLimiter) SetBucketCount(count int) {
	own.lock.Lock()
	own.buckets = make([]int, count)
	own.bucketSize = own.windowSize / time.Duration(count)
	own.lock.Unlock()
}

// SetWindowSize 设置窗口大小
func (own *SlidingWindowLimiter) SetWindowSize(size time.Duration) {
	if size < time.Second {
		size = time.Second
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	// 重新计算桶大小
	oldBucketSize := own.bucketSize
	own.windowSize = size
	own.bucketSize = size / time.Duration(len(own.buckets))

	// 如果桶大小改变，需要重新计算当前索引
	if oldBucketSize > 0 {
		elapsed := time.Since(own.lastSlide)
		oldBucketsPassed := int(elapsed / oldBucketSize)
		newBucketsPassed := int(elapsed / own.bucketSize)

		// 调整索引
		own.index = (own.index + oldBucketsPassed - newBucketsPassed) % len(own.buckets)
		if own.index < 0 {
			own.index += len(own.buckets)
		}
	}
}

// GetConfig 获取配置
func (own *SlidingWindowLimiter) GetConfig() (limit, bucketCount int, windowSize time.Duration) {
	own.lock.RLock()
	limit = int(own.limit)
	bucketCount = len(own.buckets)
	windowSize = own.windowSize
	own.lock.RUnlock()
	return
}

// GetMiddleware 获取中间件函数
func (own *SlidingWindowLimiter) GetMiddleware(timeout time.Duration) leego.MiddlewareFunc {
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
func (own *SlidingWindowLimiter) Allow() bool {
	return own.allow()
}

// AllowWithTimeout 带超时的允许检查
func (own *SlidingWindowLimiter) AllowWithTimeout(ctx context.Context, timeout time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return own.Wait(timeoutCtx) == nil
}

// Wait 带上下文的允许检查，直到允许或取消上下文
func (own *SlidingWindowLimiter) Wait(ctx context.Context) error {
	// 先尝试一次
	if own.allow() {
		return nil
	}

	// 如果没有设置超时，直接返回
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

func (own *SlidingWindowLimiter) allow() bool {
	own.lock.Lock()
	defer own.lock.Unlock()

	now := time.Now()
	elapsed := now.Sub(own.lastSlide)

	// 计算需要滑动的桶数量
	slideBuckets := int(elapsed / own.bucketSize)

	if slideBuckets > 0 {
		// 如果滑动超过整个窗口，重置所有桶
		if slideBuckets >= len(own.buckets) {
			for i := range own.buckets {
				own.buckets[i] = 0
			}

			own.total = 0
			own.index = 0
		} else {
			// 只重置过期的桶
			for i := 0; i < slideBuckets; i++ {
				own.index = (own.index + 1) % len(own.buckets) // 移动到下一个桶
				own.total -= int32(own.buckets[own.index])     // 减去被移除桶的计数
				own.buckets[own.index] = 0                     // 重置该桶
			}
		}

		own.lastSlide = now
	}

	// 检查限流
	if own.total >= own.limit {
		return false
	}

	// 增加计数
	own.buckets[own.index]++
	own.total++
	return true
}
