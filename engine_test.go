package leego

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/leminewx/leego/dashboard"
	"gitee.com/leminewx/leego/middlewares/collector"
)

// createTestEngine 构造一个关闭启动日志的测试引擎。
func createTestEngine() *Engine {
	return NewEngine(DisableStartupLog())
}

// performRequest 在引擎上直接执行一次请求并返回响应。
func performRequest(engine *Engine, method, target string, body io.Reader) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(method, target, body))
	return recorder
}

// TestEngineRouting 验证各 HTTP 方法的路由注册与匹配。
func TestEngineRouting(t *testing.T) {
	engine := createTestEngine()

	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodOptions,
	}

	for _, method := range methods {
		method := method
		handle := func(ctx *Context) {
			_ = ctx.ResponseText(http.StatusOK, method)
		}

		switch method {
		case http.MethodGet:
			engine.GET("/method", handle)
		case http.MethodPost:
			engine.POST("/method", handle)
		case http.MethodPut:
			engine.PUT("/method", handle)
		case http.MethodDelete:
			engine.DELETE("/method", handle)
		case http.MethodPatch:
			engine.PATCH("/method", handle)
		case http.MethodOptions:
			engine.OPTIONS("/method", handle)
		}
	}

	for _, method := range methods {
		recorder := performRequest(engine, method, "/method", nil)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s /method 状态码 = %d, want 200", method, recorder.Code)
		}

		if recorder.Body.String() != method {
			t.Errorf("%s /method 响应体 = %q, want %q", method, recorder.Body.String(), method)
		}
	}
}

// TestEngineNotFound 验证未注册路由返回 404。
func TestEngineNotFound(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/exists", func(ctx *Context) {})

	recorder := performRequest(engine, http.MethodGet, "/missing", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", recorder.Code)
	}
}

// TestEngineMethodNotAllowed 验证 405 与 Allow 头。
func TestEngineMethodNotAllowed(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/only-get", func(ctx *Context) {})

	recorder := performRequest(engine, http.MethodPost, "/only-get", nil)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, want 405", recorder.Code)
	}

	allow := recorder.Header().Get("Allow")
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if !strings.Contains(allow, method) {
			t.Fatalf("Allow = %q, 缺少 %s", allow, method)
		}
	}
}

// TestEngineAutoOptions 验证未注册 OPTIONS 时的自动预检响应。
func TestEngineAutoOptions(t *testing.T) {
	engine := createTestEngine()
	engine.POST("/resource", func(ctx *Context) {})

	recorder := performRequest(engine, http.MethodOptions, "/resource", nil)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d, want 204", recorder.Code)
	}

	if allow := recorder.Header().Get("Allow"); !strings.Contains(allow, http.MethodPost) {
		t.Fatalf("Allow = %q, 缺少 POST", allow)
	}
}

// TestEngineHeadFallsBackToGet 验证 HEAD 未注册时回退到 GET。
func TestEngineHeadFallsBackToGet(t *testing.T) {
	engine := createTestEngine()

	calls := 0
	engine.GET("/head", func(ctx *Context) {
		calls++
		_ = ctx.ResponseText(http.StatusOK, "body")
	})

	recorder := performRequest(engine, http.MethodHead, "/head", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HEAD 状态码 = %d, want 200", recorder.Code)
	}

	if calls != 1 {
		t.Fatalf("GET 处理函数调用次数 = %d, want 1", calls)
	}
}

// TestEngineURLBlacklist 验证黑名单拦截。
func TestEngineURLBlacklist(t *testing.T) {
	engine := createTestEngine()
	engine.AddURLBlacklist(" /secret ", "", "/admin")

	if !engine.IsBlacklisted("/secret") || !engine.IsBlacklisted("/admin") {
		t.Fatal("黑名单必须去除首尾空白并记录有效路径")
	}

	if engine.IsBlacklisted("/public") {
		t.Fatal("未加入黑名单的路径不应被拦截")
	}

	engine.GET("/secret", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "should-not-reach")
	})

	recorder := performRequest(engine, http.MethodGet, "/secret", nil)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("黑名单路径状态码 = %d, want 403", recorder.Code)
	}
}

// TestEnginePathAndWildcardParams 验证路径参数与通配符捕获。
func TestEnginePathAndWildcardParams(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/users/:id", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("id"))
	})
	engine.GET("/files/*", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("filepath"))
	})

	if body := performRequest(engine, http.MethodGet, "/users/42", nil).Body.String(); body != "42" {
		t.Fatalf("路径参数 = %q, want 42", body)
	}

	if body := performRequest(engine, http.MethodGet, "/files/a/b/c.txt", nil).Body.String(); body != "a/b/c.txt" {
		t.Fatalf("通配符参数 = %q, want a/b/c.txt", body)
	}
}

// TestEngineGroupAndMiddleware 验证路由组前缀与中间件执行顺序。
func TestEngineGroupAndMiddleware(t *testing.T) {
	engine := createTestEngine()

	order := make([]string, 0, 4)
	record := func(name string) MiddlewareFunc {
		return func(next HandleFunc) HandleFunc {
			return func(ctx *Context) {
				order = append(order, name)
				next(ctx)
			}
		}
	}

	engine.UseMiddlewares(record("engine"))

	v1 := engine.NewGroup("/api/v1")
	v1.UseMiddlewares(record("group"))
	v1.GET("/ping", func(ctx *Context) {
		order = append(order, "handler")
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	recorder := performRequest(engine, http.MethodGet, "/api/v1/ping", nil)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "pong" {
		t.Fatalf("响应 = (%d, %q)", recorder.Code, recorder.Body.String())
	}

	want := []string{"engine", "group", "handler"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("中间件执行顺序 = %v, want %v", order, want)
	}
}

// TestEngineGlobalMiddlewareOnUnmatched 验证全局中间件对 404 同样生效。
func TestEngineGlobalMiddlewareOnUnmatched(t *testing.T) {
	engine := createTestEngine()
	engine.UseMiddlewares(func(next HandleFunc) HandleFunc {
		return func(ctx *Context) {
			ctx.SetResponseHeader("X-Trace", "global")
			next(ctx)
		}
	})

	recorder := performRequest(engine, http.MethodGet, "/missing", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", recorder.Code)
	}

	if got := recorder.Header().Get("X-Trace"); got != "global" {
		t.Fatalf("X-Trace = %q, want global", got)
	}
}

// TestEngineDisabledHandler 验证禁用路由返回 423。
func TestEngineDisabledHandler(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/disabled", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "should-not-reach")
	})

	handler, ok := engine.GetHandler(http.MethodGet, "/disabled")
	if !ok {
		t.Fatal("未检索到处理器")
	}

	handler.Disable()

	recorder := performRequest(engine, http.MethodGet, "/disabled", nil)
	if recorder.Code != http.StatusLocked {
		t.Fatalf("状态码 = %d, want 423", recorder.Code)
	}

	if strings.Contains(recorder.Body.String(), "should-not-reach") {
		t.Fatal("禁用路由不得执行业务处理函数")
	}
}

// TestEngineJSONAndRedirect 验证 JSON 响应与重定向响应。
func TestEngineJSONAndRedirect(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/json", func(ctx *Context) {
		_ = ctx.ResponseJSON(http.StatusCreated, JSON{"name": "leego"})
	})
	engine.GET("/old", func(ctx *Context) {
		ctx.ResponseRedirect(http.StatusMovedPermanently, "/new")
	})

	jsonRecorder := performRequest(engine, http.MethodGet, "/json", nil)
	if jsonRecorder.Code != http.StatusCreated {
		t.Fatalf("JSON 状态码 = %d, want 201", jsonRecorder.Code)
	}

	if !strings.Contains(jsonRecorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("JSON Content-Type = %q", jsonRecorder.Header().Get("Content-Type"))
	}

	redirectRecorder := performRequest(engine, http.MethodGet, "/old", nil)
	if redirectRecorder.Code != http.StatusMovedPermanently {
		t.Fatalf("重定向状态码 = %d, want 301", redirectRecorder.Code)
	}

	if got := redirectRecorder.Header().Get("Location"); got != "/new" {
		t.Fatalf("Location = %q, want /new", got)
	}
}

// TestEngineGetRoutes 验证路由清单的过滤与排序。
func TestEngineGetRoutes(t *testing.T) {
	engine := createTestEngine()
	engine.POST("/zeta", func(ctx *Context) {}, "写入")
	engine.GET("/alpha", func(ctx *Context) {}, "读取", "query")

	routes := engine.GetRoutes()
	if len(routes) != 2 {
		t.Fatalf("路由数量 = %d, want 2", len(routes))
	}

	if routes[0].Method != http.MethodGet || routes[0].Pattern != "/alpha" {
		t.Fatalf("首条路由 = %+v, want GET /alpha", routes[0])
	}

	if routes[0].Name != "读取" || len(routes[0].Tags) != 1 || routes[0].Tags[0] != "query" {
		t.Fatalf("路由元信息 = %+v", routes[0])
	}

	if !routes[0].Enabled {
		t.Fatal("路由默认必须启用")
	}
}

// TestEngineCollectReport 验证指标汇总。
func TestEngineCollectReport(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/metrics", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "ok")
	})

	for index := 0; index < 3; index++ {
		performRequest(engine, http.MethodGet, "/metrics", nil)
	}

	report := engine.CollectReport()
	if !report.Routes[0].Enabled || report.Routes[0].Total != 3 {
		t.Fatalf("路由指标 = %+v, want total=3", report.Routes[0])
	}

	if report.Totals.Requests != 3 || report.Totals.Routes != 1 {
		t.Fatalf("汇总指标 = %+v, want requests=3, routes=1", report.Totals)
	}

	snapshots := engine.GetAllHandlerSnapshots()
	if len(snapshots) != 1 || snapshots[0].Pattern != "/metrics" {
		t.Fatalf("快照 = %+v", snapshots)
	}
}

// TestEngineGetHandler 验证按方法与模式检索处理器。
func TestEngineGetHandler(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/lookup", func(ctx *Context) {})

	if _, ok := engine.GetHandler(http.MethodGet, "/lookup"); !ok {
		t.Fatal("GET /lookup 必须能检索到")
	}

	if _, ok := engine.GetHandler(http.MethodPost, "/lookup"); ok {
		t.Fatal("未注册的 POST /lookup 不应被检索到")
	}
}

// TestEngineConfigOptions 验证配置选项与深拷贝隔离。
func TestEngineConfigOptions(t *testing.T) {
	engine := NewEngine(
		WithEngineName("  svc  "),
		WithMaxBodyBytes(128),
		WithStrictJSON(true),
		WithServerTimeouts(time.Second, 2*time.Second, 3*time.Second, 4*time.Second),
		WithTrustedProxies("10.0.0.0/8"),
		DisableStartupLog(),
	)

	config := engine.GetConfig()
	if config.Name != "svc" {
		t.Fatalf("Name = %q, want svc", config.Name)
	}

	if config.MaxBodyBytes != 128 || !config.StrictJSON || !config.DisableStartupLog {
		t.Fatalf("布尔与数值配置 = %+v", config)
	}

	if config.ReadHeaderTimeout != time.Second || config.ReadTimeout != 2*time.Second ||
		config.WriteTimeout != 3*time.Second || config.IdleTimeout != 4*time.Second {
		t.Fatalf("超时配置 = %+v", config)
	}

	// GetConfig 返回深拷贝，修改不应影响引擎。
	config.TrustedProxies[0] = "192.0.2.0/24"
	if engine.GetConfig().TrustedProxies[0] != "10.0.0.0/8" {
		t.Fatal("GetConfig 必须返回深拷贝")
	}

	if len(engine.GetTrustedProxies()) != 1 {
		t.Fatalf("可信代理数量 = %d, want 1", len(engine.GetTrustedProxies()))
	}

	// 无效 CIDR 会被忽略，但合法项仍然生效。
	mixed := NewEngine(DisableStartupLog())
	if err := mixed.SetTrustedProxies("10.0.0.0/8", "not-a-cidr"); err == nil {
		t.Fatal("包含非法 CIDR 时必须返回错误")
	}

	if len(mixed.GetTrustedProxies()) != 2 {
		t.Fatalf("原始代理列表长度 = %d, want 2", len(mixed.GetTrustedProxies()))
	}

	engine.SetName("renamed")
	if engine.GetName() != "renamed" {
		t.Fatalf("GetName() = %q, want renamed", engine.GetName())
	}

	engine.SetMaxMultipartMemory(1024)
	if engine.GetConfig().MaxMultipartMemory != 1024 {
		t.Fatal("SetMaxMultipartMemory 未生效")
	}
}

// TestEngineStrictJSON 验证严格 JSON 模式拒绝未知字段。
func TestEngineStrictJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	var bindErr error
	engine := NewEngine(WithStrictJSON(true), DisableStartupLog())
	engine.POST("/bind", func(ctx *Context) {
		var body payload
		bindErr = ctx.BindJSON(&body)
		_ = ctx.ResponseText(http.StatusOK, "ok")
	})

	body := strings.NewReader(`{"name":"leego","unknown":1}`)
	request := httptest.NewRequest(http.MethodPost, "/bind", body)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if bindErr == nil {
		t.Fatal("严格模式下未知字段必须返回错误")
	}
}

// TestEngineDashboard 验证指标后台路由注册与关键端点。
func TestEngineDashboard(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/api/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	if err := engine.RegisterDashboard("/dashboard", dashboard.WithoutAuthorizer()); err != nil {
		t.Fatalf("RegisterDashboard() error = %v", err)
	}

	page := performRequest(engine, http.MethodGet, "/dashboard", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("后台页面状态码 = %d, want 200", page.Code)
	}

	if !strings.Contains(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("后台页面 Content-Type = %q", page.Header().Get("Content-Type"))
	}

	report := performRequest(engine, http.MethodGet, "/dashboard/api/report", nil)
	if report.Code != http.StatusOK {
		t.Fatalf("报告接口状态码 = %d, want 200", report.Code)
	}

	var decoded struct {
		Routes []map[string]any `json:"routes"`
	}
	if err := json.Unmarshal(report.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("报告接口返回非法 JSON: %v", err)
	}

	if len(decoded.Routes) == 0 {
		t.Fatal("报告接口必须包含已注册路由")
	}

	health := performRequest(engine, http.MethodGet, "/dashboard/api/health", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("健康检查状态码 = %d, want 200", health.Code)
	}
}

// TestEngineShutdownWithoutServer 验证未启动时关闭不会出错。
func TestEngineShutdownWithoutServer(t *testing.T) {
	engine := createTestEngine()

	if err := engine.Shutdown(nil); err != nil {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}

	if engine.GetServer() != nil {
		t.Fatal("未启动时 GetServer 必须返回 nil")
	}
}

// TestEngineRunListenerAndShutdown 验证真实监听、请求与优雅关闭。
func TestEngineRunListenerAndShutdown(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听测试端口失败: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- engine.RunListener(listener) }()

	client := &http.Client{Timeout: time.Second}
	address := "http://" + listener.Addr().String() + "/ping"

	var response *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, err = client.Get(address)
		if err == nil {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if err != nil {
		t.Fatalf("请求测试服务失败: %v", err)
	}

	payload, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	if response.StatusCode != http.StatusOK || string(payload) != "pong" {
		t.Fatalf("响应 = (%d, %q)", response.StatusCode, payload)
	}

	if engine.GetServer() == nil {
		t.Fatal("服务启动后 GetServer 不能为 nil")
	}

	if err = engine.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("RunListener() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("优雅关闭后 RunListener 未返回")
	}
}

// TestEngineRunTLSWithoutCertificate 验证 TLS 参数缺失时的错误。
func TestEngineRunTLSWithoutCertificate(t *testing.T) {
	engine := createTestEngine()

	if err := engine.RunTLS(":0", "", ""); err == nil {
		t.Fatal("缺少证书文件时必须返回错误")
	}

	if err := engine.RunListener(nil); err == nil {
		t.Fatal("nil 监听器必须返回错误")
	}
}

// TestUseWriteTimeoutAllowsZero 验证长连接可以显式关闭写入超时。
//
// WithServerTimeouts 会忽略非正数，SSE/WebSocket 无法借它关闭 WriteTimeout，
// 因此必须提供允许 0 的独立选项。
func TestUseWriteTimeoutAllowsZero(t *testing.T) {
	engine := NewEngine(DisableStartupLog(), WithWriteTimeout(0), WithServerTimeouts(0, 0, 5*time.Second, 0))

	// WithServerTimeouts 中的正数仍然生效，但 0 不会把显式关闭覆盖回默认值。
	if got := engine.GetConfig().WriteTimeout; got != 5*time.Second {
		t.Fatalf("WriteTimeout = %v, want 5s", got)
	}

	longLived := NewEngine(DisableStartupLog(), WithWriteTimeout(0))
	if got := longLived.GetConfig().WriteTimeout; got != 0 {
		t.Fatalf("WriteTimeout = %v, want 0（SSE/WebSocket 需要无限期写入）", got)
	}

	negative := NewEngine(DisableStartupLog(), WithWriteTimeout(-time.Second))
	if got := negative.GetConfig().WriteTimeout; got != 0 {
		t.Fatalf("负数应视为 0，实际 = %v", got)
	}
}

// TestEngineMultipartMemoryOptions 验证 multipart 内存上限的配置与回退。
func TestEngineMultipartMemoryOptions(t *testing.T) {
	engine := NewEngine(WithMultipartMemory(4<<20), DisableStartupLog())
	if got := engine.getMultipartMemory(); got != 4<<20 {
		t.Fatalf("getMultipartMemory() = %d, want %d", got, int64(4<<20))
	}

	// 非正数应保留默认值。
	zero := NewEngine(WithMultipartMemory(0), DisableStartupLog())
	if got := zero.getMultipartMemory(); got != _DEFAULT_MULTIPART_MEMORY {
		t.Fatalf("零值配置 = %d, want %d", got, _DEFAULT_MULTIPART_MEMORY)
	}

	// nil 引擎必须回退为默认值，避免空指针解引用。
	var absent *Engine
	if got := absent.getMultipartMemory(); got != _DEFAULT_MULTIPART_MEMORY {
		t.Fatalf("nil 引擎 = %d, want %d", got, _DEFAULT_MULTIPART_MEMORY)
	}

	engine.SetMaxMultipartMemory(8 << 20)
	if got := engine.getMultipartMemory(); got != 8<<20 {
		t.Fatalf("SetMaxMultipartMemory() = %d, want %d", got, int64(8<<20))
	}

	// 非正数不应覆盖已有配置。
	engine.SetMaxMultipartMemory(0)
	if got := engine.getMultipartMemory(); got != 8<<20 {
		t.Fatalf("非正数覆盖后 = %d, want %d", got, int64(8<<20))
	}
}

// TestEngineTemplateFuncsAndFiles 验证模板函数表与两种模板加载方式。
func TestEngineTemplateFuncsAndFiles(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.html")
	other := filepath.Join(dir, "other.html")

	if err := os.WriteFile(page, []byte("<p>{{ shout .Name }}</p>"), 0o644); err != nil {
		t.Fatalf("写入模板失败: %v", err)
	}

	if err := os.WriteFile(other, []byte("<span>{{ .Name }}</span>"), 0o644); err != nil {
		t.Fatalf("写入模板失败: %v", err)
	}

	engine := createTestEngine()
	engine.SetTemplateFuncs(template.FuncMap{"shout": strings.ToUpper})

	if got := engine.getTemplateFuncs(); len(got) != 1 {
		t.Fatalf("函数表长度 = %d, want 1", len(got))
	}

	if err := engine.LoadHTMLGlob(filepath.Join(dir, "*.html")); err != nil {
		t.Fatalf("LoadHTMLGlob() error = %v", err)
	}

	if engine.getTemplate() == nil {
		t.Fatal("LoadHTMLGlob 后模板不应为 nil")
	}

	recorder := httptest.NewRecorder()
	ctx := NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.engine = engine

	if err := ctx.ResponseTemplate("page.html", map[string]string{"Name": "leego"}); err != nil {
		t.Fatalf("ResponseTemplate() error = %v", err)
	}

	if !strings.Contains(recorder.Body.String(), "<p>LEEGO</p>") {
		t.Fatalf("渲染结果 = %q", recorder.Body.String())
	}

	// 按文件列表加载，并通过模板名执行。
	fileEngine := createTestEngine()
	fileEngine.SetTemplateFuncs(template.FuncMap{"shout": strings.ToUpper})

	if err := fileEngine.LoadHTMLFiles(page, other); err != nil {
		t.Fatalf("LoadHTMLFiles() error = %v", err)
	}

	fileRecorder := httptest.NewRecorder()
	fileContext := NewContext(fileRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	fileContext.engine = fileEngine

	if err := fileContext.ResponseTemplate("other.html", map[string]string{"Name": "go"}); err != nil {
		t.Fatalf("ResponseTemplate(other) error = %v", err)
	}

	if !strings.Contains(fileRecorder.Body.String(), "<span>go</span>") {
		t.Fatalf("other 渲染结果 = %q", fileRecorder.Body.String())
	}

	// 空函数表等价于清空；空文件列表返回错误。
	engine.SetTemplateFuncs(nil)
	if got := engine.getTemplateFuncs(); len(got) != 0 {
		t.Fatalf("清空后函数表长度 = %d, want 0", len(got))
	}

	if err := engine.LoadHTMLFiles(); err == nil {
		t.Fatal("空文件列表必须返回错误")
	}
}

// TestEngineHTTPProtocolsFollowsH2C 验证 h2c 通过协议集开启，而非包装处理器。
func TestEngineHTTPProtocolsFollowsH2C(t *testing.T) {
	plain := createTestEngine()
	if protocols := plain.GetHTTPProtocols(); protocols != nil {
		t.Fatalf("未启用 h2c 时应返回 nil 以沿用标准库默认，实际为 %v", protocols)
	}

	if _, isEngine := plain.GetHTTPHandler().(*Engine); !isEngine {
		t.Fatal("GetHTTPHandler() 必须直接返回引擎，不再包装处理器")
	}

	upgraded := NewEngine(WithH2C(), DisableStartupLog())
	protocols := upgraded.GetHTTPProtocols()
	if protocols == nil {
		t.Fatal("启用 h2c 后 GetHTTPProtocols() 不应返回 nil")
	}

	if !protocols.HTTP1() || !protocols.HTTP2() || !protocols.UnencryptedHTTP2() {
		t.Fatalf("协议集应同时包含 HTTP/1.1、HTTP/2 与明文 HTTP/2，实际为 %v", protocols)
	}
}

// TestEngineH2CServesUnencryptedHTTP2 验证启用 h2c 后同一端口可服务明文 HTTP/2。
func TestEngineH2CServesUnencryptedHTTP2(t *testing.T) {
	engine := NewEngine(WithH2C(), DisableStartupLog())
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听测试端口失败: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- engine.RunListener(listener) }()

	// 只启用明文 HTTP/2：标准库直接发送连接序言（前导知识），不做 Upgrade 握手。
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols}
	defer transport.CloseIdleConnections()

	address := "http://" + listener.Addr().String() + "/ping"
	response, err := (&http.Client{Transport: transport, Timeout: 2 * time.Second}).Get(address)
	if err != nil {
		t.Fatalf("明文 HTTP/2 请求失败: %v", err)
	}

	payload, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	if response.ProtoMajor != 2 {
		t.Fatalf("协商到的协议 = %s，期望 HTTP/2", response.Proto)
	}

	if response.StatusCode != http.StatusOK || string(payload) != "pong" {
		t.Fatalf("响应 = (%d, %q)", response.StatusCode, payload)
	}

	// 同一端口仍需接受普通 HTTP/1.1 客户端，否则启用 h2c 会踢掉存量调用方。
	response, err = (&http.Client{Timeout: 2 * time.Second}).Get(address)
	if err != nil {
		t.Fatalf("HTTP/1.1 请求失败: %v", err)
	}

	_ = response.Body.Close()
	if response.ProtoMajor != 1 {
		t.Fatalf("默认客户端协商到的协议 = %s，期望 HTTP/1.1", response.Proto)
	}

	if err := engine.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunListener() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("优雅关闭后 RunListener 未返回")
	}
}

// TestEngineRunReportsStartupErrors 验证启动失败路径与错误归一化。
func TestEngineRunReportsStartupErrors(t *testing.T) {
	engine := createTestEngine()

	// 非法地址会让 ListenAndServe 立刻返回错误。
	if err := engine.Run("invalid-address"); err == nil {
		t.Fatal("非法监听地址必须返回错误")
	}

	if engine.GetServer() == nil {
		t.Fatal("Run 应记录服务端指针，便于 Shutdown 使用")
	}

	// 未启动时 Shutdown 必须安全返回。
	if err := createTestEngine().Shutdown(nil); err != nil {
		t.Fatalf("未启动时 Shutdown() = %v, want nil", err)
	}

	tls := createTestEngine()
	if err := tls.RunTLS("127.0.0.1:0", "", ""); err == nil {
		t.Fatal("缺少证书时必须返回错误")
	}

	if err := tls.RunFd(-1); err == nil {
		t.Fatal("非法文件描述符必须返回错误")
	}
}

// TestEngineRunUnixServesAndShutdown 验证 unix 套接字的启动、请求与优雅关闭。
//
// Go 在 Windows 上也支持 AF_UNIX，因此这里不再按平台跳过，
// 而是真实启动服务并验证请求与退出。
func TestEngineRunUnixServesAndShutdown(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "leego.sock")

	engine := createTestEngine()
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	result := make(chan error, 1)
	go func() {
		result <- engine.RunUnix(socketPath)
	}()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{Transport: transport}

	var response *http.Response
	var err error

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, err = client.Get("http://unix/ping")
		if err == nil {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	if err != nil {
		t.Fatalf("unix 套接字请求失败: %v", err)
	}

	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()

	if readErr != nil {
		t.Fatalf("读取响应体失败: %v", readErr)
	}

	if string(body) != "pong" {
		t.Fatalf("响应体 = %q, want pong", body)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := engine.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("RunUnix() error = %v", err)
		}

	case <-time.After(3 * time.Second):
		t.Fatal("RunUnix 未在关闭后退出")
	}
}

// TestEngineParsePrefix 验证可信代理网段解析。
func TestEngineParsePrefix(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "IPv4 地址", input: "10.0.0.1", want: "10.0.0.1/32"},
		{name: "IPv4 网段", input: "10.0.0.0/8", want: "10.0.0.0/8"},
		{name: "IPv6 地址", input: "::1", want: "::1/128"},
		{name: "带空白", input: "  192.168.0.0/16  ", want: "192.168.0.0/16"},
		{name: "空字符串", input: "", wantErr: true},
		{name: "空白字符串", input: "   ", wantErr: true},
		{name: "非法值", input: "not-an-ip", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			prefix, err := parsePrefix(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("parsePrefix(%q) 应返回错误", testCase.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("parsePrefix(%q) error = %v", testCase.input, err)
			}

			if got := prefix.String(); got != testCase.want {
				t.Fatalf("parsePrefix(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

// TestSortSnapshotsOrdersByMethodAndPattern 验证快照排序的稳定规则。
func TestSortSnapshotsOrdersByMethodAndPattern(t *testing.T) {
	snapshots := []collector.Snapshot{
		{Method: http.MethodPost, Pattern: "/orders"},
		{Method: http.MethodGet, Pattern: "/users"},
		{Method: http.MethodGet, Pattern: "/orders"},
	}

	sortSnapshots(snapshots)

	if snapshots[0].Method != http.MethodGet || snapshots[0].Pattern != "/orders" {
		t.Fatalf("第 1 项 = %+v, want GET /orders", snapshots[0])
	}

	if snapshots[1].Method != http.MethodGet || snapshots[1].Pattern != "/users" {
		t.Fatalf("第 2 项 = %+v, want GET /users", snapshots[1])
	}

	if snapshots[2].Method != http.MethodPost || snapshots[2].Pattern != "/orders" {
		t.Fatalf("第 3 项 = %+v, want POST /orders", snapshots[2])
	}
}

// TestGroupSetStaticFSRejectsNil 验证 nil 文件系统在注册期快速失败。
func TestGroupSetStaticFSRejectsNil(t *testing.T) {
	engine := createTestEngine()
	group := engine.NewGroup("/static")

	defer func() {
		if recover() == nil {
			t.Fatal("nil 文件系统必须 panic")
		}
	}()

	group.SetStaticFS("/assets", nil)
}

// TestEngineRecoversHandlerPanic 验证未挂恢复中间件时 panic 也返回 500。
func TestEngineRecoversHandlerPanic(t *testing.T) {
	engine := createTestEngine()

	engine.GET("/panic", func(*Context) {
		panic("boom")
	}, "panic")

	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	}, "ping")

	if recorder := performRequest(engine, http.MethodGet, "/panic", nil); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic 请求状态码 = %d, want 500", recorder.Code)
	}

	// 后续请求仍能正常处理，说明上下文与写入器已归还对象池。
	if recorder := performRequest(engine, http.MethodGet, "/ping", nil); recorder.Code != http.StatusOK {
		t.Fatalf("后续请求状态码 = %d, want 200", recorder.Code)
	}
}

// BenchmarkEngineServeHTTP 测量端到端请求处理开销。
func BenchmarkEngineServeHTTP(b *testing.B) {
	engine := NewEngine(DisableStartupLog())
	engine.GET("/users/:id", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("id"))
	})

	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
	}
}

// BenchmarkEngineServeHTTPParallel 测量并发请求处理能力。
func BenchmarkEngineServeHTTPParallel(b *testing.B) {
	engine := NewEngine(DisableStartupLog())
	engine.GET("/users/:id", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("id"))
	})

	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
		}
	})
}
