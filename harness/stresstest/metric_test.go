package stress

import (
	"strings"
	"testing"
	"time"
)

// TestFormatReportSortsStatusCodes 验证状态码分布按升序输出。
func TestFormatReportSortsStatusCodes(t *testing.T) {
	snapshot := MetricsSnapshot{
		TotalRequests: 3,
		StatusCodes:   map[int]int64{500: 1, 200: 2},
	}
	report := FormatReport(snapshot)

	okIndex := strings.Index(report, "\n  200: 2")
	errorIndex := strings.Index(report, "\n  500: 1")
	if okIndex < 0 || errorIndex < 0 {
		t.Fatalf("报告缺少状态码分布:\n%s", report)
	}

	if okIndex > errorIndex {
		t.Fatalf("状态码未按升序输出:\n%s", report)
	}
}

// TestFormatReportWithoutRequests 验证空快照不会除零且保留标题。
func TestFormatReportWithoutRequests(t *testing.T) {
	report := FormatReport(MetricsSnapshot{})

	if !strings.Contains(report, "性能测试报告") || !strings.Contains(report, "状态码分布:") {
		t.Fatalf("报告结构异常:\n%s", report)
	}

	if strings.Contains(report, "  %") {
		t.Fatalf("状态码比例不应出现异常格式:\n%s", report)
	}
}

// TestMetricsGetReportMatchesFormatReport 验证 GetReport 与 FormatReport 输出一致。
func TestMetricsGetReportMatchesFormatReport(t *testing.T) {
	metrics := NewMetrics()
	metrics.Record(200, time.Millisecond, nil)
	metrics.Record(500, 2*time.Millisecond, nil)
	metrics.Finish()

	if report := metrics.GetReport(); report != FormatReport(metrics.GetSnapshot()) {
		t.Fatal("GetReport 与 FormatReport 输出不一致")
	}
}
