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
	limit      int
	isEnabled  int32
	total      int           // 窗口内总请求数（缓存）
	index      int           // 当前桶索引
	buckets    []int         // 时间桶数组
	lastSlide  time.Time     // 上次滑动时间
	windowSize time.Duration // 窗口大小
	bucketSize time.Duration // 每个桶的时间间隔
	lock       sync.RWMutex
}

var _ Limiter = (*SlidingWindowLimiter)(nil)

// NewSlidingWindowLimite 新建一个滑动窗口限流器，可自定义桶的数量和窗口大小
func NewSlidingWindowLimite(limit int, bucketCount int, windowSize time.Duration) *SlidingWindowLimiter {
	if bucketCount <= 0 {
		bucketCount = 10
	}
	if windowSize < time.Second {
		windowSize = time.Second
	}

	return &SlidingWindowLimiter{
		buckets:    make([]int, bucketCount),
		limit:      limit,
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
	own.lock.Lock()
	own.limit = limit
	own.lock.Unlock()
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
	limit = own.limit
	bucketCount = len(own.buckets)
	windowSize = own.windowSize
	own.lock.RUnlock()
	return
}

// GetMiddleware 获取中间件函数
func (own *SlidingWindowLimiter) GetMiddleware() leego.MiddlewareFunc {
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
func (own *SlidingWindowLimiter) Allow() bool {
	return own.allow()

}

// Wait 阻塞，直到获取令牌或取消上下文
func (own *SlidingWindowLimiter) Wait(ctx context.Context) error {
	// 先尝试一次
	if own.Allow() {
		return nil
	}

	// 如果没有设置超时，直接返回
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("limiter: not set context.Context timeout")
	}

	// 使用更高效的等待策略
	waitTime := own.bucketSize / 2 // 等待半个桶的时间
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			if own.Allow() {
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
				own.total -= own.buckets[own.index]            // 减去被移除桶的计数
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
