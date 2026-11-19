package leego

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// assertPanics 断言给定函数会触发 panic。
func assertPanics(t *testing.T, name string, action func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatalf("%s 必须触发 panic", name)
		}
	}()

	action()
}

// TestJoinRoutePath 验证路由前缀拼接与规范化。
func TestJoinRoutePath(t *testing.T) {
	tests := []struct {
		prefix  string
		pattern string
		want    string
	}{
		{"/api", "/users", "/api/users"},
		{"/api/", "users", "/api/users"},
		{"/api", "/", "/api"},
		{"/api", "", "/api"},
		{"", "/", "/"},
		{"/", "/ping", "/ping"},
		{"/api", "v1//users/", "/api/v1/users"},
		{"api", "users", "/api/users"},
	}

	for _, test := range tests {
		if got := joinRoutePath(test.prefix, test.pattern); got != test.want {
			t.Errorf("joinRoutePath(%q, %q) = %q, want %q", test.prefix, test.pattern, got, test.want)
		}
	}
}

// TestContainsPathTraversal 验证路径穿越检测。
func TestContainsPathTraversal(t *testing.T) {
	blocked := []string{
		"../etc/passwd",
		"a/../../etc/passwd",
		"..",
		"a/..",
		`..\..\windows\win.ini`,
		"/etc/passwd",
		"C:/windows/win.ini",
		"//server/share/file",
		"a/\x00b",
	}

	for _, filePath := range blocked {
		if !containsPathTraversal(filePath) {
			t.Errorf("应拦截但放行: %q", filePath)
		}
	}

	allowed := []string{
		"",
		"css/style.css",
		"images/photo.jpg",
		"./style.css",
		"a//b.css",
		"..foo/bar.txt",
		"foo..bar.css",
		"my.file.name.txt",
	}

	for _, filePath := range allowed {
		if containsPathTraversal(filePath) {
			t.Errorf("合法路径被拦截: %q", filePath)
		}
	}
}

// TestNewGroupInheritsPrefix 验证子路由组继承前缀并可注册路由。
func TestNewGroupInheritsPrefix(t *testing.T) {
	engine := NewEngine(DisableStartupLog())

	api := engine.NewGroup("/api")
	v1 := api.NewGroup("v1")
	v1.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	if _, ok := engine.GetHandler(http.MethodGet, "/api/v1/ping"); !ok {
		t.Fatal("子路由组未继承父组前缀")
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "pong" {
		t.Fatalf("响应 = (%d, %q), want (200, pong)", recorder.Code, recorder.Body.String())
	}
}

// TestUseMiddlewaresIgnoresNil 验证 nil 中间件被忽略。
func TestUseMiddlewaresIgnoresNil(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	group := engine.NewGroup("/api").(*group)

	middleware := func(next HandleFunc) HandleFunc { return next }
	group.UseMiddlewares(nil, middleware, nil)

	if len(group.middlewares) != 1 {
		t.Fatalf("中间件数量 = %d, want 1", len(group.middlewares))
	}
}

// TestUseMiddlewaresPanicsAfterPrepare 验证运行期禁止追加中间件。
func TestUseMiddlewaresPanicsAfterPrepare(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	group := engine.NewGroup("/api")
	engine.Prepare()

	assertPanics(t, "运行期追加中间件", func() {
		group.UseMiddlewares(func(next HandleFunc) HandleFunc { return next })
	})
}

// TestRegisterRoutePanicsAfterPrepare 验证运行期禁止注册路由。
func TestRegisterRoutePanicsAfterPrepare(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	engine.Prepare()

	assertPanics(t, "运行期注册路由", func() {
		engine.GET("/late", func(ctx *Context) {})
	})
}

// TestAddRoutePanicsOnDuplicate 验证重复注册同一路由会 panic。
func TestAddRoutePanicsOnDuplicate(t *testing.T) {
	engine := NewEngine(DisableStartupLog())
	engine.GET("/dup", func(ctx *Context) {})

	assertPanics(t, "重复注册路由", func() {
		engine.GET("/dup", func(ctx *Context) {})
	})
}

// TestAddRoutePanicsOnNilHandler 验证 nil 处理函数被拒绝。
func TestAddRoutePanicsOnNilHandler(t *testing.T) {
	engine := NewEngine(DisableStartupLog())

	assertPanics(t, "注册 nil 处理函数", func() {
		engine.GET("/nil", nil)
	})
}

// TestSetStaticServesFiles 验证静态目录挂载、安全头与穿越拦截。
func TestSetStaticServesFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "hello.txt"), []byte("hello static"), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	engine := NewEngine(DisableStartupLog())
	engine.SetStatic("/static", directory)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/hello.txt", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if recorder.Body.String() != "hello static" {
		t.Fatalf("响应体 = %q, want hello static", recorder.Body.String())
	}

	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}

	if got := recorder.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}

	// 路径穿越请求必须被拦截，且不得泄露文件内容。
	traversal := httptest.NewRecorder()
	engine.ServeHTTP(traversal, httptest.NewRequest(http.MethodGet, "/static/../etc/passwd", nil))

	if traversal.Code != http.StatusForbidden {
		t.Fatalf("路径穿越状态码 = %d, want 403", traversal.Code)
	}

	// 未配置的静态文件返回 404。
	missing := httptest.NewRecorder()
	engine.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/static/missing.txt", nil))

	if missing.Code != http.StatusNotFound {
		t.Fatalf("缺失文件状态码 = %d, want 404", missing.Code)
	}
}

// TestSetStaticPanicsOnInvalidConfig 验证静态挂载的配置错误在启动期暴露。
func TestSetStaticPanicsOnInvalidConfig(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	assertPanics(t, "挂载不存在的目录", func() {
		NewEngine(DisableStartupLog()).SetStatic("/static", filepath.Join(directory, "missing"))
	})

	assertPanics(t, "挂载普通文件", func() {
		NewEngine(DisableStartupLog()).SetStatic("/static", file)
	})

	assertPanics(t, "挂载含参数路径", func() {
		NewEngine(DisableStartupLog()).SetStatic("/static/:version", directory)
	})

	assertPanics(t, "挂载空文件系统", func() {
		NewEngine(DisableStartupLog()).SetStaticFS("/static", nil)
	})

	engine := NewEngine(DisableStartupLog())
	engine.SetStatic("/static", directory)
	assertPanics(t, "重复挂载同一路径", func() {
		engine.SetStatic("/static", directory)
	})
}
