package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/collector"
)

// TestCollectRecordsMetrics 验证指标中间件记录请求与状态码。
func TestCollectRecordsMetrics(t *testing.T) {
	monitor := collector.NewCollector(http.MethodGet, "/api/ping")

	handle := Collect(monitor)(func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	handle(leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/ping", nil)))

	snapshot := monitor.GetSnapshot()
	if snapshot.Total != 1 {
		t.Fatalf("Total = %d, want 1", snapshot.Total)
	}

	if snapshot.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", snapshot.Failed)
	}

	if len(snapshot.Metrics) != 1 || snapshot.Metrics[0].Code != http.StatusOK {
		t.Fatalf("状态码指标 = %+v", snapshot.Metrics)
	}
}

// TestCollectRecordsFailures 验证 5xx 被计入失败。
func TestCollectRecordsFailures(t *testing.T) {
	monitor := collector.NewCollector(http.MethodPost, "/api/error")

	handle := Collect(monitor)(func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusInternalServerError, "boom")
	})

	handle(leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/error", nil)))

	snapshot := monitor.GetSnapshot()
	if snapshot.Total != 1 || snapshot.Failed != 1 {
		t.Fatalf("快照 = %+v, want total=1, failed=1", snapshot)
	}

	if snapshot.GetFailureRate() != 1 {
		t.Fatalf("失败率 = %v, want 1", snapshot.GetFailureRate())
	}
}

// TestCollectWithNilMonitor 验证 nil 采集器返回空操作中间件。
func TestCollectWithNilMonitor(t *testing.T) {
	executed := false
	handle := Collect(nil)(func(ctx *leego.Context) {
		executed = true
	})

	handle(leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)))
	if !executed {
		t.Fatal("nil 采集器时仍必须调用后续处理器")
	}
}
