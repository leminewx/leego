package leego

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego/metrics"
	"gitee.com/leminewx/leego/openapi"
)

// trackingStore 记录 Close 是否被调用，用于验证关闭钩子接线。
type trackingStore struct {
	metrics.Store
	// closed 标记 Close 已执行。
	closed atomic.Bool
}

// Close 标记关闭并透传给内部存储。
func (own *trackingStore) Close() error {
	own.closed.Store(true)
	return own.Store.Close()
}

// waitForMetricsPoints 轮询存储，直到出现至少一个时间片或超时。
func waitForMetricsPoints(t *testing.T, store metrics.Store) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		now := time.Now()
		points, err := store.Query(now.Add(-time.Minute), now, time.Second)
		if err != nil {
			t.Fatalf("查询指标失败: %v", err)
		}

		if len(points) > 0 && points[len(points)-1].Total > 0 {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("等待后台采集超时")
}

// TestRegisterMetricsExposesRoutes 验证指标路由、后台采集与关闭钩子。
func TestRegisterMetricsExposesRoutes(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	}, "ping")

	store := &trackingStore{Store: metrics.NewMemoryStore(time.Minute, 60)}
	err := engine.RegisterMetrics("/metrics", store,
		WithMetricsInterval(5*time.Millisecond),
		WithMetricsStep(time.Second),
	)
	if err != nil {
		t.Fatalf("RegisterMetrics() error = %v", err)
	}

	if recorder := performRequest(engine, http.MethodGet, "/ping", nil); recorder.Code != http.StatusOK {
		t.Fatalf("业务路由状态码 = %d, want 200", recorder.Code)
	}

	waitForMetricsPoints(t, store)

	history := performRequest(engine, http.MethodGet, "/metrics", nil)
	if history.Code != http.StatusOK {
		t.Fatalf("历史接口状态码 = %d, want 200", history.Code)
	}

	body := history.Body.String()
	for _, want := range []string{`"generated_at"`, `"step"`, `"points"`, `"total"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("历史接口缺少 %q\n%s", want, body)
		}
	}

	prometheus := performRequest(engine, http.MethodGet, "/metrics/prometheus", nil)
	if prometheus.Code != http.StatusOK {
		t.Fatalf("Prometheus 状态码 = %d, want 200", prometheus.Code)
	}

	if contentType := prometheus.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("Prometheus Content-Type = %q", contentType)
	}

	if !strings.Contains(prometheus.Body.String(), "leego_http_requests_total") {
		t.Fatalf("Prometheus 缺少指标输出\n%s", prometheus.Body.String())
	}

	if err = engine.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	// 关闭钩子必须释放存储，避免指标继续写入已关闭的句柄。
	if !store.closed.Load() {
		t.Fatal("Shutdown() 未调用 store.Close()")
	}
}

// TestRegisterMetricsAuthorizer 验证指标端点可加访问控制。
func TestRegisterMetricsAuthorizer(t *testing.T) {
	engine := createTestEngine()
	allowed := false

	err := engine.RegisterMetrics("/metrics", metrics.NewMemoryStore(time.Minute, 60),
		WithMetricsInterval(time.Hour),
		WithMetricsAuthorizer(func(*Context) bool { return allowed }),
	)
	if err != nil {
		t.Fatalf("RegisterMetrics() error = %v", err)
	}

	if recorder := performRequest(engine, http.MethodGet, "/metrics", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未授权时历史接口状态码 = %d, want 401", recorder.Code)
	}

	if recorder := performRequest(engine, http.MethodGet, "/metrics/prometheus", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未授权时 Prometheus 状态码 = %d, want 401", recorder.Code)
	}

	allowed = true

	if recorder := performRequest(engine, http.MethodGet, "/metrics", nil); recorder.Code != http.StatusOK {
		t.Fatalf("授权后历史接口状态码 = %d, want 200", recorder.Code)
	}

	if recorder := performRequest(engine, http.MethodGet, "/metrics/prometheus", nil); recorder.Code != http.StatusOK {
		t.Fatalf("授权后 Prometheus 状态码 = %d, want 200", recorder.Code)
	}
}

// TestRegisterMetricsRequiresStore 验证空存储被拒绝。
func TestRegisterMetricsRequiresStore(t *testing.T) {
	engine := createTestEngine()
	if err := engine.RegisterMetrics("/metrics", nil); err == nil {
		t.Fatal("空存储必须返回错误")
	}
}

// TestRegisterMetricsDisablePrometheus 验证可关闭抓取路由。
func TestRegisterMetricsDisablePrometheus(t *testing.T) {
	engine := createTestEngine()
	store := metrics.NewMemoryStore(time.Minute, 60)
	err := engine.RegisterMetrics("/metrics", store,
		WithMetricsInterval(time.Hour),
		DisableMetricsPrometheus(),
	)
	if err != nil {
		t.Fatalf("RegisterMetrics() error = %v", err)
	}

	if recorder := performRequest(engine, http.MethodGet, "/metrics", nil); recorder.Code != http.StatusOK {
		t.Fatalf("历史接口状态码 = %d, want 200", recorder.Code)
	}

	if recorder := performRequest(engine, http.MethodGet, "/metrics/prometheus", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("Prometheus 状态码 = %d, want 404", recorder.Code)
	}
}

// TestRegisterOpenAPIExposesRoutes 验证文档与页面路由。
func TestRegisterOpenAPIExposesRoutes(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/users/:id", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("id"))
	}, "get-user")
	engine.POST("/orders", func(ctx *Context) {
		_ = ctx.ResponseJSON(http.StatusCreated, map[string]bool{"ok": true})
	}, "post-order")

	err := engine.RegisterOpenAPI("/docs", openapi.WithTitle("订单服务"), openapi.WithVersion("2.1.0"))
	if err != nil {
		t.Fatalf("RegisterOpenAPI() error = %v", err)
	}

	document := performRequest(engine, http.MethodGet, "/docs/openapi.json", nil)
	if document.Code != http.StatusOK {
		t.Fatalf("文档状态码 = %d, want 200", document.Code)
	}

	if contentType := document.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("文档 Content-Type = %q", contentType)
	}

	body := document.Body.String()
	for _, want := range []string{
		`"openapi": "3.1.0"`,
		`"订单服务"`,
		`"2.1.0"`,
		`"/users/{id}"`,
		`"/orders"`,
		`"operationId": "get-user"`,
		`"name": "id"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("文档缺少 %q\n%s", want, body)
		}
	}

	page := performRequest(engine, http.MethodGet, "/docs", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("页面状态码 = %d, want 200", page.Code)
	}

	if contentType := page.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("页面 Content-Type = %q", contentType)
	}

	if !strings.Contains(page.Body.String(), "openapi.json") {
		t.Fatal("页面未引用 openapi.json")
	}
}

// TestRegisterOpenAPIEmptyPrefix 验证空前缀回退到默认挂载点。
func TestRegisterOpenAPIEmptyPrefix(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	}, "ping")

	if err := engine.RegisterOpenAPI(""); err != nil {
		t.Fatalf("RegisterOpenAPI() error = %v", err)
	}

	if recorder := performRequest(engine, http.MethodGet, "/openapi/openapi.json", nil); recorder.Code != http.StatusOK {
		t.Fatalf("文档状态码 = %d, want 200", recorder.Code)
	}

	if recorder := performRequest(engine, http.MethodGet, "/openapi", nil); recorder.Code != http.StatusOK {
		t.Fatalf("页面状态码 = %d, want 200", recorder.Code)
	}
}
