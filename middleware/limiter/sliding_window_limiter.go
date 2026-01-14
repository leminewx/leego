package limiter

import (
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
	index      int           // 当前桶索引
	buckets    []int         // 时间桶数组
	lastSlide  time.Time     // 上次滑动时间
	windowSize time.Duration // 窗口大小
	bucketSize time.Duration // 每个桶的时间间隔
	lock       sync.RWMutex
}

/*
NewDefaultSlidingWindowLimiter 新建一个默认的滑动窗口限流器
  - limit：限流阈值
  - 默认桶数量为 10 个
  - 默认滑动窗口大小为 1s
*/
func NewDefaultSlidingWindowLimiter(limit int) *SlidingWindowLimiter {
	return NewSlidingWindowLimiterWithBucketsAndWindowSize(limit, 10, time.Second)
}

// NewSlidingWindowLimiterWithBucketsAndWindowSize 新建一个滑动窗口限流器，可自定义桶的数量和窗口大小
func NewSlidingWindowLimiterWithBucketsAndWindowSize(limit int, bucketCount int, windowSize time.Duration) *SlidingWindowLimiter {
	if bucketCount <= 0 {
		bucketCount = 10
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

// SetWindowSize 设置窗口大小
func (own *SlidingWindowLimiter) SetWindowSize(size time.Duration) {
	if size < time.Second {
		size = time.Second
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	if own.windowSize == size {
		return
	}

	// 如果窗口大小改变，需要重置所有桶
	own.windowSize = size
	own.bucketSize = size / time.Duration(len(own.buckets))

	// 重置窗口状态
	for i := range own.buckets {
		own.buckets[i] = 0
	}

	own.index = 0
	own.lastSlide = time.Now()
}

// GetConfig 获取配置
func (own *SlidingWindowLimiter) GetConfig() (limit int, windowSize time.Duration) {
	own.lock.RLock()
	limit = own.limit
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

// Allow 检查是否允许通过
func (own *SlidingWindowLimiter) Allow() bool {
	own.lock.Lock()
	defer own.lock.Unlock()

	now := time.Now()
	elapsed := now.Sub(own.lastSlide)

	// 计算需要滑动的桶数量
	slideBuckets := int(elapsed / own.bucketSize)

	if slideBuckets > 0 {
		// 滑动窗口：重置过期的桶
		for i := 0; i < slideBuckets && i < len(own.buckets); i++ {
			own.index = (own.index + 1) % len(own.buckets)
			own.buckets[own.index] = 0
		}

		own.lastSlide = now
	}

	// 计算当前窗口内的总请求数
	total := 0
	for _, count := range own.buckets {
		total += count
	}

	// 检查是否超过限流阈值
	if total >= own.limit {
		return false
	}

	// 增加当前桶的计数
	own.buckets[own.index]++
	return true
}
