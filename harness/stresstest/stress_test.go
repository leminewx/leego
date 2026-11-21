package stress

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// createTestServer 启动一个用于压测的测试服务器，覆盖成功、失败、回显与慢响应场景。
func createTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ok":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("ok"))
		case "/fail":
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte("boom"))
		case "/slow":
			time.Sleep(100 * time.Millisecond)
			writer.WriteHeader(http.StatusOK)
		default:
			// 回显请求方法、请求头与请求体，用于验证请求构造是否正确。
			body, _ := io.ReadAll(request.Body)
			writer.Header().Set("X-Echo-Method", request.Method)
			writer.Header().Set("X-Echo-Header", request.Header.Get("X-Test-Header"))
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(body)
		}
	}))

	t.Cleanup(server.Close)

	return server
}

// TestNewDefaultConfigUsesDefaults 验证默认配置的取值。
func TestNewDefaultConfigUsesDefaults(t *testing.T) {
	config := NewDefaultConfig("http://127.0.0.1:8080/examples")

	if config.Method != _DEFAULT_METHOD {
		t.Fatalf("Method = %q, want %q", config.Method, _DEFAULT_METHOD)
	}
	if config.URL != "http://127.0.0.1:8080/examples" {
		t.Fatalf("URL = %q", config.URL)
	}
	if config.Concurrent != _DEFAULT_CONCURRENT {
		t.Fatalf("Concurrent = %d, want %d", config.Concurrent, _DEFAULT_CONCURRENT)
	}
	if config.TotalRequests != _DEFAULT_TOTAL_REQUESTS {
		t.Fatalf("TotalRequests = %d, want %d", config.TotalRequests, _DEFAULT_TOTAL_REQUESTS)
	}
	if config.Timeout != _DEFAULT_TIMEOUT {
		t.Fatalf("Timeout = %v, want %v", config.Timeout, _DEFAULT_TIMEOUT)
	}
	if config.Headers == nil {
		t.Fatal("Headers 不应为 nil")
	}
}

// TestConfigCloneIsDeepCopy 验证 Clone 产生深拷贝，Headers 互不影响。
func TestConfigCloneIsDeepCopy(t *testing.T) {
	original := NewDefaultConfig("http://127.0.0.1:8080")
	original.Headers["X-A"] = "1"

	clone := original.Clone()
	clone.Headers["X-A"] = "2"
	clone.Headers["X-B"] = "3"
	clone.Concurrent = 99

	if original.Headers["X-A"] != "1" {
		t.Fatalf("原配置的 Header 被修改: %q", original.Headers["X-A"])
	}
	if _, exists := original.Headers["X-B"]; exists {
		t.Fatal("原配置不应出现克隆后新增的 Header")
	}
	if original.Concurrent != _DEFAULT_CONCURRENT {
		t.Fatalf("原配置 Concurrent 被修改为 %d", original.Concurrent)
	}
}

// TestConfigCloneHandlesNil 验证 nil 配置返回 nil。
func TestConfigCloneHandlesNil(t *testing.T) {
	var config *Config
	if clone := config.Clone(); clone != nil {
		t.Fatalf("nil 配置 Clone 结果 = %+v, want nil", clone)
	}
}

// TestConfigNormalizeFillsDefaults 验证可修复的问题被就地纠正。
func TestConfigNormalizeFillsDefaults(t *testing.T) {
	config := &Config{
		Method:        "  post  ",
		URL:           "  http://127.0.0.1:8080/api  ",
		Concurrent:    0,
		TotalRequests: -1,
		Timeout:       0,
	}

	if err := config.normalize(); err != nil {
		t.Fatalf("规范化失败: %v", err)
	}

	if config.Method != "POST" {
		t.Fatalf("Method = %q, want POST", config.Method)
	}
	if config.URL != "http://127.0.0.1:8080/api" {
		t.Fatalf("URL = %q", config.URL)
	}
	if config.Concurrent != _DEFAULT_CONCURRENT {
		t.Fatalf("Concurrent = %d, want %d", config.Concurrent, _DEFAULT_CONCURRENT)
	}
	if config.TotalRequests != _DEFAULT_TOTAL_REQUESTS {
		t.Fatalf("TotalRequests = %d, want %d", config.TotalRequests, _DEFAULT_TOTAL_REQUESTS)
	}
	if config.Timeout != _DEFAULT_TIMEOUT {
		t.Fatalf("Timeout = %v, want %v", config.Timeout, _DEFAULT_TIMEOUT)
	}
	if config.Headers == nil {
		t.Fatal("Headers 应被初始化")
	}
}

// TestConfigNormalizeDefaultsEmptyMethod 验证空方法回退为 GET。
func TestConfigNormalizeDefaultsEmptyMethod(t *testing.T) {
	config := &Config{URL: "http://127.0.0.1:8080"}
	if err := config.normalize(); err != nil {
		t.Fatalf("规范化失败: %v", err)
	}

	if config.Method != _DEFAULT_METHOD {
		t.Fatalf("Method = %q, want %q", config.Method, _DEFAULT_METHOD)
	}
}

// TestConfigNormalizeRejectsInvalidTarget 验证不可修复的地址问题返回错误。
func TestConfigNormalizeRejectsInvalidTarget(t *testing.T) {
	cases := []struct {
		name   string
		config *Config
	}{
		{name: "nil 配置", config: nil},
		{name: "空地址", config: &Config{URL: "   "}},
		{name: "非法 URL", config: &Config{URL: "http://[::1"}},
		{name: "不支持的协议", config: &Config{URL: "ftp://127.0.0.1/file"}},
		{name: "缺少主机", config: &Config{URL: "http:///path"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.config.normalize(); err == nil {
				t.Fatal("非法配置必须返回错误")
			}
		})
	}
}

// TestNewClientRejectsNilConfig 验证 nil 配置被拒绝。
func TestNewClientRejectsNilConfig(t *testing.T) {
	if _, err := NewClient(nil); !errors.Is(err, errNilConfig) {
		t.Fatalf("err = %v, want errNilConfig", err)
	}
}

// TestNewClientRejectsInvalidConfig 验证非法地址在创建阶段即被拒绝。
func TestNewClientRejectsInvalidConfig(t *testing.T) {
	config := NewDefaultConfig("")
	if _, err := NewClient(config); err == nil {
		t.Fatal("空地址必须返回错误")
	}
}

// TestNewClientNormalizesConfig 验证创建时内部保存的是规范化后的副本。
func TestNewClientNormalizesConfig(t *testing.T) {
	config := &Config{
		URL:           "  http://127.0.0.1:8080  ",
		Method:        " get ",
		Concurrent:    0,
		TotalRequests: 0,
		Timeout:       0,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	snapshot := client.GetConfig()
	if snapshot.Method != http.MethodGet {
		t.Fatalf("Method = %q, want GET", snapshot.Method)
	}
	if snapshot.URL != "http://127.0.0.1:8080" {
		t.Fatalf("URL = %q", snapshot.URL)
	}
	if snapshot.Concurrent != _DEFAULT_CONCURRENT || snapshot.TotalRequests != _DEFAULT_TOTAL_REQUESTS {
		t.Fatalf("并发/总数未规范化: %+v", snapshot)
	}
	if snapshot.Timeout != _DEFAULT_TIMEOUT {
		t.Fatalf("Timeout = %v, want %v", snapshot.Timeout, _DEFAULT_TIMEOUT)
	}

	// 修改快照不应影响客户端内部配置。
	snapshot.Headers["X-Leak"] = "1"
	if _, exists := client.GetConfig().Headers["X-Leak"]; exists {
		t.Fatal("GetConfig 必须返回副本")
	}
}

// TestClientSetTransport 验证替换连接池与忽略 nil。
func TestClientSetTransport(t *testing.T) {
	client, err := NewClient(NewDefaultConfig("http://127.0.0.1:8080"))
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	original := client.GetHTTPClient().Transport
	client.SetTransport(nil)
	if client.GetHTTPClient().Transport != original {
		t.Fatal("nil 连接池不应被替换")
	}

	replacement := &http.Transport{}
	client.SetTransport(replacement)
	if client.GetHTTPClient().Transport != replacement {
		t.Fatal("自定义连接池未被应用")
	}
	if client.transport != replacement {
		t.Fatal("内部连接池引用未更新")
	}
}

// TestClientRunCountsAllRequests 验证实际请求数与配置一致（含余数分配）。
func TestClientRunCountsAllRequests(t *testing.T) {
	server := createTestServer(t)
	config := NewDefaultConfig(server.URL + "/ok")
	config.Concurrent = 3
	config.TotalRequests = 10

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	snapshot := client.Run()

	if snapshot.TotalRequests != 10 {
		t.Fatalf("TotalRequests = %d, want 10", snapshot.TotalRequests)
	}
	if snapshot.SuccessCount != 10 {
		t.Fatalf("SuccessCount = %d, want 10", snapshot.SuccessCount)
	}
	if snapshot.FailedCount != 0 || snapshot.ErrorCount != 0 {
		t.Fatalf("失败统计 = %d/%d, want 0/0", snapshot.FailedCount, snapshot.ErrorCount)
	}
	if snapshot.SuccessRate != 1 {
		t.Fatalf("SuccessRate = %v, want 1", snapshot.SuccessRate)
	}
	if snapshot.StatusCodes[http.StatusOK] != 10 {
		t.Fatalf("状态码分布 = %v, want 200x10", snapshot.StatusCodes)
	}
	if snapshot.Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", snapshot.Duration)
	}
}

// TestClientRunCountsServerErrorsAsFailures 验证 5xx 计入失败但不计入网络错误。
func TestClientRunCountsServerErrorsAsFailures(t *testing.T) {
	server := createTestServer(t)
	config := NewDefaultConfig(server.URL + "/fail")
	config.Concurrent = 2
	config.TotalRequests = 4

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	snapshot := client.Run()

	if snapshot.TotalRequests != 4 || snapshot.FailedCount != 4 || snapshot.SuccessCount != 0 {
		t.Fatalf("统计结果 = %+v", snapshot)
	}
	if snapshot.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0（服务端 5xx 不是网络错误）", snapshot.ErrorCount)
	}
	if snapshot.StatusCodes[http.StatusInternalServerError] != 4 {
		t.Fatalf("状态码分布 = %v, want 500x4", snapshot.StatusCodes)
	}
}

// TestClientRunCountsNetworkErrors 验证超时等网络错误计入错误数且状态码为 0。
func TestClientRunCountsNetworkErrors(t *testing.T) {
	server := createTestServer(t)
	config := NewDefaultConfig(server.URL + "/slow")
	config.Concurrent = 2
	config.TotalRequests = 2
	config.Timeout = 10 * time.Millisecond

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	snapshot := client.Run()

	if snapshot.TotalRequests != 2 || snapshot.ErrorCount != 2 || snapshot.FailedCount != 2 {
		t.Fatalf("统计结果 = %+v", snapshot)
	}
	if snapshot.StatusCodes[0] != 2 {
		t.Fatalf("状态码分布 = %v, want 0x2", snapshot.StatusCodes)
	}
}

// TestClientRunSendsMethodHeadersAndBody 验证请求方法、请求头与请求体都被正确发送。
func TestClientRunSendsMethodHeadersAndBody(t *testing.T) {
	var (
		receivedMethod string
		receivedHeader string
		receivedBody   string
		lock           sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)

		lock.Lock()
		receivedMethod = request.Method
		receivedHeader = request.Header.Get("X-Test-Header")
		receivedBody = string(body)
		lock.Unlock()

		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := NewDefaultConfig(server.URL + "/echo")
	config.Method = "post"
	config.Body = `{"name":"leego"}`
	config.Headers["X-Test-Header"] = "stress"
	config.TotalRequests = 1

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	snapshot := client.Run()
	if snapshot.SuccessCount != 1 {
		t.Fatalf("SuccessCount = %d, want 1", snapshot.SuccessCount)
	}

	lock.Lock()
	defer lock.Unlock()

	if receivedMethod != http.MethodPost {
		t.Fatalf("Method = %q, want POST", receivedMethod)
	}
	if receivedHeader != "stress" {
		t.Fatalf("X-Test-Header = %q, want stress", receivedHeader)
	}
	if receivedBody != `{"name":"leego"}` {
		t.Fatalf("Body = %q", receivedBody)
	}
}

// TestClientRunKeepsAliveConnections 验证开启 KeepAlive 后连接可复用且压测正常完成。
func TestClientRunKeepsAliveConnections(t *testing.T) {
	server := createTestServer(t)
	config := NewDefaultConfig(server.URL + "/ok")
	config.KeepAlive = true
	config.Concurrent = 4
	config.TotalRequests = 20

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	if client.transport.DisableKeepAlives {
		t.Fatal("KeepAlive=true 时不应禁用连接复用")
	}

	snapshot := client.Run()
	if snapshot.SuccessCount != 20 {
		t.Fatalf("SuccessCount = %d, want 20", snapshot.SuccessCount)
	}
}

// TestClientRunConcurrently 验证同一个客户端可被多协程并发执行压测。
func TestClientRunConcurrently(t *testing.T) {
	server := createTestServer(t)
	config := NewDefaultConfig(server.URL + "/ok")
	config.Concurrent = 2
	config.TotalRequests = 6
	config.KeepAlive = true

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	defer client.Close()

	const runners = 4
	snapshots := make([]MetricsSnapshot, runners)

	var waitGroup sync.WaitGroup
	for index := 0; index < runners; index++ {
		waitGroup.Add(1)
		go func(slot int) {
			defer waitGroup.Done()
			snapshots[slot] = client.Run()
		}(index)
	}
	waitGroup.Wait()

	for index, snapshot := range snapshots {
		if snapshot.TotalRequests != 6 || snapshot.SuccessCount != 6 {
			t.Fatalf("第 %d 次压测结果 = %+v", index, snapshot)
		}
	}
}

// TestClientCloseIsIdempotent 验证 Close 可重复调用。
func TestClientCloseIsIdempotent(t *testing.T) {
	client, err := NewClient(NewDefaultConfig("http://127.0.0.1:8080"))
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	if err = client.Close(); err != nil {
		t.Fatalf("首次关闭失败: %v", err)
	}
	if err = client.Close(); err != nil {
		t.Fatalf("重复关闭失败: %v", err)
	}
}

// TestNewLatenciesWithCapacity 验证容量参数与负容量处理。
func TestNewLatenciesWithCapacity(t *testing.T) {
	latencies := NewLatenciesWithCapacity(8)
	if latencies.GetLen() != 0 {
		t.Fatalf("初始样本数 = %d, want 0", latencies.GetLen())
	}

	negative := NewLatenciesWithCapacity(-1)
	negative.Append(time.Millisecond)
	if negative.GetLen() != 1 {
		t.Fatalf("负容量采集器样本数 = %d, want 1", negative.GetLen())
	}
}

// TestLatenciesComputesStatistics 验证样本统计与分位数（最近秩法）。
func TestLatenciesComputesStatistics(t *testing.T) {
	latencies := NewLatencies()
	for _, value := range []time.Duration{30, 10, 20, 40, 50, 60, 70, 80, 90, 100} {
		latencies.Append(value * time.Millisecond)
	}

	if got := latencies.GetLen(); got != 10 {
		t.Fatalf("GetLen() = %d, want 10", got)
	}
	if got := latencies.CountTotal(); got != 550*time.Millisecond {
		t.Fatalf("CountTotal() = %v, want 550ms", got)
	}
	if got := latencies.GetAverage(); got != 55*time.Millisecond {
		t.Fatalf("GetAverage() = %v, want 55ms", got)
	}
	if got := latencies.GetMin(); got != 10*time.Millisecond {
		t.Fatalf("GetMin() = %v, want 10ms", got)
	}
	if got := latencies.GetMax(); got != 100*time.Millisecond {
		t.Fatalf("GetMax() = %v, want 100ms", got)
	}
	if got := latencies.GetPercentile(50); got != 50*time.Millisecond {
		t.Fatalf("P50 = %v, want 50ms", got)
	}
	if got := latencies.GetPercentile(90); got != 90*time.Millisecond {
		t.Fatalf("P90 = %v, want 90ms", got)
	}
	if got := latencies.GetPercentile(99); got != 100*time.Millisecond {
		t.Fatalf("P99 = %v, want 100ms", got)
	}
}

// TestLatenciesClampsPercentile 验证越界分位数被裁剪到合法区间。
func TestLatenciesClampsPercentile(t *testing.T) {
	latencies := NewLatencies()
	latencies.Append(10 * time.Millisecond)
	latencies.Append(20 * time.Millisecond)

	if got := latencies.GetPercentile(0); got != 10*time.Millisecond {
		t.Fatalf("P0 应裁剪为 P1，得到 %v", got)
	}
	if got := latencies.GetPercentile(-5); got != 10*time.Millisecond {
		t.Fatalf("负分位应裁剪为 P1，得到 %v", got)
	}
	if got := latencies.GetPercentile(101); got != 20*time.Millisecond {
		t.Fatalf("超界分位应裁剪为 P100，得到 %v", got)
	}
}

// TestLatenciesEmptyStatistics 验证空采集器的统计值为 0。
func TestLatenciesEmptyStatistics(t *testing.T) {
	latencies := NewLatencies()

	if latencies.CountTotal() != 0 || latencies.GetAverage() != 0 {
		t.Fatal("空采集器的总和与均值应为 0")
	}
	if latencies.GetMin() != 0 || latencies.GetMax() != 0 || latencies.GetPercentile(50) != 0 {
		t.Fatal("空采集器的极值与分位应为 0")
	}
}

// TestLatenciesMerge 验证合并样本（含 nil 与空采集器）。
func TestLatenciesMerge(t *testing.T) {
	target := NewLatencies()
	target.Append(30 * time.Millisecond)

	target.Merge(nil)
	target.Merge(NewLatencies())
	if target.GetLen() != 1 {
		t.Fatalf("合并空采集器后样本数 = %d, want 1", target.GetLen())
	}

	other := NewLatencies()
	other.Append(10 * time.Millisecond)
	other.Append(20 * time.Millisecond)
	target.Merge(other)

	if target.GetLen() != 3 {
		t.Fatalf("合并后样本数 = %d, want 3", target.GetLen())
	}
	if got := target.GetMin(); got != 10*time.Millisecond {
		t.Fatalf("GetMin() = %v, want 10ms", got)
	}
}

// TestLatenciesGetLatenciesReturnsCopy 验证样本副本语义。
func TestLatenciesGetLatenciesReturnsCopy(t *testing.T) {
	latencies := NewLatencies()
	latencies.Append(time.Millisecond)

	values := latencies.GetLatencies()
	values[0] = time.Hour

	if got := latencies.GetLatencies()[0]; got != time.Millisecond {
		t.Fatalf("内部样本被外部修改: %v", got)
	}
}

// TestLatenciesResetAndSort 验证重置与幂等排序。
func TestLatenciesResetAndSort(t *testing.T) {
	latencies := NewLatencies()
	latencies.Append(3 * time.Millisecond)
	latencies.Append(1 * time.Millisecond)
	latencies.Append(2 * time.Millisecond)

	latencies.Sort()
	latencies.Sort() // 已排序时重复调用不应改变顺序

	values := latencies.GetLatencies()
	for index := 1; index < len(values); index++ {
		if values[index-1] > values[index] {
			t.Fatalf("样本未按升序排列: %v", values)
		}
	}

	latencies.Reset()
	if latencies.GetLen() != 0 {
		t.Fatalf("Reset 后样本数 = %d, want 0", latencies.GetLen())
	}

	latencies.Append(5 * time.Millisecond)
	if got := latencies.GetMax(); got != 5*time.Millisecond {
		t.Fatalf("Reset 后追加的样本 = %v", got)
	}
}

// TestMetricsRecordClassifiesResults 验证成功、4xx/5xx 与网络错误的分类。
func TestMetricsRecordClassifiesResults(t *testing.T) {
	metrics := NewMetrics()

	metrics.Record(http.StatusOK, time.Millisecond, nil)
	metrics.Record(http.StatusNoContent, time.Millisecond, nil)
	metrics.Record(http.StatusNotFound, time.Millisecond, nil)
	metrics.Record(http.StatusInternalServerError, time.Millisecond, nil)
	metrics.Record(0, time.Millisecond, errors.New("connection refused"))

	snapshot := metrics.GetSnapshot()

	if snapshot.TotalRequests != 5 {
		t.Fatalf("TotalRequests = %d, want 5", snapshot.TotalRequests)
	}
	if snapshot.SuccessCount != 2 {
		t.Fatalf("SuccessCount = %d, want 2", snapshot.SuccessCount)
	}
	if snapshot.FailedCount != 3 {
		t.Fatalf("FailedCount = %d, want 3", snapshot.FailedCount)
	}
	if snapshot.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %d, want 1", snapshot.ErrorCount)
	}
	if snapshot.SuccessRate != 0.4 {
		t.Fatalf("SuccessRate = %v, want 0.4", snapshot.SuccessRate)
	}
	if snapshot.StatusCodes[0] != 1 || snapshot.StatusCodes[http.StatusOK] != 1 || snapshot.StatusCodes[http.StatusNotFound] != 1 {
		t.Fatalf("状态码分布 = %v", snapshot.StatusCodes)
	}
}

// TestMetricsFinishIsIdempotent 验证结束时间只记录一次。
func TestMetricsFinishIsIdempotent(t *testing.T) {
	metrics := NewMetrics()
	metrics.Record(http.StatusOK, time.Millisecond, nil)

	metrics.Finish()
	first := metrics.GetSnapshot()

	time.Sleep(10 * time.Millisecond)
	metrics.Finish()
	second := metrics.GetSnapshot()

	if !first.EndTime.Equal(second.EndTime) {
		t.Fatalf("重复 Finish 修改了结束时间: %v != %v", first.EndTime, second.EndTime)
	}
	if second.Duration != first.Duration {
		t.Fatalf("重复 Finish 修改了耗时: %v != %v", second.Duration, first.Duration)
	}
}

// TestMetricsSnapshotIsIndependent 验证快照中的状态码映射是副本。
func TestMetricsSnapshotIsIndependent(t *testing.T) {
	metrics := NewMetrics()
	metrics.Record(http.StatusOK, time.Millisecond, nil)

	snapshot := metrics.GetSnapshot()
	snapshot.StatusCodes[http.StatusOK] = 999

	if got := metrics.GetSnapshot().StatusCodes[http.StatusOK]; got != 1 {
		t.Fatalf("内部状态码统计被外部修改: %d", got)
	}
}

// TestFormatReportContainsAllSections 验证报告包含全部统计项，且状态码按升序输出。
func TestFormatReportContainsAllSections(t *testing.T) {
	metrics := NewMetrics()
	metrics.Record(http.StatusOK, 2*time.Millisecond, nil)
	metrics.Record(http.StatusOK, 4*time.Millisecond, nil)
	metrics.Record(http.StatusInternalServerError, 6*time.Millisecond, nil)
	metrics.Record(0, 8*time.Millisecond, errors.New("timeout"))
	metrics.Finish()

	report := metrics.GetReport()

	keywords := []string{
		"性能测试报告", "开始时间", "结束时间", "测试耗时", "总请求数: 4",
		"成功请求: 2", "失败请求: 2", "网络错误: 1", "成功率: 50.00%",
		"QPS", "最小延迟", "最大延迟", "平均延迟", "P50", "P90", "P95", "P99",
		"状态码分布", "200: 2", "500: 1", "0: 1",
	}
	for _, keyword := range keywords {
		if !strings.Contains(report, keyword) {
			t.Fatalf("报告缺少 %q:\n%s", keyword, report)
		}
	}

	// 状态码应按升序输出：0 → 200 → 500。
	indexOfError := strings.Index(report, "\n  0: ")
	indexOfOK := strings.Index(report, "\n  200: ")
	indexOfServerError := strings.Index(report, "\n  500: ")
	if !(indexOfError >= 0 && indexOfOK > indexOfError && indexOfServerError > indexOfOK) {
		t.Fatalf("状态码未按升序输出:\n%s", report)
	}
}

// TestFormatReportHandlesEmptySnapshot 验证空快照不会出现除零或异常输出。
func TestFormatReportHandlesEmptySnapshot(t *testing.T) {
	report := FormatReport(MetricsSnapshot{})
	if !strings.Contains(report, "总请求数: 0") {
		t.Fatalf("空快照报告异常:\n%s", report)
	}
	if !strings.Contains(report, "成功率: 0.00%") {
		t.Fatalf("空快照成功率异常:\n%s", report)
	}
}

// TestMetricsRecordConcurrently 验证指标收集器在并发记录下统计准确。
func TestMetricsRecordConcurrently(t *testing.T) {
	const goroutines, perGoroutine = 16, 100

	metrics := NewMetrics()

	var waitGroup sync.WaitGroup
	for index := 0; index < goroutines; index++ {
		waitGroup.Add(1)
		go func(slot int) {
			defer waitGroup.Done()
			for round := 0; round < perGoroutine; round++ {
				if slot%2 == 0 {
					metrics.Record(http.StatusOK, time.Millisecond, nil)
				} else {
					metrics.Record(http.StatusInternalServerError, time.Millisecond, nil)
				}
			}
		}(index)
	}
	waitGroup.Wait()

	metrics.Finish()
	snapshot := metrics.GetSnapshot()

	expected := int64(goroutines * perGoroutine)
	if snapshot.TotalRequests != expected {
		t.Fatalf("TotalRequests = %d, want %d", snapshot.TotalRequests, expected)
	}
	if snapshot.SuccessCount != expected/2 || snapshot.FailedCount != expected/2 {
		t.Fatalf("成功/失败 = %d/%d, want %d/%d", snapshot.SuccessCount, snapshot.FailedCount, expected/2, expected/2)
	}
}

// BenchmarkLatenciesAppend 测量延迟样本追加的耗时。
func BenchmarkLatenciesAppend(b *testing.B) {
	latencies := NewLatenciesWithCapacity(b.N)
	latency := time.Millisecond

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		latencies.Append(latency)
	}
}

// BenchmarkLatenciesGetPercentile 测量已排序样本的分位数计算耗时。
func BenchmarkLatenciesGetPercentile(b *testing.B) {
	latencies := NewLatenciesWithCapacity(1000)
	for i := 0; i < 1000; i++ {
		latencies.Append(time.Duration(i) * time.Microsecond)
	}
	latencies.Sort()

	b.ReportAllocs()

	for b.Loop() {
		_ = latencies.GetPercentile(99)
	}
}

// BenchmarkMetricsRecord 测量并发安全的指标记录耗时。
func BenchmarkMetricsRecord(b *testing.B) {
	metrics := NewMetrics()
	latency := time.Millisecond

	b.ReportAllocs()

	for b.Loop() {
		metrics.Record(http.StatusOK, latency, nil)
	}
}
