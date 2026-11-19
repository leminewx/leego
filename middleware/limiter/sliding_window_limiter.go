package limiter

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
)

// 滑动窗口默认参数。
const (
	// _DEFAULT_SLIDING_BUCKET_COUNT 为默认的桶数量。
	_DEFAULT_SLIDING_BUCKET_COUNT = 10
	// _DEFAULT_SLIDING_WINDOW_SIZE 为默认的窗口长度。
	_DEFAULT_SLIDING_WINDOW_SIZE = time.Second
)

/*
SlidingWindowLimiter 实现滑动窗口限流。

原理：把整个窗口切成若干等长的小桶，桶环形复用。
每来一个请求就把“已过期”的桶清零并推进索引，窗口内总计数随之减少。
相比固定窗口，它把边界突发摊平到多个小桶上，流量曲线更平滑。

计数（total）与桶数组必须由同一把锁保护：
total 是缓存值，若允许并发写入就会出现与桶内实际计数不一致的情况。
*/
type SlidingWindowLimiter struct {
	// isEnabled 表示限流是否启用。
	isEnabled atomic.Bool
	// limit 为窗口内允许的最大请求数（atomic，支持热更新）。
	limit atomic.Int64

	// mu 保护以下全部字段。
	mu sync.Mutex
	// buckets 为环形桶数组，每个元素保存该时间片内的请求数。
	buckets []int64
	// total 为窗口内所有桶的请求数之和（缓存值）。
	total int64
	// index 为当前桶下标。
	index int
	// lastSlide 为上次推进窗口的时间。
	lastSlide time.Time
	// windowSize 为窗口长度。
	windowSize time.Duration
	// bucketSize 为单个桶的时间跨度。
	bucketSize time.Duration
}

// 编译期断言：滑动窗口限流器必须满足 Limiter 接口。
var _ Limiter = (*SlidingWindowLimiter)(nil)

/*
NewSlidingWindowLimiter 创建滑动窗口限流器。

参数：
  - limit：窗口内允许的最大请求数；小于等于 0 表示不限流；
  - bucketCount：桶数量，小于 1 时使用默认值 10；
  - windowSize：窗口长度，小于 1 秒时使用默认值 1 秒。

桶数量越多，窗口边界越平滑，但单次判断的计算量也越大。
限流器创建后即处于启用状态。
*/
func NewSlidingWindowLimiter(limit int, bucketCount int, windowSize time.Duration) *SlidingWindowLimiter {
	if bucketCount < 1 {
		bucketCount = _DEFAULT_SLIDING_BUCKET_COUNT
	}

	if windowSize < _DEFAULT_SLIDING_WINDOW_SIZE {
		windowSize = _DEFAULT_SLIDING_WINDOW_SIZE
	}

	instance := &SlidingWindowLimiter{
		buckets:    make([]int64, bucketCount),
		lastSlide:  time.Now(),
		windowSize: windowSize,
		bucketSize: windowSize / time.Duration(bucketCount),
	}
	instance.limit.Store(int64(limit))
	instance.Enable()

	return instance
}

// Enable 启用限流。
func (own *SlidingWindowLimiter) Enable() {
	own.isEnabled.Store(true)
}

// Disable 临时禁用限流，中间件将直接放行。
func (own *SlidingWindowLimiter) Disable() {
	own.isEnabled.Store(false)
}

// IsEnabled 返回限流是否启用。
func (own *SlidingWindowLimiter) IsEnabled() bool {
	return own.isEnabled.Load()
}

// SetLimit 更新窗口内允许的最大请求数，非正数表示不限流。
func (own *SlidingWindowLimiter) SetLimit(limit int) {
	own.limit.Store(int64(limit))
}

// SetBucketCount 设置桶数量并重置窗口。
//
// 重建桶后必须同步重置索引与累计计数，否则索引可能越界、
// total 也会与桶内实际计数不一致。
func (own *SlidingWindowLimiter) SetBucketCount(bucketCount int) {
	if bucketCount < 1 {
		bucketCount = _DEFAULT_SLIDING_BUCKET_COUNT
	}

	own.mu.Lock()
	own.buckets = make([]int64, bucketCount)
	own.bucketSize = own.windowSize / time.Duration(bucketCount)
	own.index = 0
	own.total = 0
	own.lastSlide = time.Now()
	own.mu.Unlock()
}

// SetWindowSize 设置窗口长度并重置窗口。
func (own *SlidingWindowLimiter) SetWindowSize(windowSize time.Duration) {
	if windowSize < _DEFAULT_SLIDING_WINDOW_SIZE {
		windowSize = _DEFAULT_SLIDING_WINDOW_SIZE
	}

	own.mu.Lock()
	own.windowSize = windowSize
	own.bucketSize = windowSize / time.Duration(len(own.buckets))
	own.index = 0
	own.total = 0
	for index := range own.buckets {
		own.buckets[index] = 0
	}
	own.lastSlide = time.Now()
	own.mu.Unlock()
}

// GetLimit 返回窗口内允许的最大请求数。
func (own *SlidingWindowLimiter) GetLimit() int {
	return int(own.limit.Load())
}

// GetBucketCount 返回桶数量。
func (own *SlidingWindowLimiter) GetBucketCount() int {
	own.mu.Lock()
	count := len(own.buckets)
	own.mu.Unlock()
	return count
}

// GetWindowSize 返回窗口长度。
func (own *SlidingWindowLimiter) GetWindowSize() time.Duration {
	own.mu.Lock()
	windowSize := own.windowSize
	own.mu.Unlock()
	return windowSize
}

// GetCurrentCount 返回当前窗口内已放行的请求数，主要用于观测与测试。
func (own *SlidingWindowLimiter) GetCurrentCount() int64 {
	own.mu.Lock()
	count := own.total
	own.mu.Unlock()
	return count
}

// GetConfig 返回当前配置。
func (own *SlidingWindowLimiter) GetConfig() (limit, bucketCount int, windowSize time.Duration) {
	own.mu.Lock()
	bucketCount = len(own.buckets)
	windowSize = own.windowSize
	own.mu.Unlock()

	return int(own.limit.Load()), bucketCount, windowSize
}

// GetMiddleware 返回滑动窗口限流中间件。
//
// 参数 waitTimeout 语义与固定窗口限流器一致：
// 0 表示立即失败，负数表示使用默认的 3 秒等待。
func (own *SlidingWindowLimiter) GetMiddleware(waitTimeout time.Duration) leego.MiddlewareFunc {
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

// Allow 非阻塞地判断是否放行，放行时消耗一个配额。
func (own *SlidingWindowLimiter) Allow() bool {
	own.mu.Lock()
	defer own.mu.Unlock()

	own.advanceLocked(time.Now())

	limit := own.limit.Load()
	if limit <= 0 {
		return true
	}

	if own.total >= limit {
		return false
	}

	own.buckets[own.index]++
	own.total++

	return true
}

// advanceLocked 按流逝时间推进窗口并清空过期桶。
func (own *SlidingWindowLimiter) advanceLocked(now time.Time) {
	if own.bucketSize <= 0 || len(own.buckets) == 0 {
		return
	}

	steps := int(now.Sub(own.lastSlide) / own.bucketSize)
	if steps <= 0 {
		return
	}

	// 跨度超过整个窗口：整体清零，无需逐桶处理。
	if steps >= len(own.buckets) {
		for index := range own.buckets {
			own.buckets[index] = 0
		}

		own.total = 0
		own.index = 0
		own.lastSlide = now

		return
	}

	for step := 0; step < steps; step++ {
		own.index = (own.index + 1) % len(own.buckets)
		own.total -= own.buckets[own.index]
		own.buckets[own.index] = 0
	}

	if own.total < 0 {
		own.total = 0
	}

	own.lastSlide = now
}

// AllowWithTimeout 在给定超时内等待配额。
func (own *SlidingWindowLimiter) AllowWithTimeout(ctx context.Context, timeout time.Duration) bool {
	if timeout <= 0 {
		return own.Allow()
	}

	if ctx == nil {
		ctx = context.Background()
	}

	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return waitForQuota(waitContext, own.Allow) == nil
}

// Wait 阻塞等待配额，直到放行或上下文结束。
func (own *SlidingWindowLimiter) Wait(ctx context.Context) error {
	return waitForQuota(ctx, own.Allow)
}
