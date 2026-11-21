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

// TestProxySeesRuntimeUpstreamChanges 验证运行期增删上游无需重建代理。
func TestProxySeesRuntimeUpstreamChanges(t *testing.T) {
	newUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
			_, _ = resp.Write([]byte(name))
		}))
	}

	first := newUpstream("first")
	defer first.Close()
	second := newUpstream("second")
	defer second.Close()

	firstURL, _ := url.Parse(first.URL)
	secondURL, _ := url.Parse(second.URL)

	balancer, err := NewBalancer(Upstream{URL: firstURL, Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer)

	if body := perform(engine, http.MethodGet, "/api/ping", nil, nil).Body.String(); body != "first" {
		t.Fatalf("初始上游 = %q, want first", body)
	}

	// 新增上游后，代理应在下个请求就感知到并参与轮询。
	if err = balancer.Upsert(Upstream{URL: secondURL, Weight: 1}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	seen := make(map[string]int, 2)
	for index := 0; index < 4; index++ {
		seen[perform(engine, http.MethodGet, "/api/ping", nil, nil).Body.String()]++
	}

	if seen["first"] != 2 || seen["second"] != 2 {
		t.Fatalf("新增后的流量分布 = %v, want 2/2", seen)
	}

	// 删除上游后，代理不应再把流量转发到它。
	if err = balancer.Remove(firstURL); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	for index := 0; index < 3; index++ {
		if body := perform(engine, http.MethodGet, "/api/ping", nil, nil).Body.String(); body != "second" {
			t.Fatalf("删除后的上游 = %q, want second", body)
		}
	}
}

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

	WithTransport(transport)(&options)
	WithFlushInterval(-time.Second)(&options)
	WithTimeout(2 * time.Second)(&options)
	WithTagHeader("X-Env")(&options)
	WithTimeoutHeader("X-Budget")(&options)
	WithErrorHandler(func(*leego.Context, *url.URL, error) { called = true })(&options)

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

// TestProxyDoesNotMutateOriginalHeaders 验证代理改写超时头不会污染原始请求。
//
// 复用请求对象（如中间件链上的 ctx.Request）时，直接修改 Header 会影响
// 同一请求的其他中间件与日志；因此代理必须先在副本上改写。
func TestProxyDoesNotMutateOriginalHeaders(t *testing.T) {
	seen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, request *http.Request) {
		seen <- request.Header.Get(_DEFAULT_TIMEOUT_HEADER)
		_, _ = resp.Write([]byte("ok"))
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL)
	balancer, err := NewBalancer(Upstream{URL: target, Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer, WithTimeout(1500*time.Millisecond))

	request := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	request.Header.Set(_DEFAULT_TIMEOUT_HEADER, "1")

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if got := <-seen; got != "1500" {
		t.Fatalf("上游收到的超时头 = %q, want 1500", got)
	}

	if got := request.Header.Get(_DEFAULT_TIMEOUT_HEADER); got != "1" {
		t.Fatalf("原始请求头被改写 = %q, want 1", got)
	}
}
