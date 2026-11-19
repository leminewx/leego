package metrics

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/leminewx/leego/middleware/collector"
)

// buildReport 构造用于导出测试的报告。
func buildReport() collector.Report {
	return collector.NewReport([]collector.Snapshot{
		{
			Name:    "get-user",
			Method:  http.MethodGet,
			Pattern: "/users/:id",
			Total:   3,
			Failed:  1,
			Metrics: []collector.Metric{
				{Code: 200, Total: 2, MaxCostMicro: 900},
				{Code: 500, Total: 1, MaxCostMicro: 1500},
			},
			TotalCostMicro: 3000,
		},
		{
			Name:           "post-order",
			Method:         http.MethodPost,
			Pattern:        "/orders",
			Total:          1,
			Flights:        2,
			TotalCostMicro: 500,
			Metrics:        []collector.Metric{{Code: 201, Total: 1, MaxCostMicro: 500}},
		},
	})
}

func TestTimeSeriesAggregatesByStep(t *testing.T) {
	series := NewTimeSeries(time.Second, 10)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	series.AppendPoint(Point{At: base, Total: 2, AvgMicro: 100, MaxMicro: 300})
	series.AppendPoint(Point{At: base.Add(100 * time.Millisecond), Total: 1, AvgMicro: 400, MaxMicro: 500})
	series.AppendPoint(Point{At: base.Add(500 * time.Millisecond), Total: 1, AvgMicro: 200, MaxMicro: 200})

	points := series.Query(base.Add(-time.Second), base.Add(time.Second), 100*time.Millisecond)
	if len(points) != 3 {
		t.Fatalf("聚合点数 = %d, want 3: %+v", len(points), points)
	}

	if points[0].Total != 2 || points[0].AvgMicro != 100 || points[0].MaxMicro != 300 {
		t.Fatalf("首个时间片异常: %+v", points[0])
	}
}

func TestTimeSeriesRotatesWindow(t *testing.T) {
	series := NewTimeSeries(30*time.Millisecond, 3)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	for index := 0; index < 5; index++ {
		series.AppendPoint(Point{At: base.Add(time.Duration(index) * 10 * time.Millisecond), Total: 1})
	}

	points := series.Query(base.Add(-time.Hour), base.Add(time.Hour), 10*time.Millisecond)
	if len(points) != 3 {
		t.Fatalf("窗口内点数 = %d, want 3: %+v", len(points), points)
	}

	if !points[0].At.Equal(base.Add(20 * time.Millisecond)) {
		t.Fatalf("最旧时间片 = %s, want %s", points[0].At, base.Add(20*time.Millisecond))
	}
}

func TestTimeSeriesDropsOldPoints(t *testing.T) {
	series := NewTimeSeries(time.Second, 5)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	series.AppendPoint(Point{At: base, Total: 1})
	series.AppendPoint(Point{At: base.Add(-time.Minute), Total: 1})

	points := series.Query(base.Add(-time.Hour), base.Add(time.Hour), time.Second)
	if len(points) != 1 || points[0].Total != 1 {
		t.Fatalf("早于窗口的点应被丢弃: %+v", points)
	}
}

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore(time.Minute, 60)
	snapshot := collector.Snapshot{Total: 4, Failed: 1, TotalCostMicro: 1000}
	if err := store.Append(snapshot); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	now := time.Now()
	points, err := store.Query(now.Add(-time.Minute), now.Add(time.Minute), time.Second)
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}

	if len(points) != 1 || points[0].Total != 4 || points[0].Failed != 1 {
		t.Fatalf("查询结果异常: %+v", points)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestEncodePrometheus(t *testing.T) {
	output := encodePrometheus(buildReport())

	wants := []string{
		"# TYPE leego_http_requests_total counter",
		`leego_http_requests_total{method="GET",pattern="/users/:id",name="get-user",status="200"} 2`,
		`leego_http_requests_total{method="GET",pattern="/users/:id",name="get-user",status="500"} 1`,
		`leego_http_request_duration_seconds_sum{method="POST",pattern="/orders",name="post-order"} 0.000500`,
		`leego_http_requests_in_flight{method="POST",pattern="/orders",name="post-order"} 2`,
		`leego_http_requests_failed_total{method="GET",pattern="/users/:id",name="get-user"} 1`,
	}

	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Fatalf("输出缺少 %q\n%s", want, output)
		}
	}

	// 同一份报告两次编码必须逐字一致。
	if again := encodePrometheus(buildReport()); again != output {
		t.Fatal("Prometheus 输出不稳定")
	}
}

func TestEscapeLabelValue(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		`say "hi"`:     `say \"hi\"`,
		"a\\b":         `a\\b`,
		"line1\nline2": `line1\nline2`,
	}

	for input, want := range cases {
		if got := escapeLabelValue(input); got != want {
			t.Fatalf("escapeLabelValue(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPrometheusHandler(t *testing.T) {
	handler := NewPrometheusHandler(buildReport)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics/prometheus", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q", contentType)
	}

	if !strings.Contains(recorder.Body.String(), "leego_http_requests_total") {
		t.Fatal("缺少指标输出")
	}

	empty := httptest.NewRecorder()
	NewPrometheusHandler(nil).ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/", nil))
	if empty.Code != http.StatusOK {
		t.Fatalf("空数据源状态码 = %d, want 200", empty.Code)
	}
}

func TestFileStoreAppendsAndReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	base := time.Now().Add(-time.Minute)

	// 窗口 1 分钟、分为 60 个桶，桶宽恰为 1 秒，
	// 因此下面两个间隔 1 秒的样本会落入不同的时间片。
	store, err := NewFileStore(path, time.Minute, 60)
	if err != nil {
		t.Fatalf("创建 FileStore 失败: %v", err)
	}

	if err = store.AppendPoint(Point{At: base, Total: 2, AvgMicro: 10, MaxMicro: 20}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if err = store.AppendPoint(Point{At: base.Add(time.Second), Total: 1, AvgMicro: 5}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if err = store.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	reopened, err := NewFileStore(path, time.Minute, 60)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	t.Cleanup(func() {
		_ = reopened.Close()
	})

	points, err := reopened.Query(base.Add(-time.Minute), base.Add(time.Minute), time.Second)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	if len(points) != 2 {
		t.Fatalf("回放点数 = %d, want 2: %+v", len(points), points)
	}

	if points[0].Total != 2 || points[0].AvgMicro != 10 || points[0].MaxMicro != 20 {
		t.Fatalf("首条回放记录异常: %+v", points[0])
	}

	if points[1].Total != 1 || points[1].AvgMicro != 5 {
		t.Fatalf("次条回放记录异常: %+v", points[1])
	}
}

func TestFileStoreTruncatesBrokenTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	valid := "{\"at\":\"2026-11-19T10:00:00Z\",\"total\":3}\n"
	if err := os.WriteFile(path, []byte(valid+"{\"at\":\"2026-11-19T10:00:01Z\""), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	store, err := NewFileStore(path, time.Hour, 60)
	if err != nil {
		t.Fatalf("打开损坏文件失败: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取文件状态失败: %v", err)
	}

	if info.Size() != int64(len(valid)) {
		t.Fatalf("文件大小 = %d, want %d", info.Size(), len(valid))
	}

	if err = store.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
}

func TestFileStoreRotates(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "metrics.jsonl")

	store, err := NewFileStore(path, time.Hour, 60, FileOptions{MaxBytes: 64, MaxFiles: 1})
	if err != nil {
		t.Fatalf("创建 FileStore 失败: %v", err)
	}

	base := time.Now().Add(-time.Minute)
	for index := 0; index < 6; index++ {
		point := Point{At: base.Add(time.Duration(index) * time.Second), Total: 1, AvgMicro: 100}
		if err = store.AppendPoint(point); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	if err = store.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}

	history := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "metrics-") {
			history++
		}
	}

	if history == 0 {
		t.Fatal("未生成轮转文件")
	}

	if history > 1 {
		t.Fatalf("保留历史文件数 = %d, want 1", history)
	}
}

func TestFileStoreRejectsAfterClose(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "metrics.jsonl"), time.Hour, 60)
	if err != nil {
		t.Fatalf("创建 FileStore 失败: %v", err)
	}

	if err = store.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if err = store.AppendPoint(Point{At: time.Now(), Total: 1}); err == nil {
		t.Fatal("关闭后写入应返回错误")
	}

	if _, err = store.Query(time.Now(), time.Now(), time.Second); err == nil {
		t.Fatal("关闭后查询应返回错误")
	}
}

func TestRotateName(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	name := rotateName(filepath.Join("logs", "metrics.jsonl"), at)
	if !strings.HasSuffix(name, "metrics-1700000000.jsonl") {
		t.Fatalf("轮转文件名 = %q", name)
	}
}
