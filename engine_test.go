package leego

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestEngine 构造一个用于测试的最小引擎
func newTestEngine() *Engine {
	return NewEngine()
}

// TestEngineRouting 验证 GET/POST/PUT/DELETE/PATCH/OPTIONS 路由注册与匹配
func TestEngineRouting(t *testing.T) {
	engine := newTestEngine()
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions}
	for _, m := range methods {
		m := m
		switch m {
		case http.MethodGet:
			engine.GET("/method", func(ctx *Context) { _ = ctx.ResponsePlain(http.StatusOK, m) })
		case http.MethodPost:
			engine.POST("/method", func(ctx *Context) { _ = ctx.ResponsePlain(http.StatusOK, m) })
		case http.MethodPut:
			engine.PUT("/method", func(ctx *Context) { _ = ctx.ResponsePlain(http.StatusOK, m) })
		case http.MethodDelete:
			engine.DELETE("/method", func(ctx *Context) { _ = ctx.ResponsePlain(http.StatusOK, m) })
		case http.MethodPatch:
			engine.PATCH("/method", func(ctx *Context) { _ = ctx.ResponsePlain(http.StatusOK, m) })
		case http.MethodOptions:
			engine.OPTIONS("/method", func(ctx *Context) { _ = ctx.ResponsePlain(http.StatusOK, m) })
		}
	}

	for _, m := range methods {
		req := httptest.NewRequest(m, "/method", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s /method 期望 200，实际 %d", m, rec.Code)
		}
		if rec.Body.String() != m {
			t.Errorf("%s /method 期望响应体 %q，实际 %q", m, m, rec.Body.String())
		}
	}
}

// TestEngineNotFound 验证未注册路由返回 404
func TestEngineNotFound(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/exists", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("未注册路由期望 404，实际 %d", rec.Code)
	}
}

// TestEngineURLBlacklist 验证黑名单拦截
func TestEngineURLBlacklist(t *testing.T) {
	engine := newTestEngine()
	engine.AddURLBlacklist("/secret")
	engine.GET("/secret", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/secret", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("黑名单路由期望 403，实际 %d", rec.Code)
	}
}

// TestEnginePathParam 验证参数路由（:id）的取值
func TestEnginePathParam(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/users/:id", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, ctx.GetPathValue("id"))
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if rec.Body.String() != "42" {
		t.Errorf("期望参数值 42，实际 %q", rec.Body.String())
	}
}

// TestEngineWildcard 验证通配符路由匹配多级路径
func TestEngineWildcard(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/files/*", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, ctx.GetPathValue("filepath"))
	})

	req := httptest.NewRequest(http.MethodGet, "/files/a/b/c.txt", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if rec.Body.String() != "a/b/c.txt" {
		t.Errorf("期望通配符捕获 a/b/c.txt，实际 %q", rec.Body.String())
	}
}

// TestEngineGroup 验证路由组的路由与中间件继承
func TestEngineGroup(t *testing.T) {
	engine := newTestEngine()
	v1 := engine.Group("/api/v1")
	v1.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Errorf("期望 pong，实际 %q", rec.Body.String())
	}
}

// TestEngineGroupMiddleware 验证组级中间件按注册顺序链式执行
func TestEngineGroupMiddleware(t *testing.T) {
	engine := newTestEngine()
	var order []string

	engine.UseMiddlewares(func(next HandleFunc) HandleFunc {
		return func(ctx *Context) {
			order = append(order, "engine")
			next(ctx)
		}
	})

	g := engine.Group("/g")
	g.UseMiddlewares(func(next HandleFunc) HandleFunc {
		return func(ctx *Context) {
			order = append(order, "group")
			next(ctx)
		}
	})
	g.GET("/x", func(ctx *Context) {
		order = append(order, "handler")
		_ = ctx.ResponsePlain(http.StatusOK, strings.Join(order, ","))
	})

	req := httptest.NewRequest(http.MethodGet, "/g/x", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	// 执行顺序应为 engine -> group -> handler
	if rec.Body.String() != "engine,group,handler" {
		t.Errorf("中间件执行顺序错误，实际 %q", rec.Body.String())
	}
}

// TestEngineHandlerDisable 验证禁用处理器返回 423 且不调用处理函数
func TestEngineHandlerDisable(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/disabled", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, "should-not-reach")
	})

	h, ok := engine.GetHandler(http.MethodGet, "/disabled")
	if !ok {
		t.Fatal("未检索到 /disabled 处理器")
	}
	h.Disable()

	req := httptest.NewRequest(http.MethodGet, "/disabled", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusLocked {
		t.Errorf("禁用路由期望 423，实际 %d", rec.Code)
	}
}

// TestEngineContextHelpers 验证上下文查询参数与错误处理辅助方法
func TestEngineContextHelpers(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/ctx", func(ctx *Context) {
		_ = ctx.ResponsePlain(http.StatusOK, ctx.GetQueryValue("q"))
	})

	req := httptest.NewRequest(http.MethodGet, "/ctx?q=hello", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Body.String() != "hello" {
		t.Errorf("期望查询参数 hello，实际 %q", rec.Body.String())
	}
}

// TestEngineResponseJSON 验证 JSON 响应与 Content-Type
func TestEngineResponseJSON(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/json", func(ctx *Context) {
		_ = ctx.ResponseJSON(http.StatusCreated, JSON{"name": "leego", "ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("期望 201，实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("期望 application/json，实际 %q", ct)
	}
	body := strings.TrimSpace(rec.Body.String())
	if !strings.Contains(body, `"name":"leego"`) {
		t.Errorf("JSON 响应体不符合预期: %q", body)
	}
}

// TestEngineResponseRedirect 验证重定向响应
func TestEngineResponseRedirect(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/old", func(ctx *Context) {
		ctx.ResponseRedirect(http.StatusMovedPermanently, "/new")
	})

	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("期望 301，实际 %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/new" {
		t.Errorf("期望 Location /new，实际 %q", loc)
	}
}

// TestEngineGetHandler 验证 GetHandler 能按方法与模式检索已注册处理器
func TestEngineGetHandler(t *testing.T) {
	engine := newTestEngine()
	engine.GET("/lookup", func(ctx *Context) {})

	_, ok := engine.GetHandler(http.MethodGet, "/lookup")
	if !ok {
		t.Errorf("GetHandler 检索失败: ok=%v", ok)
	}

	if _, ok := engine.GetHandler(http.MethodPost, "/lookup"); ok {
		t.Error("未注册的 POST /lookup 不应被检索到")
	}
}
