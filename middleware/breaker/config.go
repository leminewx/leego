package breaker

import (
	"sync/atomic"
	"time"
)

// Config 定义熔断器配置信息的结构类型
type Config struct {
	IsEnabled        bool          `json:"is_enabled"`        // 是否启用
	FailureThreshold int           `json:"failure_threshold"` // 失败阈值
	SuccessThreshold int           `json:"success_threshold"` // 成功阈值
	BreakTimeout     time.Duration `json:"break_timeout"`     // 熔断超时时间
}

// GetConfig 获取熔断器配置
func (own *CircuitBreaker) GetConfig() Config {
	config := Config{
		IsEnabled: own.IsEnabled(),
	}

	own.lock.RLock()
	config.FailureThreshold = own.failureThreshold
	config.SuccessThreshold = own.successThreshold
	config.BreakTimeout = own.breakTimeout
	own.lock.Unlock()
	return config
}

// SetSuccessChecker 设置自定义的成功判断函数
func (own *CircuitBreaker) SetSuccessChecker(checker func(status int, err error) bool) {
	own.lock.Lock()
	own.isSuccessFunc = checker
	own.lock.Unlock()
}

// Enable 启用
func (own *CircuitBreaker) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用
func (own *CircuitBreaker) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)
}

// IsEnabled 检查是否启用
func (own *CircuitBreaker) IsEnabled() bool {
	return atomic.LoadInt32(&own.isEnabled) == 1
}

// GetState 获取当前熔断器状态
func (own *CircuitBreaker) GetState() State {
	own.lock.RLock()
	state := own.state
	own.lock.RUnlock()
	return state
}

// GetMetricsSnapshot 获取熔断器指标快照
func (own *CircuitBreaker) GetMetricsSnapshot() MetricsSnapshot {
	metrics := own.metrics.generateSnapshot()
	metrics.CurrentState = own.GetState()
	return metrics
}

// ResetMetrics 重置指标
func (own *CircuitBreaker) ResetMetrics() {
	own.lock.Lock()
	own.metrics.totalRequests.Store(0)
	own.metrics.failedRequests.Store(0)
	own.metrics.blockedRequests.Store(0)
	own.metrics.stateChanges.Store(0)
	own.lock.Unlock()
}
