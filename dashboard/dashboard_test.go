package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gitee.com/leminewx/leego/middlewares/collector"
)

// createTestReport 构造包含多种方法、状态码与启用状态的测试报告。
//
// 请求量：GET /api/users = 10，POST /api/users = 6，DELETE /api/users/:id = 2。
func createTestReport() collector.Report {
	return collector.NewReport([]collector.Snapshot{
		{
			Name:           "用户列表",
			Method:         http.MethodGet,
			Pattern:        "/api/users",
			Tags:           []string{"用户", "查询"},
			Enabled:        true,
			Flights:        1,
			Total:          10,
			Failed:         1,
			TotalCostMicro: 1000,
			Metrics: []collector.Metric{
				{Code: http.StatusOK, Total: 9, MaxCostMicro: 120, MinCostMicro: 40, TotalCostMicro: 900},
				{Code: http.StatusInternalServerError, Total: 1, MaxCostMicro: 100, MinCostMicro: 100, TotalCostMicro: 100},
			},
		},
		{
			Name:           "创建用户",
			Method:         http.MethodPost,
			Pattern:        "/api/users",
			Tags:           []string{"用户", "写入"},
			Enabled:        true,
			Total:          6,
			TotalCostMicro: 600,
			Metrics: []collector.Metric{
				{Code: http.StatusCreated, Total: 6, MaxCostMicro: 150, MinCostMicro: 80, TotalCostMicro: 600},
			},
		},
		{
			Name:           "删除用户",
			Method:         http.MethodDelete,
			Pattern:        "/api/users/:id",
			Tags:           []string{"用户", "写入"},
			Enabled:        false,
			Total:          2,
			TotalCostMicro: 200,
			Metrics: []collector.Metric{
				{Code: http.StatusNoContent, Total: 2, MaxCostMicro: 110, MinCostMicro: 90, TotalCostMicro: 200},
			},
		},
		{
			Name:    "未访问接口",
			Method:  http.MethodPut,
			Pattern: "/api/unused",
			Enabled: true,
		},
	})
}

// createTestDashboard 创建注入了测试数据的后台。
func createTestDashboard(t *testing.T, options ...Option) *Dashboard {
	t.Helper()

	options = append([]Option{WithProvider(createTestReport), WithoutAuthorizer()}, options...)
	instance, err := New(options...)
	if err != nil {
		t.Fatalf("创建后台失败: %v", err)
	}

	return instance
}

// performDashboardRequest 发起一次后台请求并返回记录器。
func performDashboardRequest(instance *Dashboard, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	instance.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

// TestNewDefaultOptions 验证默认配置。
func TestNewDefaultOptions(t *testing.T) {
	options := NewDefaultOptions()

	if options.Title != _DEFAULT_TITLE {
		t.Fatalf("Title = %q, want %q", options.Title, _DEFAULT_TITLE)
	}
	if options.RefreshInterval != _DEFAULT_REFRESH_INTERVAL {
		t.Fatalf("RefreshInterval = %v, want %v", options.RefreshInterval, _DEFAULT_REFRESH_INTERVAL)
	}
	if options.MaxRoutes != _DEFAULT_MAX_ROUTES {
		t.Fatalf("MaxRoutes = %d, want %d", options.MaxRoutes, _DEFAULT_MAX_ROUTES)
	}
	if !options.EnableExport {
		t.Fatal("默认应启用导出")
	}
	if options.Provider != nil || options.Authorizer != nil {
		t.Fatal("默认 Provider/Authorizer 应为 nil")
	}
}

// TestOptionsApplyExpectedValues 验证各配置项的行为与非法值忽略规则。
func TestOptionsApplyExpectedValues(t *testing.T) {
	options := NewDefaultOptions()

	WithTitle("  订单服务  ")(&options)
	if options.Title != "订单服务" {
		t.Fatalf("Title = %q, want 订单服务", options.Title)
	}
	WithTitle("   ")(&options)
	if options.Title != "订单服务" {
		t.Fatalf("空白标题不应覆盖，得到 %q", options.Title)
	}

	WithRefreshInterval(2 * time.Second)(&options)
	WithRefreshInterval(0)(&options)
	WithRefreshInterval(-time.Second)(&options)
	if options.RefreshInterval != 2*time.Second {
		t.Fatalf("RefreshInterval = %v, want 2s", options.RefreshInterval)
	}

	WithMaxRoutes(20)(&options)
	WithMaxRoutes(0)(&options)
	if options.MaxRoutes != 20 {
		t.Fatalf("MaxRoutes = %d, want 20", options.MaxRoutes)
	}

	provider := createTestReport
	WithProvider(provider)(&options)
	WithProvider(nil)(&options)
	if options.Provider == nil {
		t.Fatal("nil Provider 不应覆盖已有实现")
	}

	authorizer := func(*http.Request) bool { return true }
	WithAuthorizer(authorizer)(&options)
	if options.Authorizer == nil {
		t.Fatal("Authorizer 未被设置")
	}
	WithAuthorizer(nil)(&options)
	if options.Authorizer != nil {
		t.Fatal("Authorizer 应可被重置为 nil")
	}

	EnableExport(false)(&options)
	if options.EnableExport {
		t.Fatal("EnableExport(false) 未生效")
	}
}

// TestNewValidatesMaxRoutes 验证非正数 MaxRoutes 会被拒绝。
func TestNewValidatesMaxRoutes(t *testing.T) {
	_, err := New(func(options *Options) {
		options.MaxRoutes = 0
	})
	if err == nil {
		t.Fatal("MaxRoutes=0 必须返回错误")
	}
}

// TestNewRendersPage 验证页面模板占位符已被替换，并完成 HTML 转义。
func TestNewRendersPage(t *testing.T) {
	instance := createTestDashboard(t,
		WithTitle(`后台 <script>alert("x")</script>`),
		WithRefreshInterval(2*time.Second),
		EnableExport(false),
	)

	page := string(instance.page)
	for _, placeholder := range []string{"{{TITLE}}", "{{REFRESH_MS}}", "{{EXPORT_ENABLED}}"} {
		if strings.Contains(page, placeholder) {
			t.Fatalf("页面仍包含未替换的占位符 %q", placeholder)
		}
	}

	if !strings.Contains(page, "&lt;script&gt;") {
		t.Fatalf("标题未被 HTML 转义: %q", page[:min(len(page), 400)])
	}
	if strings.Contains(page, `<script>alert("x")</script>`) {
		t.Fatal("页面不应包含未转义的脚本")
	}
	if !strings.Contains(page, "2000") {
		t.Fatal("刷新间隔（毫秒）未被写入页面")
	}
	if !strings.Contains(page, "false") {
		t.Fatal("导出开关未被写入页面")
	}
}

// TestGetOptionsAndMetadata 验证配置副本、创建时间与字符串描述。
func TestGetOptionsAndMetadata(t *testing.T) {
	before := time.Now()
	instance := createTestDashboard(t, WithTitle("指标"), WithMaxRoutes(10))
	after := time.Now()

	options := instance.GetOptions()
	if options.Title != "指标" || options.MaxRoutes != 10 {
		t.Fatalf("GetOptions() = %+v", options)
	}

	// 修改副本不应影响内部配置。
	options.Title = "篡改"
	if instance.GetOptions().Title != "指标" {
		t.Fatal("GetOptions 必须返回副本")
	}

	startedAt := instance.GetStartedAt()
	if startedAt.Before(before) || startedAt.After(after) {
		t.Fatalf("GetStartedAt() = %v, want [%v, %v]", startedAt, before, after)
	}

	if description := instance.String(); !strings.Contains(description, "指标") || !strings.Contains(description, "10") {
		t.Fatalf("String() = %q", description)
	}
}

// TestServeHTTPSetsSecurityHeaders 验证所有响应都携带安全头。
func TestServeHTTPSetsSecurityHeaders(t *testing.T) {
	instance := createTestDashboard(t)
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics")

	expected := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	}
	for name, value := range expected {
		if actual := recorder.Header().Get(name); actual != value {
			t.Fatalf("%s = %q, want %q", name, actual, value)
		}
	}
}

// TestServeHTTPRejectsUnsupportedMethods 验证非 GET/HEAD 请求返回 405 与 Allow。
func TestServeHTTPRejectsUnsupportedMethods(t *testing.T) {
	instance := createTestDashboard(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		recorder := performDashboardRequest(instance, method, "/metrics")
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s 状态码 = %d, want 405", method, recorder.Code)
		}
		if allow := recorder.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Fatalf("%s Allow = %q, want GET, HEAD", method, allow)
		}
	}

	// HEAD 允许通过。
	if recorder := performDashboardRequest(instance, http.MethodHead, "/metrics"); recorder.Code != http.StatusOK {
		t.Fatalf("HEAD 状态码 = %d, want 200", recorder.Code)
	}
}

// TestServeHTTPAuthorizer 验证鉴权失败返回 401，成功继续处理。
func TestServeHTTPAuthorizer(t *testing.T) {
	instance := createTestDashboard(t, WithAuthorizer(func(req *http.Request) bool {
		return req.Header.Get("X-Admin-Token") == "secret"
	}))

	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未授权状态码 = %d, want 401", recorder.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("X-Admin-Token", "secret")
	authorized := httptest.NewRecorder()
	instance.ServeHTTP(authorized, request)

	if authorized.Code != http.StatusOK {
		t.Fatalf("授权后状态码 = %d, want 200", authorized.Code)
	}
}

// TestResolveRoute 验证后缀匹配规则（挂载前缀可任意、兼容结尾斜杠与反斜杠）。
func TestResolveRoute(t *testing.T) {
	instance := createTestDashboard(t)

	cases := []struct {
		path     string
		expected pageRoute
	}{
		{path: "", expected: _ROUTE_PAGE},
		{path: "/", expected: _ROUTE_PAGE},
		{path: "/metrics", expected: _ROUTE_PAGE},
		{path: "/metrics/", expected: _ROUTE_PAGE},
		{path: "/metrics/index.html", expected: _ROUTE_PAGE},
		{path: "/metrics/api/report", expected: _ROUTE_REPORT},
		{path: "/metrics/api", expected: _ROUTE_REPORT},
		{path: "/metrics/report", expected: _ROUTE_REPORT},
		{path: "/metrics/api/routes", expected: _ROUTE_ROUTES},
		{path: "/metrics/api/health", expected: _ROUTE_HEALTH},
		{path: "/metrics/export.csv", expected: _ROUTE_EXPORT},
		{path: `/metrics\api\report`, expected: _ROUTE_REPORT},
		{path: "/metrics/unknown", expected: _ROUTE_UNKNOWN},
		{path: "/metrics/api/report/extra", expected: _ROUTE_UNKNOWN},
	}

	for _, testCase := range cases {
		if actual := instance.resolveRoute(testCase.path); actual != testCase.expected {
			t.Fatalf("resolveRoute(%q) = %d, want %d", testCase.path, actual, testCase.expected)
		}
	}
}

// TestServeHTTPDispatchesRoutes 验证各路由的状态码与内容类型。
func TestServeHTTPDispatchesRoutes(t *testing.T) {
	instance := createTestDashboard(t)

	cases := []struct {
		path            string
		expectedStatus  int
		expectedContent string
	}{
		{path: "/metrics", expectedStatus: http.StatusOK, expectedContent: "text/html"},
		{path: "/metrics/api/report", expectedStatus: http.StatusOK, expectedContent: "application/json"},
		{path: "/metrics/api/routes", expectedStatus: http.StatusOK, expectedContent: "application/json"},
		{path: "/metrics/api/health", expectedStatus: http.StatusOK, expectedContent: "application/json"},
		{path: "/metrics/export.csv", expectedStatus: http.StatusOK, expectedContent: "text/csv"},
		{path: "/metrics/not-found", expectedStatus: http.StatusNotFound, expectedContent: "text/plain"},
	}

	for _, testCase := range cases {
		recorder := performDashboardRequest(instance, http.MethodGet, testCase.path)
		if recorder.Code != testCase.expectedStatus {
			t.Fatalf("%s 状态码 = %d, want %d", testCase.path, recorder.Code, testCase.expectedStatus)
		}
		if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, testCase.expectedContent) {
			t.Fatalf("%s Content-Type = %q, want 包含 %q", testCase.path, contentType, testCase.expectedContent)
		}
	}
}

// TestWriteReportContainsAllRoutes 验证报告接口返回全部路由与汇总。
func TestWriteReportContainsAllRoutes(t *testing.T) {
	instance := createTestDashboard(t)
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/api/report")

	var report collector.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("报告不是合法 JSON: %v", err)
	}

	if len(report.Routes) != 4 {
		t.Fatalf("路由数 = %d, want 4", len(report.Routes))
	}
	if report.Totals.Routes != 4 || report.Totals.Requests != 18 || report.Totals.Failed != 1 {
		t.Fatalf("汇总 = %+v", report.Totals)
	}
	if report.GeneratedAt.IsZero() {
		t.Fatal("GeneratedAt 不应为零值")
	}
}

// TestWriteReportFiltersByQuery 验证 q / method / status / enabled 过滤。
func TestWriteReportFiltersByQuery(t *testing.T) {
	instance := createTestDashboard(t)

	cases := []struct {
		name           string
		query          string
		expectedRoutes int
		expectedTotal  uint64
	}{
		{name: "按方法过滤", query: "?method=post", expectedRoutes: 1, expectedTotal: 6},
		{name: "按关键字过滤模式", query: "?q=%2Fapi%2Funused", expectedRoutes: 1, expectedTotal: 0},
		{name: "按关键字过滤名称", query: "?q=%E5%88%9B%E5%BB%BA", expectedRoutes: 1, expectedTotal: 6},
		{name: "按标签过滤", query: "?q=%E5%86%99%E5%85%A5", expectedRoutes: 2, expectedTotal: 8},
		{name: "按启用状态过滤", query: "?enabled=false", expectedRoutes: 1, expectedTotal: 2},
		{name: "按状态码过滤", query: "?status=500", expectedRoutes: 1, expectedTotal: 10},
		{name: "非法状态码被忽略", query: "?status=abc", expectedRoutes: 4, expectedTotal: 18},
		{name: "组合过滤", query: "?method=GET&enabled=true", expectedRoutes: 1, expectedTotal: 10},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/api/report"+testCase.query)

			var report collector.Report
			if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
				t.Fatalf("报告不是合法 JSON: %v", err)
			}

			if len(report.Routes) != testCase.expectedRoutes {
				t.Fatalf("路由数 = %d, want %d", len(report.Routes), testCase.expectedRoutes)
			}
			if testCase.expectedRoutes > 0 && report.Totals.Requests != testCase.expectedTotal {
				t.Fatalf("请求总数 = %d, want %d", report.Totals.Requests, testCase.expectedTotal)
			}
		})
	}
}

// TestWriteReportLimitsRoutes 验证按请求量截断并重新汇总。
func TestWriteReportLimitsRoutes(t *testing.T) {
	instance := createTestDashboard(t, WithMaxRoutes(2))
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/api/report")

	var report collector.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("报告不是合法 JSON: %v", err)
	}

	if len(report.Routes) != 2 {
		t.Fatalf("路由数 = %d, want 2", len(report.Routes))
	}
	// 优先保留请求量最高的两条：GET(10) 与 POST(6)。
	if report.Routes[0].Total != 10 || report.Routes[1].Total != 6 {
		t.Fatalf("截断结果 = %d/%d, want 10/6", report.Routes[0].Total, report.Routes[1].Total)
	}
	if report.Totals.Routes != 2 || report.Totals.Requests != 16 {
		t.Fatalf("截断后汇总 = %+v, want routes=2 requests=16", report.Totals)
	}
}

// TestWriteRoutesStructure 验证路由清单接口的输出结构。
func TestWriteRoutesStructure(t *testing.T) {
	instance := createTestDashboard(t)
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/api/routes")

	var payload struct {
		GeneratedAt time.Time            `json:"generated_at"`
		Routes      []collector.Snapshot `json:"routes"`
		Totals      collector.Totals     `json:"totals"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("路由清单不是合法 JSON: %v", err)
	}

	if len(payload.Routes) != 4 {
		t.Fatalf("路由数 = %d, want 4", len(payload.Routes))
	}
	if payload.Totals.Requests != 18 {
		t.Fatalf("Totals.Requests = %d, want 18", payload.Totals.Requests)
	}
	if payload.GeneratedAt.IsZero() {
		t.Fatal("generated_at 不应为零值")
	}
}

// TestWriteHealth 验证健康检查返回状态与运行时长。
func TestWriteHealth(t *testing.T) {
	instance := createTestDashboard(t)
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/api/health")

	var payload struct {
		Status    string `json:"status"`
		StartedAt string `json:"started_at"`
		Uptime    string `json:"uptime"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("健康检查不是合法 JSON: %v", err)
	}

	if payload.Status != "ok" {
		t.Fatalf("status = %q, want ok", payload.Status)
	}
	if payload.StartedAt == "" || payload.Uptime == "" {
		t.Fatalf("健康检查字段不完整: %+v", payload)
	}
}

// TestWriteCSV 验证 CSV 的响应头、列头与数据行。
func TestWriteCSV(t *testing.T) {
	instance := createTestDashboard(t)
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/export.csv")

	if disposition := recorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "leego-metrics.csv") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}

	rows, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("CSV 解析失败: %v", err)
	}

	expectedHeader := []string{"method", "pattern", "name", "status", "total", "avg_cost_micro", "max_cost_micro", "min_cost_micro"}
	if len(rows) == 0 || strings.Join(rows[0], ",") != strings.Join(expectedHeader, ",") {
		t.Fatalf("列头 = %v, want %v", rows[0], expectedHeader)
	}

	// 数据行数 = 4 条指标行 + 1 条无指标路由行。
	if len(rows)-1 != 5 {
		t.Fatalf("数据行数 = %d, want 5", len(rows)-1)
	}

	// 无指标路由仍会输出一行，状态码与耗时为空白。
	foundUnused := false
	foundOKMetric := false
	for _, row := range rows[1:] {
		if row[1] == "/api/unused" {
			foundUnused = true
			if row[3] != "" || row[6] != "" || row[7] != "" {
				t.Fatalf("无指标路由行 = %v，状态码与耗时列应为空", row)
			}
			if row[4] != "0" {
				t.Fatalf("无指标路由 total = %q, want 0", row[4])
			}
		}
		if row[1] == "/api/users" && row[0] == http.MethodGet && row[3] == "200" {
			foundOKMetric = true
			if row[4] != "9" || row[5] != "100" || row[6] != "120" || row[7] != "40" {
				t.Fatalf("200 指标行 = %v", row)
			}
		}
	}

	if !foundUnused {
		t.Fatal("CSV 缺少无指标路由行")
	}
	if !foundOKMetric {
		t.Fatal("CSV 缺少 200 指标行")
	}
}

// TestWriteCSVDisabled 验证关闭导出后返回 404。
func TestWriteCSVDisabled(t *testing.T) {
	instance := createTestDashboard(t, EnableExport(false))
	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/export.csv")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", recorder.Code)
	}
}

// TestWriteReportWithoutProvider 验证未配置 Provider 时返回空报告。
func TestWriteReportWithoutProvider(t *testing.T) {
	// 鉴权默认拒绝，这里显式放行，只验证空报告的内容。
	instance, err := New(WithoutAuthorizer())
	if err != nil {
		t.Fatalf("创建后台失败: %v", err)
	}

	recorder := performDashboardRequest(instance, http.MethodGet, "/metrics/api/report")

	var report collector.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("报告不是合法 JSON: %v", err)
	}
	if len(report.Routes) != 0 || report.Totals.Requests != 0 {
		t.Fatalf("空报告内容 = %+v", report)
	}

	// 空报告必须输出 [] 而不是 null。
	if !strings.Contains(recorder.Body.String(), `"routes": []`) {
		t.Fatalf("空报告的 routes 应为 []: %s", recorder.Body.String())
	}
}

// TestParseStatus 验证状态码参数的解析与裁剪规则。
func TestParseStatus(t *testing.T) {
	cases := []struct {
		value    string
		expected int
		ok       bool
	}{
		{value: "", ok: false},
		{value: "  ", ok: false},
		{value: "200", expected: 200, ok: true},
		{value: " 500 ", expected: 500, ok: true},
		{value: "99", ok: false},
		{value: "600", ok: false},
		{value: "abc", ok: false},
	}

	for _, testCase := range cases {
		status, ok := parseStatus(testCase.value)
		if ok != testCase.ok || status != testCase.expected {
			t.Fatalf("parseStatus(%q) = (%d, %v), want (%d, %v)", testCase.value, status, ok, testCase.expected, testCase.ok)
		}
	}
}

// TestParseBool 验证布尔参数的解析规则。
func TestParseBool(t *testing.T) {
	cases := []struct {
		value    string
		expected bool
		ok       bool
	}{
		{value: "", ok: false},
		{value: "true", expected: true, ok: true},
		{value: "TRUE", expected: true, ok: true},
		{value: "1", expected: true, ok: true},
		{value: "false", expected: false, ok: true},
		{value: "0", expected: false, ok: true},
		{value: "yes", ok: false},
	}

	for _, testCase := range cases {
		parsed, ok := parseBool(testCase.value)
		if ok != testCase.ok || parsed != testCase.expected {
			t.Fatalf("parseBool(%q) = (%v, %v), want (%v, %v)", testCase.value, parsed, ok, testCase.expected, testCase.ok)
		}
	}
}

// TestEscapeHTML 验证标题转义覆盖全部危险字符。
func TestEscapeHTML(t *testing.T) {
	escaped := escapeHTML(`<a href="x">&'</a>`)
	expected := `&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;`
	if escaped != expected {
		t.Fatalf("escapeHTML() = %q, want %q", escaped, expected)
	}
}

// TestLimitReportKeepsOrderForEqualTotals 验证请求量相同时保持稳定顺序。
func TestLimitReportKeepsOrderForEqualTotals(t *testing.T) {
	report := collector.NewReport([]collector.Snapshot{
		{Method: http.MethodGet, Pattern: "/a", Total: 5},
		{Method: http.MethodPost, Pattern: "/b", Total: 5},
	})

	limited := limitReport(report, 1)
	if len(limited.Routes) != 1 {
		t.Fatalf("路由数 = %d, want 1", len(limited.Routes))
	}
	// NewReport 已按方法名升序排列，稳定排序应保留 GET 在前。
	if limited.Routes[0].Method != http.MethodGet {
		t.Fatalf("保留路由 = %q, want GET", limited.Routes[0].Method)
	}
}

// TestLimitReportIgnoresSmallReport 验证报告未超限时原样返回。
func TestLimitReportIgnoresSmallReport(t *testing.T) {
	report := createTestReport()
	limited := limitReport(report, 100)

	if len(limited.Routes) != len(report.Routes) {
		t.Fatalf("路由数 = %d, want %d", len(limited.Routes), len(report.Routes))
	}
	if limited.Totals.Requests != report.Totals.Requests {
		t.Fatalf("汇总被修改: %d != %d", limited.Totals.Requests, report.Totals.Requests)
	}
}

// TestServeHTTPConcurrently 验证后台可被并发访问（页面与接口）。
func TestServeHTTPConcurrently(t *testing.T) {
	instance := createTestDashboard(t)
	paths := []string{"/metrics", "/metrics/api/report", "/metrics/api/routes", "/metrics/api/health", "/metrics/export.csv"}

	var waitGroup sync.WaitGroup
	for index := 0; index < 20; index++ {
		waitGroup.Add(1)
		go func(slot int) {
			defer waitGroup.Done()

			recorder := performDashboardRequest(instance, http.MethodGet, paths[slot%len(paths)])
			if recorder.Code != http.StatusOK {
				t.Errorf("%s 状态码 = %d, want 200", paths[slot%len(paths)], recorder.Code)
			}
		}(index)
	}
	waitGroup.Wait()
}

// BenchmarkServePage 测量页面响应耗时。
func BenchmarkServePage(b *testing.B) {
	instance, err := New()
	if err != nil {
		b.Fatalf("创建后台失败: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		instance.ServeHTTP(httptest.NewRecorder(), request)
	}
}

// BenchmarkServeReport 测量报告接口（含过滤与 JSON 序列化）的耗时。
func BenchmarkServeReport(b *testing.B) {
	instance, err := New(WithProvider(createTestReport))
	if err != nil {
		b.Fatalf("创建后台失败: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics/api/report", nil)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		instance.ServeHTTP(httptest.NewRecorder(), request)
	}
}
