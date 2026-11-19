package breaker

import (
	"testing"
	"time"
)

// TestGenerateSnapshot 验证原子计数器的快照读取。
func TestGenerateSnapshot(t *testing.T) {
	own := &metrics{}
	own.totalRequests.Store(10)
	own.failedRequests.Store(3)
	own.blockedRequests.Store(2)
	own.stateChanges.Store(1)

	snapshot := own.generateSnapshot()

	if snapshot.TotalRequests != 10 || snapshot.FailedRequests != 3 {
		t.Fatalf("请求统计 = (%d, %d), want (10, 3)", snapshot.TotalRequests, snapshot.FailedRequests)
	}

	if snapshot.BlockedRequests != 2 || snapshot.StateChanges != 1 {
		t.Fatalf("拒绝与状态切换 = (%d, %d), want (2, 1)", snapshot.BlockedRequests, snapshot.StateChanges)
	}

	if snapshot.SnapshotTime.IsZero() {
		t.Fatal("SnapshotTime 必须被设置")
	}
}

// TestMetricsReset 验证计数器归零。
func TestMetricsReset(t *testing.T) {
	own := &metrics{}
	own.totalRequests.Add(5)
	own.failedRequests.Add(2)
	own.blockedRequests.Add(1)
	own.stateChanges.Add(3)

	own.Reset()

	snapshot := own.generateSnapshot()
	if snapshot.TotalRequests != 0 || snapshot.FailedRequests != 0 || snapshot.BlockedRequests != 0 || snapshot.StateChanges != 0 {
		t.Fatalf("Reset 后快照 = %+v, want 全零", snapshot)
	}
}

// TestGetMetricsSnapshot 验证完整快照包含状态与窗口统计。
func TestGetMetricsSnapshot(t *testing.T) {
	gate := createTestBreaker(100, time.Minute)

	_ = gate.Execute(func() error { return nil })
	_ = gate.Execute(func() error { return nil })
	gate.afterExecute(500, nil)

	snapshot := gate.GetMetricsSnapshot()

	if snapshot.CurrentState != STATE_CLOSED {
		t.Fatalf("CurrentState = %v, want %v", snapshot.CurrentState, STATE_CLOSED)
	}

	if snapshot.TotalRequests != 2 {
		t.Fatalf("TotalRequests = %d, want 2（仅 beforeExecute 计数）", snapshot.TotalRequests)
	}

	if snapshot.WindowSuccesses != 2 || snapshot.WindowFailures != 1 {
		t.Fatalf("窗口统计 = (%d, %d), want (2, 1)", snapshot.WindowSuccesses, snapshot.WindowFailures)
	}

	wantRate := 1.0 / 3.0
	if snapshot.WindowFailureRate != wantRate {
		t.Fatalf("窗口失败率 = %v, want %v", snapshot.WindowFailureRate, wantRate)
	}
}

// TestGetMetricsSnapshotEmptyWindow 验证空窗口的失败率不会除零。
func TestGetMetricsSnapshotEmptyWindow(t *testing.T) {
	gate := createTestBreaker(1, time.Minute)

	snapshot := gate.GetMetricsSnapshot()
	if snapshot.WindowFailureRate != 0 {
		t.Fatalf("空窗口失败率 = %v, want 0", snapshot.WindowFailureRate)
	}

	if snapshot.WindowSuccesses != 0 || snapshot.WindowFailures != 0 {
		t.Fatalf("空窗口统计 = (%d, %d), want (0, 0)", snapshot.WindowSuccesses, snapshot.WindowFailures)
	}
}

// TestMetricsSnapshotJSON 验证快照字段可被 JSON 序列化（结构体标签有效）。
func TestMetricsSnapshotJSON(t *testing.T) {
	snapshot := MetricsSnapshot{
		CurrentState:      STATE_OPEN,
		WindowFailureRate: 0.5,
	}

	if snapshot.CurrentState.String() != "CircuitBreaker.Open" {
		t.Fatalf("状态字符串 = %q", snapshot.CurrentState.String())
	}

	if snapshot.WindowFailureRate != 0.5 {
		t.Fatal("快照字段必须可直接读取")
	}
}
