package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

// TestProxyOptionsNormalize 验证零值与负数配置回退到安全值。
func TestProxyOptionsNormalize(t *testing.T) {
	options := Options{Timeout: -time.Second}.normalize()

	if options.Transport == nil {
		t.Fatal("Transport = nil, want 默认连接池")
	}

	if options.TagHeader != _DEFAULT_TAG_HEADER {
		t.Fatalf("TagHeader = %q, want %q", options.TagHeader, _DEFAULT_TAG_HEADER)
	}

	if options.TimeoutHeader != _DEFAULT_TIMEOUT_HEADER {
		t.Fatalf("TimeoutHeader = %q, want %q", options.TimeoutHeader, _DEFAULT_TIMEOUT_HEADER)
	}

	if options.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0", options.Timeout)
	}
}

// TestProxyOptionsApply 验证各选项写入目标字段。
func TestProxyOptionsApply(t *testing.T) {
	options := NewDefaultOptions()
	transport := &http.Transport{}
	called := false

	UseTransport(transport)(&options)
	UseFlushInterval(-time.Second)(&options)
	UseTimeout(2 * time.Second)(&options)
	UseTagHeader("X-Env")(&options)
	UseTimeoutHeader("X-Budget")(&options)
	UseErrorHandler(func(*leego.Context, *url.URL, error) { called = true })(&options)

	if options.Transport != transport || options.FlushInterval != -time.Second || options.Timeout != 2*time.Second {
		t.Fatalf("Options = %+v", options)
	}

	if options.TagHeader != "X-Env" || options.TimeoutHeader != "X-Budget" {
		t.Fatalf("请求头配置 = %+v", options)
	}

	options.OnError(nil, nil, nil)
	if !called {
		t.Fatal("OnError 未被设置")
	}
}

// TestSelectUpstreamPrefersTag 验证命中标签时优先返回同标签上游。
func TestSelectUpstreamPrefersTag(t *testing.T) {
	stable := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer stable.Close()
	canary := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer canary.Close()

	stableURL, _ := url.Parse(stable.URL)
	canaryURL, _ := url.Parse(canary.URL)
	balancer, err := NewBalancer(
		Upstream{URL: stableURL, Weight: 9, Tag: "stable"},
		Upstream{URL: canaryURL, Weight: 1, Tag: "v2"},
	)
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	current := &proxy{balancer: balancer, options: NewDefaultOptions()}
	engine := leego.NewEngine(leego.DisableStartupLog())
	engine.GET("/pick", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, map[string]string{
			"target": current.selectUpstream(ctx).String(),
		})
	}, "pick")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/pick", nil)
	request.Header.Set(_DEFAULT_TAG_HEADER, "v2")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if payload["target"] != canaryURL.String() {
		t.Fatalf("target = %q, want 灰度上游 %q", payload["target"], canaryURL.String())
	}
}
