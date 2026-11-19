package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/leminewx/leego"
)

// TestWrapFromHTTPHandler 验证标准库处理器适配。
func TestWrapFromHTTPHandler(t *testing.T) {
	standard := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		resp.WriteHeader(http.StatusCreated)
		_, _ = resp.Write([]byte("standard"))
	})

	nextCalled := false
	handle := WrapFromHTTPHandler(standard)(func(ctx *leego.Context) {
		nextCalled = true
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if recorder.Code != http.StatusCreated || recorder.Body.String() != "standard" {
		t.Fatalf("响应 = (%d, %q)", recorder.Code, recorder.Body.String())
	}

	if !nextCalled {
		t.Fatal("标准库处理器执行后仍必须调用 next")
	}
}

// TestWrapFromHTTPHandlerFunc 验证 HandlerFunc 适配与 nil 回退。
func TestWrapFromHTTPHandlerFunc(t *testing.T) {
	handle := WrapFromHTTPHandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		_, _ = resp.Write([]byte("func"))
	})(func(ctx *leego.Context) {})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if recorder.Body.String() != "func" {
		t.Fatalf("响应体 = %q, want func", recorder.Body.String())
	}

	nextCalled := false
	WrapFromHTTPHandlerFunc(nil)(func(ctx *leego.Context) { nextCalled = true })(
		leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)),
	)

	if !nextCalled {
		t.Fatal("nil 处理器应直接放行到 next")
	}

	WrapFromHTTPHandler(nil)(func(ctx *leego.Context) { nextCalled = true })(
		leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)),
	)

	if !nextCalled {
		t.Fatal("nil http.Handler 应直接放行到 next")
	}
}

// TestUnwrapFromMiddlewareFuncToHTTPHandler 验证解包为 http.Handler。
func TestUnwrapFromMiddlewareFuncToHTTPHandler(t *testing.T) {
	middleware := func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			ctx.SetResponseHeader("X-Middleware", "applied")
			ctx.Response.WriteHeader(http.StatusAccepted)
			next(ctx)
		}
	}

	handler := UnwrapFromMiddlewareFuncToHTTPHandler(middleware)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := recorder.Header().Get("X-Middleware"); got != "applied" {
		t.Fatalf("X-Middleware = %q, want applied", got)
	}

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("状态码 = %d, want 202", recorder.Code)
	}
}

// TestUnwrapFromMiddlewareFuncToHTTPHandleFunc 验证解包为 HandlerFunc 与 nil 回退。
func TestUnwrapFromMiddlewareFuncToHTTPHandleFunc(t *testing.T) {
	middleware := func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			ctx.SetResponseHeader("X-Unwrapped", "1")
			next(ctx)
		}
	}

	recorder := httptest.NewRecorder()
	UnwrapFromMiddlewareFuncToHTTPHandleFunc(middleware)(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := recorder.Header().Get("X-Unwrapped"); got != "1" {
		t.Fatalf("X-Unwrapped = %q, want 1", got)
	}

	// nil 中间件解包后应为空操作。
	recorder = httptest.NewRecorder()
	UnwrapFromMiddlewareFuncToHTTPHandleFunc(nil)(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("nil 中间件输出 = (%d, %q)", recorder.Code, recorder.Body.String())
	}
}
