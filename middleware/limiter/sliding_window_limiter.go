package limiter

import (
	"net/http"
	"sync"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// SlidingWindow 定义滑动窗口限流器的数据结构
type SlidingWindow struct {
	index      int           // 当前桶索引
	buckets    []int         // 时间桶数组
	config     *Config       // 配置信息
	lastSlide  time.Time     // 上次滑动时间
	bucketSize time.Duration // 每个桶的时间间隔
	lock       sync.RWMutex  // 读写锁
}

var (
	_ Limiter          = &SlidingWindow{}
	_ leego.Middleware = &SlidingWindow{}
)

/*
NewDefaultSlidingWindow 新建一个默认的滑动窗口限流器
  - limit：限流阈值
  - 默认桶数量为 10 个
  - 默认滑动窗口大小为 1s
*/
func NewDefaultSlidingWindow(limit int) *SlidingWindow {
	return NewSlidingWindowWithBucketsAndWindowSize(limit, 10, time.Second)
}

// NewSlidingWindowWithBucketsAndWindowSize 新建一个滑动窗口限流器，可自定义桶的数量和窗口大小
func NewSlidingWindowWithBucketsAndWindowSize(limit int, bucketCount int, windowSize time.Duration) *SlidingWindow {
	if bucketCount <= 0 {
		bucketCount = 10
	}

	config := newConfig(limit)
	config.WindowSize = windowSize

	return &SlidingWindow{
		buckets:    make([]int, bucketCount),
		config:     config,
		lastSlide:  time.Now(),
		bucketSize: windowSize / time.Duration(bucketCount),
	}
}

// WithLimit 设置限流阈值
func (own *SlidingWindow) WithLimit(limit int) Limiter {
	own.lock.Lock()
	own.config.Limit = limit
	own.lock.Unlock()
	return own
}

// SetScope 设置作用域
func (own *SlidingWindow) SetScope(scope string) {
	own.lock.Lock()
	own.config.Scope = scope
	own.lock.Unlock()
}

// WithWindowSize 设置窗口大小
func (own *SlidingWindow) WithWindowSize(size time.Duration) Limiter {
	if size < time.Second {
		size = time.Second
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	if own.config.WindowSize == size {
		return own
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
	return own
}

// GetConfig 获取配置信息（线程安全）
func (own *SlidingWindow) GetConfig() *Config {
	own.lock.RLock()
	config := own.config.Clone()
	for _, count := range own.buckets {
		config.Requests += count
	}
	own.lock.RUnlock()
	return config
}

// Sync 同步配置
func (own *SlidingWindow) Sync(config *Config) {
	own.lock.Lock()
	own.config.Sync(config)
	own.lock.Unlock()
}

// Handle 中间件处理函数
func (own *SlidingWindow) Handle(handle leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 请求过多，触发限流
		if !own.Allow() {
			ctx.ResponseError(http.StatusTooManyRequests, response.STATUS_429_TOO_MANY_REQUESTS)
			return
		}

		// 调用下一个处理器
		handle(ctx)
	}
}

// Allow 检查是否允许通过
func (own *SlidingWindow) Allow() bool {
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
