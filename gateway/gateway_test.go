package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

// mustParseURL 解析测试用上游地址。
func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	target, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析地址失败: %v", err)
	}

	return target
}

// newProxyEngine 构造带反向代理的引擎。
func newProxyEngine(balancer *Balancer, options ...Option) *leego.Engine {
	engine := leego.NewEngine(leego.DisableStartupLog())
	handler := NewProxy(balancer, options...)
	engine.GET("/api/*path", handler, "proxy")
	engine.POST("/api/*path", handler, "proxy-post")
	return engine
}

// perform 在引擎上执行一次请求。
func perform(engine *leego.Engine, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestNewBalancerValidation(t *testing.T) {
	if _, err := NewBalancer(); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("空上游错误 = %v, want ErrNoUpstream", err)
	}

	if _, err := NewBalancer(Upstream{Weight: 1}); !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("nil URL 错误 = %v, want ErrInvalidUpstream", err)
	}

	if _, err := NewBalancer(Upstream{URL: &url.URL{Host: "example.com"}, Weight: 1}); !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("缺少 scheme 错误 = %v, want ErrInvalidUpstream", err)
	}

	if _, err := NewBalancer(Upstream{URL: mustParseURL(t, "http://example.com"), Weight: -1}); !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("负权重错误 = %v, want ErrInvalidUpstream", err)
	}

	balancer, err := NewBalancer(
		Upstream{URL: mustParseURL(t, "http://one.example"), Weight: 0},
		Upstream{URL: mustParseURL(t, "http://two.example"), Weight: 2},
	)
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	if balancer.CountUpstreams() != 1 {
		t.Fatalf("上游数量 = %d, want 1", balancer.CountUpstreams())
	}
}

func TestBalancerWeightedDistribution(t *testing.T) {
	balancer, err := NewBalancer(
		Upstream{URL: mustParseURL(t, "http://a.example"), Weight: 3},
		Upstream{URL: mustParseURL(t, "http://b.example"), Weight: 1},
	)
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	counts := make(map[string]int, 2)
	for index := 0; index < 400; index++ {
		counts[balancer.PickNext().Host]++
	}

	if counts["a.example"] != 300 || counts["b.example"] != 100 {
		t.Fatalf("权重分配 = %v, want 300/100", counts)
	}
}

func TestBalancerTagRouting(t *testing.T) {
	balancer, err := NewBalancer(
		Upstream{URL: mustParseURL(t, "http://stable.example"), Weight: 1},
		Upstream{URL: mustParseURL(t, "http://canary.example"), Weight: 1, Tag: "v2"},
	)
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	for index := 0; index < 20; index++ {
		if host := balancer.PickNextFor("v2").Host; host != "canary.example" {
			t.Fatalf("灰度命中 = %q, want canary.example", host)
		}
	}

	// 未知标签回落到全量分配，两个上游都应出现。
	seen := make(map[string]struct{}, 2)
	for index := 0; index < 8; index++ {
		seen[balancer.PickNextFor("unknown").Host] = struct{}{}
	}

	if len(seen) != 2 {
		t.Fatalf("回落分配只有 %v", seen)
	}

	upstreams := balancer.GetUpstreams()
	if len(upstreams) != 2 || upstreams[1].Tag != "v2" {
		t.Fatalf("上游快照异常: %+v", upstreams)
	}
}

func TestProxyForwardsRequest(t *testing.T) {
	var forwarded atomic.Bool

	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Forwarded-For") != "" {
			forwarded.Store(true)
		}

		_ = json.NewEncoder(resp).Encode(map[string]string{
			"path":  request.URL.Path,
			"query": request.URL.RawQuery,
			"host":  request.Host,
		})
	}))
	t.Cleanup(upstream.Close)

	balancer, err := NewBalancer(Upstream{URL: mustParseURL(t, upstream.URL), Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer)
	recorder := perform(engine, http.MethodGet, "/api/users?limit=10", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d: %s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]string
	if err = json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if payload["path"] != "/api/users" || payload["query"] != "limit=10" {
		t.Fatalf("转发结果异常: %+v", payload)
	}

	if !forwarded.Load() {
		t.Fatal("缺少 X-Forwarded-For")
	}
}

func TestProxyForwardsBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		_, _ = resp.Write(body)
	}))
	t.Cleanup(upstream.Close)

	balancer, err := NewBalancer(Upstream{URL: mustParseURL(t, upstream.URL), Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer)
	recorder := perform(engine, http.MethodPost, "/api/echo", strings.NewReader("leego"), nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d: %s", recorder.Code, recorder.Body.String())
	}

	if body := recorder.Body.String(); body != "leego" {
		t.Fatalf("响应体 = %q, want leego", body)
	}
}

func TestProxyCanaryRouting(t *testing.T) {
	newUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
			_, _ = resp.Write([]byte(name))
		}))
	}

	stable := newUpstream("stable")
	canary := newUpstream("canary")
	t.Cleanup(stable.Close)
	t.Cleanup(canary.Close)

	balancer, err := NewBalancer(
		Upstream{URL: mustParseURL(t, stable.URL), Weight: 1, Tag: "stable"},
		Upstream{URL: mustParseURL(t, canary.URL), Weight: 1, Tag: "v2"},
	)
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer)

	headers := map[string]string{_DEFAULT_TAG_HEADER: "v2"}
	for index := 0; index < 5; index++ {
		recorder := perform(engine, http.MethodGet, "/api/hello", nil, headers)
		if body := recorder.Body.String(); body != "canary" {
			t.Fatalf("灰度响应 = %q, want canary", body)
		}
	}

	seen := make(map[string]struct{}, 2)
	for index := 0; index < 8; index++ {
		seen[perform(engine, http.MethodGet, "/api/hello", nil, nil).Body.String()] = struct{}{}
	}

	if len(seen) != 2 {
		t.Fatalf("未命中灰度时流量分布 = %v", seen)
	}
}

func TestProxyUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		_, _ = resp.Write([]byte("ok"))
	}))
	target := upstream.URL
	upstream.Close()

	balancer, err := NewBalancer(Upstream{URL: mustParseURL(t, target), Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	var reported atomic.Int64
	engine := newProxyEngine(balancer, UseErrorHandler(func(*leego.Context, *url.URL, error) {
		reported.Add(1)
	}))

	recorder := perform(engine, http.MethodGet, "/api/hello", nil, nil)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("状态码 = %d, want 502", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), "upstream unavailable") {
		t.Fatalf("错误响应异常: %s", recorder.Body.String())
	}

	// 502 不能回显上游地址，避免泄露内网拓扑。
	upstreamHost := mustParseURL(t, target).Host
	if strings.Contains(recorder.Body.String(), upstreamHost) {
		t.Fatalf("错误响应泄露了上游地址 %q: %s", upstreamHost, recorder.Body.String())
	}

	if reported.Load() != 1 {
		t.Fatalf("错误回调次数 = %d, want 1", reported.Load())
	}
}

// TestProxyDropsClientForwardedHeaders 验证客户端伪造的转发头不会透传到上游。
func TestProxyDropsClientForwardedHeaders(t *testing.T) {
	forwarded := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, request *http.Request) {
		forwarded <- request.Header.Get("X-Forwarded-For")
		resp.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	balancer, err := NewBalancer(Upstream{URL: mustParseURL(t, upstream.URL), Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer)
	recorder := perform(engine, http.MethodGet, "/api/hello", nil, map[string]string{
		"X-Forwarded-For":  "1.2.3.4",
		"X-Forwarded-Host": "evil.example",
	})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d: %s", recorder.Code, recorder.Body.String())
	}

	value := <-forwarded
	if strings.Contains(value, "1.2.3.4") {
		t.Fatalf("客户端伪造的 X-Forwarded-For 被透传: %q", value)
	}
}

func TestProxyTimeoutBudget(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, request *http.Request) {
		time.Sleep(10 * time.Millisecond)
		_, _ = resp.Write([]byte(request.Header.Get(_DEFAULT_TIMEOUT_HEADER)))
	}))
	t.Cleanup(upstream.Close)

	balancer, err := NewBalancer(Upstream{URL: mustParseURL(t, upstream.URL), Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer, UseTimeout(2*time.Second))
	recorder := perform(engine, http.MethodGet, "/api/slow", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d: %s", recorder.Code, recorder.Body.String())
	}

	if body := recorder.Body.String(); body != "2000" {
		t.Fatalf("超时预算头 = %q, want 2000", body)
	}
}

func TestProxyStreamsWithFlushInterval(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		flusher, _ := resp.(http.Flusher)
		_, _ = resp.Write([]byte("first;"))
		flusher.Flush()
		time.Sleep(5 * time.Millisecond)
		_, _ = resp.Write([]byte("second;"))
	}))
	t.Cleanup(upstream.Close)

	balancer, err := NewBalancer(Upstream{URL: mustParseURL(t, upstream.URL), Weight: 1})
	if err != nil {
		t.Fatalf("NewBalancer() error = %v", err)
	}

	engine := newProxyEngine(balancer, UseFlushInterval(-1))
	recorder := perform(engine, http.MethodGet, "/api/stream", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", recorder.Code)
	}

	if body := recorder.Body.String(); body != "first;second;" {
		t.Fatalf("流式响应体 = %q", body)
	}
}

func TestNewProxyPanicsOnEmptyBalancer(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("空负载均衡器必须 panic")
		}
	}()

	_ = NewProxy(nil)
}
