package breaker

import (
	"time"
)

// 默认配置常量。
//
// 取值原则：在“快速隔离故障”和“避免误熔断”之间取平衡，
// 并且对零值配置也给出可用结果。
const (
	// _DEFAULT_WINDOW_SIZE 为失败率统计窗口，默认 10 秒。
	_DEFAULT_WINDOW_SIZE = 10 * time.Second
	// _DEFAULT_MINIMUM_REQUESTS 为触发评估所需的最小样本数。
	_DEFAULT_MINIMUM_REQUESTS int64 = 10
	// _DEFAULT_FAILURE_RATE 为触发熔断的失败率阈值（50%）。
	_DEFAULT_FAILURE_RATE = 0.5
	// _DEFAULT_OPEN_TIMEOUT 为打开状态的持续时间，默认 30 秒。
	_DEFAULT_OPEN_TIMEOUT = 30 * time.Second
	// _DEFAULT_HALF_OPEN_MAX_REQUESTS 为半开状态允许的并发探测数。
	_DEFAULT_HALF_OPEN_MAX_REQUESTS int64 = 1
	// _DEFAULT_HALF_OPEN_SUCCESS_THRESHOLD 为半开状态恢复闭合所需的连续成功数。
	_DEFAULT_HALF_OPEN_SUCCESS_THRESHOLD int64 = 3
)

// Config 定义熔断器配置。
type Config struct {
	// IsEnabled 表示是否启用熔断（创建后即生效）。
	IsEnabled bool
	// WindowSize 为失败率统计窗口。
	WindowSize time.Duration
	// MinimumRequests 为窗口内触发评估所需的最小请求数。
	MinimumRequests int64
	// FailureRate 为触发熔断的失败率阈值，取值范围 (0, 1]。
	FailureRate float64
	// OpenTimeout 为打开状态的持续时间。
	OpenTimeout time.Duration
	// HalfOpenMaxRequests 为半开状态允许的并发探测请求数。
	HalfOpenMaxRequests int64
	// HalfOpenSuccessThreshold 为半开状态恢复闭合所需的连续成功数。
	HalfOpenSuccessThreshold int64
}

// NewDefaultConfig 返回默认熔断配置。
//
// 默认启用熔断，窗口 10 秒、最小样本 10、失败率 50%、
// 打开 30 秒、半开放行 1 个探测、连续 3 次成功后恢复。
func NewDefaultConfig() Config {
	return Config{
		IsEnabled:                true,
		WindowSize:               _DEFAULT_WINDOW_SIZE,
		MinimumRequests:          _DEFAULT_MINIMUM_REQUESTS,
		FailureRate:              _DEFAULT_FAILURE_RATE,
		OpenTimeout:              _DEFAULT_OPEN_TIMEOUT,
		HalfOpenMaxRequests:      _DEFAULT_HALF_OPEN_MAX_REQUESTS,
		HalfOpenSuccessThreshold: _DEFAULT_HALF_OPEN_SUCCESS_THRESHOLD,
	}
}

// Normalize 返回修正非法数值后的配置副本。
//
// 修正规则：
//   - WindowSize 小于等于 0 时使用默认值；
//   - MinimumRequests 小于 1 时使用 1（至少 1 个样本才能计算失败率）；
//   - FailureRate 不在 (0,1] 区间时使用默认值；
//   - OpenTimeout 小于等于 0 时使用默认值；
//   - HalfOpenMaxRequests 小于 1 时使用 1；
//   - HalfOpenSuccessThreshold 小于 1 时使用 1。
func (own Config) Normalize() Config {
	normalized := own
	defaults := NewDefaultConfig()

	if normalized.WindowSize <= 0 {
		normalized.WindowSize = defaults.WindowSize
	}

	if normalized.MinimumRequests < 1 {
		normalized.MinimumRequests = 1
	}

	if normalized.FailureRate <= 0 || normalized.FailureRate > 1 {
		normalized.FailureRate = defaults.FailureRate
	}

	if normalized.OpenTimeout <= 0 {
		normalized.OpenTimeout = defaults.OpenTimeout
	}

	if normalized.HalfOpenMaxRequests < 1 {
		normalized.HalfOpenMaxRequests = 1
	}

	if normalized.HalfOpenSuccessThreshold < 1 {
		normalized.HalfOpenSuccessThreshold = 1
	}

	return normalized
}

// GetConfig 返回当前配置的副本。
//
// 配置在创建后不可变，因此无需加锁即可返回。
func (own *CircuitBreaker) GetConfig() Config {
	return own.config
}

// SetSuccessChecker 设置自定义的成功判定函数。
//
// 判定函数签名中的 status 为 HTTP 状态码，err 为执行错误；
// 传入 nil 表示恢复默认策略（err == nil 且 status < 500）。
func (own *CircuitBreaker) SetSuccessChecker(checker func(status int, err error) bool) {
	own.mu.Lock()
	defer own.mu.Unlock()

	if checker == nil {
		own.isSuccessFunc = func(status int, err error) bool {
			return err == nil && status < 500
		}
		return
	}

	own.isSuccessFunc = checker
}

// Enable 启用熔断器。
func (own *CircuitBreaker) Enable() {
	own.isEnabled.Store(true)
}

// Disable 禁用熔断器。
//
// 禁用后中间件直接放行，但已有状态与指标会保留，便于重新启用后继续观察。
func (own *CircuitBreaker) Disable() {
	own.isEnabled.Store(false)
}

// IsEnabled 返回熔断器是否启用。
func (own *CircuitBreaker) IsEnabled() bool {
	return own.isEnabled.Load()
}

// GetState 返回当前状态。
func (own *CircuitBreaker) GetState() State {
	own.mu.Lock()
	state := own.state
	own.mu.Unlock()
	return state
}

// ResetMetrics 重置熔断指标。
//
// 该方法只清零计数器，不会改变当前状态，避免运维误操作导致保护失效。
func (own *CircuitBreaker) ResetMetrics() {
	own.metrics.Reset()
}

// resetWindow 仅供测试使用：清空滑动窗口与探测计数。
func (own *CircuitBreaker) resetWindow() {
	own.mu.Lock()
	defer own.mu.Unlock()

	for index := range own.buckets {
		own.buckets[index] = windowBucket{}
	}

	own.currentBucket = 0
	own.lastAdvance = time.Now()
	own.probes = 0
	own.successes = 0
}
