package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

// newTestMiddleware 构造仅用于单元测试的中间件实例。
func newTestMiddleware(methods ...string) *middleware {
	set := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		set[method] = struct{}{}
	}

	return &middleware{
		options: NewDefaultConfig(),
		methods: set,
		store:   newStore(time.Minute, 0, 1<<20),
	}
}

// TestHandlePassesThroughWithoutRequest 验证上下文没有请求时直接放行。
func TestHandlePassesThroughWithoutRequest(t *testing.T) {
	own := newTestMiddleware(http.MethodGet)
	called := false

	own.handle(func(*leego.Context) { called = true }, &leego.Context{})
	if !called {
		t.Fatal("无请求时仍应执行后续处理器")
	}
}

// TestHandlePassesThroughUnsupportedMethod 验证非缓存方法直接放行。
func TestHandlePassesThroughUnsupportedMethod(t *testing.T) {
	own := newTestMiddleware(http.MethodGet)
	called := false

	ctx := leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/submit", nil))
	own.handle(func(*leego.Context) { called = true }, ctx)
	if !called {
		t.Fatal("POST 请求不应进入缓存流程")
	}
}

// TestWriteEntryReplaysCachedResponse 验证缓存回放补齐 Content-Length 与命中标记。
func TestWriteEntryReplaysCachedResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx := leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/cached", nil))

	item := &entry{
		status: http.StatusOK,
		header: http.Header{"Content-Type": {"text/plain"}, _HEADER_X_CACHE: {_CACHE_HIT}},
		body:   []byte("payload"),
	}
	writeEntry(ctx, item, _CACHE_MISS)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if tag := recorder.Header().Get(_HEADER_X_CACHE); tag != _CACHE_MISS {
		t.Fatalf("%s = %q, want %q", _HEADER_X_CACHE, tag, _CACHE_MISS)
	}

	if length := recorder.Header().Get("Content-Length"); length != "7" {
		t.Fatalf("Content-Length = %q, want 7", length)
	}

	if body := recorder.Body.String(); body != "payload" {
		t.Fatalf("响应体 = %q", body)
	}
}

// TestWriteEntryHeadSkipsBody 验证 HEAD 命中只回写响应头。
func TestWriteEntryHeadSkipsBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx := leego.NewContext(recorder, httptest.NewRequest(http.MethodHead, "/cached", nil))

	item := &entry{status: http.StatusOK, header: make(http.Header), body: []byte("payload")}
	writeEntry(ctx, item, _CACHE_HIT)

	if recorder.Body.Len() != 0 {
		t.Fatalf("HEAD 响应不应写体: %q", recorder.Body.String())
	}

	if length := recorder.Header().Get("Content-Length"); length != "7" {
		t.Fatalf("Content-Length = %q, want 7", length)
	}
}
