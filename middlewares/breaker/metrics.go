package breaker

import (
	"sync/atomic"
	"time"
)

// MetricsSnapshot 定义熔断器运行指标快照。
//
// 快照是某一时刻的一致性视图：调用方拿到值之后，
// 熔断器继续运行也不会影响已返回的快照，因此可以安全地跨协程传递或序列化。
type MetricsSnapshot struct {
	// TotalRequests 为累计进入熔断判断的请求数（包含被拒绝的请求）。
	TotalRequests int64 `json:"total_requests"`
	// FailedRequests 为累计被判定为失败的请求数。
	FailedRequests int64 `json:"failed_requests"`
	// BlockedRequests 为累计被熔断拒绝、未真正执行的请求数。
	BlockedRequests int64 `json:"blocked_requests"`
	// StateChanges 为累计状态切换次数。
	StateChanges int64 `json:"state_changes"`
	// CurrentState 为快照时刻的状态。
	CurrentState State `json:"current_state"`
	// WindowSuccesses 为当前滑动窗口内的成功次数。
	WindowSuccesses int64 `json:"window_successes"`
	// WindowFailures 为当前滑动窗口内的失败次数。
	WindowFailures int64 `json:"window_failures"`
	// WindowFailureRate 为当前滑动窗口的失败率，取值 [0,1]；窗口内无样本时为 0。
	WindowFailureRate float64 `json:"window_failure_rate"`
	// SnapshotTime 为快照生成时间。
	SnapshotTime time.Time `json:"snapshot_time"`
}

// metrics 保存熔断器的累计计数器。
//
// 所有字段均为原子类型：累计计数只增不减，使用 atomic 可以完全避开状态锁，
// 也让 /metrics 之类的观测接口不会阻塞请求热路径。
type metrics struct {
	// totalRequests 为累计请求数。
	totalRequests atomic.Int64
	// failedRequests 为累计失败数。
	failedRequests atomic.Int64
	// blockedRequests 为累计被拒绝数。
	blockedRequests atomic.Int64
	// stateChanges 为累计状态切换次数。
	stateChanges atomic.Int64
}

// generateSnapshot 基于累计计数器生成快照。
//
// 该方法只读取原子计数器，不涉及状态锁，因此可在任意协程调用；
// 状态与窗口统计需要额外加锁，请使用 CircuitBreaker.GetMetricsSnapshot。
func (own *metrics) generateSnapshot() MetricsSnapshot {
	return MetricsSnapshot{
		TotalRequests:   own.totalRequests.Load(),
		FailedRequests:  own.failedRequests.Load(),
		BlockedRequests: own.blockedRequests.Load(),
		StateChanges:    own.stateChanges.Load(),
		SnapshotTime:    time.Now(),
	}
}

// Reset 将全部累计计数器归零。
func (own *metrics) Reset() {
	own.totalRequests.Store(0)
	own.failedRequests.Store(0)
	own.blockedRequests.Store(0)
	own.stateChanges.Store(0)
}

// GetMetricsSnapshot 返回熔断器的完整指标快照。
//
// 与仅读取原子计数器的 generateSnapshot 不同，本方法需要读取当前状态与
// 滑动窗口统计，因此会对状态锁做一次短暂持有。锁内只做整数累加，
// 不会调用任何外部代码，不会造成锁升级或重入。
func (own *CircuitBreaker) GetMetricsSnapshot() MetricsSnapshot {
	snapshot := own.metrics.generateSnapshot()

	own.mu.Lock()
	snapshot.CurrentState = own.state
	successes, failures := own.countLocked()
	own.mu.Unlock()

	snapshot.WindowSuccesses = successes
	snapshot.WindowFailures = failures
	if total := successes + failures; total > 0 {
		snapshot.WindowFailureRate = float64(failures) / float64(total)
	}

	return snapshot
}
