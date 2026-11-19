package collector

import (
	"testing"
	"time"
)

// TestNewReport 验证汇总报告的排序与统计。
func TestNewReport(t *testing.T) {
	snapshots := []Snapshot{
		{Method: "POST", Pattern: "/orders", Total: 3, Failed: 1, Flights: 1, TotalCostMicro: 900},
		{Method: "GET", Pattern: "/users/:id", Total: 5, Failed: 0, Flights: 2, TotalCostMicro: 500},
		{Method: "GET", Pattern: "/health", Total: 2, Failed: 0, Flights: 0, TotalCostMicro: 100},
	}

	report := NewReport(snapshots)

	if report.GeneratedAt.IsZero() {
		t.Fatal("GeneratedAt 必须被设置")
	}

	if len(report.Routes) != 3 {
		t.Fatalf("routes 数量 = %d, want 3", len(report.Routes))
	}

	// 排序规则：先按方法，再按模式。
	wantOrder := []string{"/health", "/users/:id", "/orders"}
	for index, want := range wantOrder {
		if report.Routes[index].Pattern != want {
			t.Fatalf("routes[%d].Pattern = %q, want %q", index, report.Routes[index].Pattern, want)
		}
	}

	if report.Totals.Routes != 3 {
		t.Fatalf("totals.routes = %d, want 3", report.Totals.Routes)
	}

	if report.Totals.Requests != 10 {
		t.Fatalf("totals.requests = %d, want 10", report.Totals.Requests)
	}

	if report.Totals.Failed != 1 {
		t.Fatalf("totals.failed = %d, want 1", report.Totals.Failed)
	}

	if report.Totals.Flights != 3 {
		t.Fatalf("totals.flights = %d, want 3", report.Totals.Flights)
	}

	if report.Totals.TotalCostMicro != 1500 {
		t.Fatalf("totals.total_cost_micro = %d, want 1500", report.Totals.TotalCostMicro)
	}

	if got := report.Totals.GetAverageCostMicro(); got != 150 {
		t.Fatalf("平均耗时 = %d, want 150", got)
	}

	if got := report.Totals.GetFailureRate(); got != 0.1 {
		t.Fatalf("失败率 = %v, want 0.1", got)
	}
}

// TestNewReportDoesNotMutateInput 验证入参切片不会被修改。
func TestNewReportDoesNotMutateInput(t *testing.T) {
	snapshots := []Snapshot{
		{Method: "POST", Pattern: "/b"},
		{Method: "GET", Pattern: "/a"},
	}

	_ = NewReport(snapshots)

	if snapshots[0].Pattern != "/b" || snapshots[1].Pattern != "/a" {
		t.Fatal("NewReport 不应修改调用方切片")
	}
}

// TestNewEmptyReport 验证空报告的兜底行为。
func TestNewEmptyReport(t *testing.T) {
	report := NewEmptyReport()

	if report.Routes == nil {
		t.Fatal("Routes 必须为非 nil 切片，避免前端判空")
	}

	if len(report.Routes) != 0 {
		t.Fatalf("routes 数量 = %d, want 0", len(report.Routes))
	}

	if report.Totals.Routes != 0 || report.Totals.Requests != 0 {
		t.Fatalf("totals = %+v, want 全零", report.Totals)
	}

	if report.Totals.GetAverageCostMicro() != 0 || report.Totals.GetFailureRate() != 0 {
		t.Fatal("空汇总的派生指标必须为 0")
	}

	if time.Since(report.GeneratedAt) > time.Minute {
		t.Fatal("GeneratedAt 必须接近当前时间")
	}
}
