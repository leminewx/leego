package limiter

import (
	"net/http"
	"sync"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// FixedWindowLimiter 定义固定窗口限流器的数据结构
type FixedWindowLimiter struct {
	count    int
	config   *Config
	lastTime time.Time
	lock     sync.RWMutex
}

var (
	_ Limiter          = &FixedWindowLimiter{}
	_ leego.Middleware = &FixedWindowLimiter{}
)

/*
NewFixedWindowLimiter 新建一个默认的固定窗口限流器
  - limit：限流阈值
  - 默认固定窗口大小为 1s
*/
func NewFixedWindowLimiter(limit int) *FixedWindowLimiter {
	return NewFixedWindowLimiterWithWindowSize(limit, time.Second)
}

// NewFixedWindowLimiterWithWindowSize 新建一个固定窗口限流器（可自定义窗口大小）
func NewFixedWindowLimiterWithWindowSize(limit int, size time.Duration) *FixedWindowLimiter {
	config := newConfig(limit)
	config.WindowSize = size

	return &FixedWindowLimiter{
		config:   config,
		lastTime: time.Now(),
	}
}

// SetScope 设置作用域
func (own *FixedWindowLimiter) SetScope(scope string) {
	own.lock.Lock()
	own.config.Scope = scope
	own.lock.Unlock()
}

// WithLimit 设置限流阈值
func (own *FixedWindowLimiter) WithLimit(limit int) Limiter {
	own.lock.Lock()
	own.config.Limit = limit
	own.lock.Unlock()
	return own
}

// WithWindowSize 设置窗口大小
func (own *FixedWindowLimiter) WithWindowSize(size time.Duration) Limiter {
	if size <= 0 {
		size = time.Second // 默认1秒
	}

	own.lock.Lock()
	own.count = 0
	own.lastTime = time.Now()
	own.config.WindowSize = size
	own.lock.Unlock()
	return own
}

// GetConfig 获取配置信息（线程安全）
func (own *FixedWindowLimiter) GetConfig() *Config {
	own.lock.RLock()
	config := own.config.Clone()
	config.Requests = own.count
	own.lock.RUnlock()
	return config
}

// Sync 同步配置
func (own *FixedWindowLimiter) Sync(config *Config) {
	own.lock.Lock()
	own.config.Sync(config)
	own.lock.Unlock()
}

// Handle 中间件处理函数
func (own *FixedWindowLimiter) Handle(handle leego.HandleFunc) leego.HandleFunc {
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
func (own *FixedWindowLimiter) Allow() bool {
	now := time.Now()

	own.lock.Lock()
	defer own.lock.Unlock()

	if now.Sub(own.lastTime) >= own.config.WindowSize {
		own.count = 0
		own.lastTime = now
	}

	if own.count >= own.config.Limit {
		return false
	}

	own.count++
	return true
}
