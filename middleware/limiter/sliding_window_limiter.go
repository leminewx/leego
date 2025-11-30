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
	index      int           // 当前桶索引
	buckets    []int         // 时间桶数组
	config     *Config       // 配置信息
	lastSlide  time.Time     // 上次滑动时间
	bucketSize time.Duration // 每个桶的时间间隔
	lock       sync.RWMutex
}

var _ leego.Middleware = &SlidingWindowLimiter{}

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

	config := newConfig(limit)
	config.WindowSize = windowSize

	return &SlidingWindowLimiter{
		buckets:    make([]int, bucketCount),
		config:     config,
		lastSlide:  time.Now(),
		bucketSize: windowSize / time.Duration(bucketCount),
	}
}

// Enable 启用中间件
func (own *SlidingWindowLimiter) Enable() {
	atomic.StoreInt32(&own.config.IsEnabled, 1)
}

// Disable 禁用中间件
func (own *SlidingWindowLimiter) Disable() {
	atomic.StoreInt32(&own.config.IsEnabled, 0)
}

// SetScope 设置作用域
func (own *SlidingWindowLimiter) SetScope(scope string) {
	if scope == "" {
		scope = "/"
	}

	own.lock.Lock()
	own.config.Scope = scope
	own.lock.Unlock()
}

// SetLimit 设置限流阈值
func (own *SlidingWindowLimiter) SetLimit(limit int) {
	own.lock.Lock()
	own.config.Limit = limit
	own.lock.Unlock()
}

// SetWindowSize 设置窗口大小
func (own *SlidingWindowLimiter) SetWindowSize(size time.Duration) {
	if size < time.Second {
		size = time.Second
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	if own.config.WindowSize == size {
		return
	}

	// 如果窗口大小改变，需要重置所有桶
	own.config.WindowSize = size
	own.bucketSize = size / time.Duration(len(own.buckets))

	// 重置窗口状态
	for i := range own.buckets {
		own.buckets[i] = 0
	}

	own.index = 0
	own.lastSlide = time.Now()
}

// GetID 获取ID
func (own *SlidingWindowLimiter) GetID() string {
	return own.config.Id
}

// GetConfig 获取配置信息（线程安全）
func (own *SlidingWindowLimiter) GetConfig() any {
	own.lock.RLock()
	config := own.config.Clone()
	for _, count := range own.buckets {
		config.Requests += count
	}
	own.lock.RUnlock()
	return config
}

// SyncConfig 同步配置
func (own *SlidingWindowLimiter) SyncConfig(config any) error {
	own.lock.Lock()
	err := own.config.Sync(config)
	own.lock.Unlock()
	return err
}

// Handle 中间件处理函数
func (own *SlidingWindowLimiter) Handle(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 未启用，直接跳过
		if atomic.LoadInt32(&own.config.IsEnabled) == 0 {
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
	if total >= own.config.Limit {
		return false
	}

	// 增加当前桶的计数
	own.buckets[own.index]++
	return true
}
