package leego

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTestHandler 构造一条绑定到新建引擎的处理器。
//
// 返回的引擎未进入运行期，便于测试继续注册路由或中间件。
func createTestHandler(t *testing.T, method, pattern string, tags ...string) (*Engine, Handler) {
	t.Helper()

	engine := NewEngine(DisableStartupLog())
	engine.GET(pattern, func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "ok")
	}, tags...)

	handler, ok := engine.GetHandler(method, pattern)
	if !ok {
		t.Fatalf("未检索到处理器 %s %s", method, pattern)
	}

	return engine, handler
}

// TestNewHandler 验证处理器的元信息与参数提取。
func TestNewHandler(t *testing.T) {
	_, route := createTestHandler(t, http.MethodGet, "/users/:id/*path", "查询用户", "user", "query")

	monitor := route.GetCollector()
	if monitor == nil {
		t.Fatal("GetCollector() 不允许返回 nil")
	}

	if monitor.GetMethod() != http.MethodGet {
		t.Fatalf("method = %q, want GET", monitor.GetMethod())
	}

	if monitor.GetPattern() != "/users/:id/*path" {
		t.Fatalf("pattern = %q", monitor.GetPattern())
	}

	if monitor.GetName() != "查询用户" {
		t.Fatalf("name = %q, want 查询用户", monitor.GetName())
	}

	names := route.(*handler).GetParamNames()
	if len(names) != 2 || names[0] != "id" || names[1] != "path" {
		t.Fatalf("参数名 = %v, want [id path]", names)
	}

	// 返回值必须是副本，调用方修改不能影响处理器内部状态。
	names[0] = "changed"
	if route.(*handler).GetParamNames()[0] != "id" {
		t.Fatal("GetParamNames 必须返回副本")
	}
}

// TestNewHandlerWithoutGroup 验证未绑定路由组的处理器仍可执行。
func TestNewHandlerWithoutGroup(t *testing.T) {
	executed := false
	handler := newHandler(nil, http.MethodGet, "/standalone", func(ctx *Context) {
		executed = true
		_ = ctx.ResponseText(http.StatusOK, "standalone")
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(NewContext(recorder, httptest.NewRequest(http.MethodGet, "/standalone", nil)))

	if !executed {
		t.Fatal("未绑定路由组时处理器仍应执行")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}
}

// TestHandlerPrepareChainOrder 验证组级中间件按“由外到内”的顺序执行，
// 且根路由组（引擎级）中间件不会在处理器内重复执行。
func TestHandlerPrepareChainOrder(t *testing.T) {
	engine := NewEngine(DisableStartupLog())

	order := make([]string, 0, 4)
	record := func(name string) MiddlewareFunc {
		return func(next HandleFunc) HandleFunc {
			return func(ctx *Context) {
				order = append(order, name)
				next(ctx)
			}
		}
	}

	engine.UseMiddlewares(record("root"))

	group := engine.NewGroup("/api")
	group.UseMiddlewares(record("outer"), record("inner"))
	group.GET("/ping", func(ctx *Context) {
		order = append(order, "handler")
		_ = ctx.ResponseText(http.StatusOK, "ok")
	})

	handler, ok := engine.GetHandler(http.MethodGet, "/api/ping")
	if !ok {
		t.Fatal("未检索到处理器")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(NewContext(recorder, httptest.NewRequest(http.MethodGet, "/api/ping", nil)))

	want := []string{"outer", "inner", "handler"}
	if len(order) != len(want) {
		t.Fatalf("执行顺序 = %v, want %v（根中间件由引擎执行，不应重复）", order, want)
	}

	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("执行顺序 = %v, want %v", order, want)
		}
	}
}

// TestHandlerPrepareIsIdempotent 验证 Prepare 只折叠一次中间件链。
func TestHandlerPrepareIsIdempotent(t *testing.T) {
	engine := NewEngine(DisableStartupLog())

	builds := 0
	group := engine.NewGroup("/api")
	group.UseMiddlewares(func(next HandleFunc) HandleFunc {
		builds++
		return next
	})
	group.GET("/ping", func(ctx *Context) {})

	handler, ok := engine.GetHandler(http.MethodGet, "/api/ping")
	if !ok {
		t.Fatal("未检索到处理器")
	}

	handler.Prepare()
	handler.Prepare()
	handler.Prepare()

	if builds != 1 {
		t.Fatalf("中间件链折叠次数 = %d, want 1（Prepare 必须幂等）", builds)
	}
}

// TestHandlerEnableDisable 验证路由启停状态。
func TestHandlerEnableDisable(t *testing.T) {
	engine, handler := createTestHandler(t, http.MethodGet, "/toggle")

	if !handler.IsEnabled() {
		t.Fatal("路由创建后默认必须启用")
	}

	handler.Disable()
	if handler.IsEnabled() {
		t.Fatal("Disable 后必须为禁用状态")
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/toggle", nil))

	if recorder.Code != http.StatusLocked {
		t.Fatalf("禁用路由状态码 = %d, want 423", recorder.Code)
	}

	handler.Enable()
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/toggle", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("重新启用后状态码 = %d, want 200", recorder.Code)
	}
}
