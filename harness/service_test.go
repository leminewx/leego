package harness_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego/harness"
)

// testResponse 汇总一次测试请求的结果。
type testResponse struct {
	status int
	body   []byte
	header http.Header
}

// newTestService 启动一套隔离的体验服务。
//
// 数据目录使用 t.TempDir()，访问日志关闭以减少测试噪声；
// 返回的 httptest.Server 已注册清理，Shutdown 会关闭内部上游与后台协程。
func newTestService(t *testing.T, options ...harness.Option) (*harness.Service, *httptest.Server) {
	t.Helper()

	base := []harness.Option{
		harness.UseDataDir(t.TempDir()),
		harness.UseLogging(false),
	}

	service, err := harness.NewService(append(base, options...)...)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if shutdownErr := service.Shutdown(ctx); shutdownErr != nil {
			t.Errorf("Shutdown() error = %v", shutdownErr)
		}
	})

	server := httptest.NewServer(service.GetHandler())
	t.Cleanup(server.Close)

	return service, server
}

// newClient 创建带 Cookie 容器的测试客户端。
func newClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 Cookie 容器失败: %v", err)
	}

	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

// do 执行一次请求并读取完整响应。
func do(t *testing.T, client *http.Client, method, target string, body []byte, headers map[string]string) testResponse {
	t.Helper()

	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}

	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s 失败: %v", method, target, err)
	}

	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	return testResponse{status: response.StatusCode, body: payload, header: response.Header}
}

// decodeJSON 把响应体解析为目标结构。
func decodeJSON(t *testing.T, payload []byte, target any) {
	t.Helper()

	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("解析 JSON 失败: %v（响应体 %s）", err, payload)
	}
}

func TestServiceServesIndexAndHello(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	index := do(t, client, http.MethodGet, server.URL+"/", nil, nil)
	if index.status != http.StatusOK {
		t.Fatalf("首页状态码 = %d", index.status)
	}

	if contentType := index.header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("首页 Content-Type = %q", contentType)
	}

	page := string(index.body)
	for _, want := range []string{
		"leego harness",
		`class="sidebar"`,
		`data-panel="upload"`,
		`id="panel-overview"`,
		"/openapi",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("首页缺少 %q", want)
		}
	}

	hello := do(t, client, http.MethodGet, server.URL+"/api/hello", nil, nil)
	if hello.status != http.StatusOK {
		t.Fatalf("hello 状态码 = %d: %s", hello.status, hello.body)
	}

	var payload struct {
		Service string            `json:"service"`
		Go      string            `json:"go"`
		Config  map[string]any    `json:"config"`
		Links   map[string]string `json:"links"`
	}
	decodeJSON(t, hello.body, &payload)

	if payload.Service != "leego-harness" {
		t.Fatalf("服务名 = %q", payload.Service)
	}

	if payload.Go == "" || payload.Config["rate_limit"] == nil {
		t.Fatalf("概览字段缺失: %+v", payload)
	}

	if payload.Links["openapi"] != "/openapi" {
		t.Fatalf("平台链接异常: %+v", payload.Links)
	}
}

func TestServiceSessionLifecycle(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	loginBody, err := json.Marshal(map[string]string{"name": "gopher"})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	login := do(t, client, http.MethodPost, server.URL+"/api/session/login", loginBody, map[string]string{
		"Content-Type": "application/json",
	})
	if login.status != http.StatusOK {
		t.Fatalf("登录状态码 = %d: %s", login.status, login.body)
	}

	var loginPayload struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeJSON(t, login.body, &loginPayload)

	if loginPayload.ID == "" || loginPayload.Name != "gopher" {
		t.Fatalf("登录响应异常: %+v", loginPayload)
	}

	current := do(t, client, http.MethodGet, server.URL+"/api/session/current", nil, nil)
	if current.status != http.StatusOK {
		t.Fatalf("会话查询状态码 = %d: %s", current.status, current.body)
	}

	var currentPayload struct {
		ID   string `json:"id"`
		New  bool   `json:"new"`
		Name string `json:"name"`
	}
	decodeJSON(t, current.body, &currentPayload)

	if currentPayload.ID != loginPayload.ID {
		t.Fatalf("会话 ID 未保持: %q != %q", currentPayload.ID, loginPayload.ID)
	}

	if currentPayload.New {
		t.Fatal("已持久化的会话不应被标记为新会话")
	}

	if currentPayload.Name != "gopher" {
		t.Fatalf("会话数据丢失: %+v", currentPayload)
	}

	logout := do(t, client, http.MethodPost, server.URL+"/api/session/logout", nil, nil)
	if logout.status != http.StatusOK {
		t.Fatalf("退出状态码 = %d: %s", logout.status, logout.body)
	}

	after := do(t, client, http.MethodGet, server.URL+"/api/session/current", nil, nil)
	var afterPayload struct {
		Name any `json:"name"`
	}
	decodeJSON(t, after.body, &afterPayload)

	if afterPayload.Name != nil {
		t.Fatalf("退出后仍能读到会话数据: %v", afterPayload.Name)
	}
}

func TestServiceCacheHit(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	first := do(t, client, http.MethodGet, server.URL+"/api/cache/timestamp", nil, nil)
	if first.status != http.StatusOK {
		t.Fatalf("首次缓存请求状态码 = %d: %s", first.status, first.body)
	}

	if cacheState := first.header.Get("X-Cache"); cacheState != "MISS" {
		t.Fatalf("首次请求 X-Cache = %q, want MISS", cacheState)
	}

	second := do(t, client, http.MethodGet, server.URL+"/api/cache/timestamp", nil, nil)
	if second.status != http.StatusOK {
		t.Fatalf("二次缓存请求状态码 = %d: %s", second.status, second.body)
	}

	if cacheState := second.header.Get("X-Cache"); cacheState != "HIT" {
		t.Fatalf("二次请求 X-Cache = %q, want HIT", cacheState)
	}

	if !bytes.Equal(first.body, second.body) {
		t.Fatalf("命中缓存后响应体不一致:\n%s\n%s", first.body, second.body)
	}
}

func TestServiceRateLimit(t *testing.T) {
	_, server := newTestService(t, harness.UseRateLimit(2, 2))
	client := newClient(t)

	var allowed atomic.Int64
	var limited atomic.Int64

	for index := 0; index < 12; index++ {
		response := do(t, client, http.MethodGet, server.URL+"/api/limited/ping", nil, nil)
		switch response.status {
		case http.StatusOK:
			allowed.Add(1)
		case http.StatusTooManyRequests:
			limited.Add(1)
		default:
			t.Fatalf("非预期状态码 = %d: %s", response.status, response.body)
		}
	}

	if allowed.Load() == 0 {
		t.Fatal("限流器未放行任何请求")
	}

	if limited.Load() == 0 {
		t.Fatalf("限流器未触发：allowed=%d", allowed.Load())
	}

	if allowed.Load()+limited.Load() != 12 {
		t.Fatalf("统计异常: allowed=%d limited=%d", allowed.Load(), limited.Load())
	}
}

func TestServiceUploadRoundTrip(t *testing.T) {
	_, server := newTestService(t, harness.UseChunkSize(8))
	client := newClient(t)

	content := []byte("leego harness upload demo")
	initBody, err := json.Marshal(map[string]any{
		"name": "demo.txt",
		"size": len(content),
	})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	init := do(t, client, http.MethodPost, server.URL+"/api/upload?action=init", initBody, map[string]string{
		"Content-Type": "application/json",
	})
	if init.status != http.StatusOK {
		t.Fatalf("初始化上传状态码 = %d: %s", init.status, init.body)
	}

	var session struct {
		ID       string `json:"id"`
		PartSize int64  `json:"part_size"`
	}
	decodeJSON(t, init.body, &session)

	if session.ID == "" || session.PartSize != 8 {
		t.Fatalf("上传会话异常: %+v", session)
	}

	partSize := int(session.PartSize)
	for offset, index := 0, 0; offset < len(content); index++ {
		end := offset + partSize
		if end > len(content) {
			end = len(content)
		}

		target := server.URL + "/api/upload?action=part&id=" + session.ID + "&index=" + itoa(index)
		response := do(t, client, http.MethodPut, target, content[offset:end], nil)
		if response.status != http.StatusOK {
			t.Fatalf("分片 %d 上传失败: %d %s", index, response.status, response.body)
		}

		offset = end
	}

	complete := do(t, client, http.MethodPost, server.URL+"/api/upload?action=complete&id="+session.ID, nil, nil)
	if complete.status != http.StatusOK {
		t.Fatalf("合并状态码 = %d: %s", complete.status, complete.body)
	}

	var completed struct {
		Path string `json:"path"`
	}
	decodeJSON(t, complete.body, &completed)

	if !strings.HasPrefix(completed.Path, "files/") {
		t.Fatalf("合并结果路径异常: %q", completed.Path)
	}

	download := do(t, client, http.MethodGet, server.URL+"/api/upload/download/"+completed.Path, nil, nil)
	if download.status != http.StatusOK {
		t.Fatalf("下载状态码 = %d: %s", download.status, download.body)
	}

	if !bytes.Equal(download.body, content) {
		t.Fatalf("下载内容不一致: %q", download.body)
	}
}

func TestServiceGatewayRouting(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	stable := do(t, client, http.MethodGet, server.URL+"/api/gateway/demo", nil, nil)
	if stable.status != http.StatusOK {
		t.Fatalf("代理状态码 = %d: %s", stable.status, stable.body)
	}

	var stablePayload struct {
		Upstream string `json:"upstream"`
		Path     string `json:"path"`
	}
	decodeJSON(t, stable.body, &stablePayload)

	if stablePayload.Upstream != "stable" && stablePayload.Upstream != "canary" {
		t.Fatalf("未命中演示上游: %+v", stablePayload)
	}

	if stablePayload.Path != "/api/gateway/demo" {
		t.Fatalf("代理路径被改写: %q", stablePayload.Path)
	}

	for index := 0; index < 5; index++ {
		response := do(t, client, http.MethodGet, server.URL+"/api/gateway/demo", nil, map[string]string{
			"X-Leego-Canary": "v2",
		})

		var payload struct {
			Upstream string `json:"upstream"`
		}
		decodeJSON(t, response.body, &payload)

		if payload.Upstream != "canary" {
			t.Fatalf("灰度流量未命中 canary: %+v", payload)
		}
	}
}

func TestServicePlatformRoutes(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	// 先产生一次业务请求，让指标与链路有数据。
	do(t, client, http.MethodGet, server.URL+"/api/hello", nil, nil)

	metrics := do(t, client, http.MethodGet, server.URL+"/metrics", nil, nil)
	if metrics.status != http.StatusOK {
		t.Fatalf("指标状态码 = %d: %s", metrics.status, metrics.body)
	}

	prometheus := do(t, client, http.MethodGet, server.URL+"/metrics/prometheus", nil, nil)
	if prometheus.status != http.StatusOK {
		t.Fatalf("Prometheus 状态码 = %d: %s", prometheus.status, prometheus.body)
	}

	document := do(t, client, http.MethodGet, server.URL+"/openapi/openapi.json", nil, nil)
	if document.status != http.StatusOK {
		t.Fatalf("OpenAPI 状态码 = %d: %s", document.status, document.body)
	}

	var spec struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
		Paths      map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	decodeJSON(t, document.body, &spec)

	if _, ok := spec.Paths["/api/hello"]; !ok {
		t.Fatalf("OpenAPI 未收录 /api/hello: %v", spec.Paths)
	}

	if _, ok := spec.Paths["/api/upload/download/{filepath}"]; !ok {
		t.Fatalf("OpenAPI 未转换路径参数: %v", spec.Paths)
	}

	if len(spec.Servers) == 0 || len(spec.Tags) == 0 {
		t.Fatalf("OpenAPI 缺少顶层元数据: servers=%v tags=%v", spec.Servers, spec.Tags)
	}

	if _, ok := spec.Components.Schemas["HarnessOverview"]; !ok {
		t.Fatalf("OpenAPI 缺少命名模式: %v", spec.Components.Schemas)
	}

	text := string(document.body)
	for _, want := range []string{
		`"summary": "服务概览"`,
		`"$ref": "#/components/schemas/HarnessOverview"`,
		`"requestBody"`,
		`"X-Leego-Canary"`,
		`"text/event-stream"`,
		`"404"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("OpenAPI 缺少结构化字段 %q", want)
		}
	}

	dashboard := do(t, client, http.MethodGet, server.URL+"/dashboard", nil, nil)
	if dashboard.status != http.StatusOK {
		t.Fatalf("分析后台状态码 = %d: %s", dashboard.status, dashboard.body)
	}

	if contentType := dashboard.header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("分析后台 Content-Type = %q", contentType)
	}
}

func TestServiceJobsAndTraces(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	jobs := do(t, client, http.MethodGet, server.URL+"/api/jobs", nil, nil)
	if jobs.status != http.StatusOK {
		t.Fatalf("任务统计状态码 = %d: %s", jobs.status, jobs.body)
	}

	var jobsPayload struct {
		Workers int `json:"workers"`
		Stats   struct {
			Started bool `json:"Started"`
		} `json:"stats"`
	}
	decodeJSON(t, jobs.body, &jobsPayload)
	if jobsPayload.Workers != 2 || !jobsPayload.Stats.Started {
		t.Fatalf("任务统计异常: %+v", jobsPayload)
	}

	// 触发一次业务请求，等待导出器按刷新间隔落盘。
	do(t, client, http.MethodGet, server.URL+"/api/hello", nil, nil)

	deadline := time.Now().Add(3 * time.Second)
	for {
		traces := do(t, client, http.MethodGet, server.URL+"/api/traces", nil, nil)
		if traces.status != http.StatusOK {
			t.Fatalf("链路状态码 = %d: %s", traces.status, traces.body)
		}

		var payload struct {
			Total int `json:"total"`
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		decodeJSON(t, traces.body, &payload)

		if payload.Total > 0 && len(payload.Items) > 0 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("等待链路导出超时: %s", traces.body)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func TestServiceEventsStream(t *testing.T) {
	_, server := newTestService(t)
	client := newClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("建立 SSE 连接失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE 状态码 = %d", response.StatusCode)
	}

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "hello") {
			return
		}

		if ctx.Err() != nil {
			break
		}
	}

	t.Fatalf("未收到 hello 事件: %v", scanner.Err())
}

func TestServiceRunAndShutdown(t *testing.T) {
	service, err := harness.NewService(
		harness.UseAddr("127.0.0.1:0"),
		harness.UseDataDir(t.TempDir()),
		harness.UseLogging(false),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- service.Run(ctx)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run() error = %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run 未在超时内退出")
	}
}

// itoa 是 strconv.Itoa 的轻量替代，避免测试文件额外导入。
func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}

	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}

	return string(digits)
}
