package breaker

import (
	"sync/atomic"
	"time"
)

// MetricsSnapshot 定义熔断器指标快照的结构类型
type MetricsSnapshot struct {
	TotalRequests   int64     `json:"total_requests"`   // 总请求数
	FailedRequests  int64     `json:"failed_requests"`  // 失败请求数
	BlockedRequests int64     `json:"blocked_requests"` // 被熔断拒绝的请求数
	StateChanges    int64     `json:"state_changes"`    // 状态变化次数
	CurrentState    State     `json:"current_state"`    // 当前状态
	SnapshotTime    time.Time `json:"snapshot_time"`    // 快照时间
}

// metrics 定义熔断器运行指标的结构类型
type metrics struct {
	totalRequests   atomic.Int64 // 总请求数
	failedRequests  atomic.Int64 // 失败请求数
	blockedRequests atomic.Int64 // 被熔断拒绝的请求数
	stateChanges    atomic.Int64 // 状态变化次数
}

// generateSnapshot 获取熔断器运行指标快照
func (own *metrics) generateSnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		TotalRequests:   own.totalRequests.Load(),
		FailedRequests:  own.failedRequests.Load(),
		BlockedRequests: own.blockedRequests.Load(),
		StateChanges:    own.stateChanges.Load(),
		SnapshotTime:    time.Now(),
	}
}
