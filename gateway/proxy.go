package gateway

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
)

const (
	// _DEFAULT_TAG_HEADER 为灰度标签请求头。
	_DEFAULT_TAG_HEADER = "X-Leego-Canary"
	// _DEFAULT_TIMEOUT_HEADER 为超时预算请求头。
	_DEFAULT_TIMEOUT_HEADER = "X-Request-Timeout"
)

// Options 定义反向代理行为。
type Options struct {
	// Transport 为上游连接池，nil 时使用 http.DefaultTransport。
	Transport http.RoundTripper
	// FlushInterval 为响应刷新间隔；负数表示立即刷新（SSE 流式透传）。
	FlushInterval time.Duration
	// Timeout 为单次上游调用的超时预算，0 表示不设置。
	Timeout time.Duration
	// TagHeader 为灰度标签请求头，空值时使用 X-Leego-Canary。
	TagHeader string
	// TimeoutHeader 为超时预算请求头，空值时使用 X-Request-Timeout。
	TimeoutHeader string
	// OnError 为上游失败回调，可为 nil。
	OnError func(ctx *leego.Context, target *url.URL, err error)
}

// NewDefaultOptions 返回推荐配置。
func NewDefaultOptions() Options {
	return Options{
		Transport:     http.DefaultTransport,
		TagHeader:     _DEFAULT_TAG_HEADER,
		TimeoutHeader: _DEFAULT_TIMEOUT_HEADER,
	}
}

// normalize 修正空字段。
func (own Options) normalize() Options {
	defaults := NewDefaultOptions()

	if own.Transport == nil {
		own.Transport = defaults.Transport
	}

	if strings.TrimSpace(own.TagHeader) == "" {
		own.TagHeader = defaults.TagHeader
	}

	if strings.TrimSpace(own.TimeoutHeader) == "" {
		own.TimeoutHeader = defaults.TimeoutHeader
	}

	if own.Timeout < 0 {
		own.Timeout = 0
	}

	return own
}

// Option 修改 Options。
type Option func(*Options)

// UseTransport 设置上游连接池。
func UseTransport(transport http.RoundTripper) Option {
	return func(options *Options) {
		options.Transport = transport
	}
}

// UseFlushInterval 设置响应刷新间隔；传入负数表示立即刷新。
func UseFlushInterval(interval time.Duration) Option {
	return func(options *Options) {
		options.FlushInterval = interval
	}
}

// UseTimeout 设置上游超时预算。
func UseTimeout(timeout time.Duration) Option {
	return func(options *Options) {
		options.Timeout = timeout
	}
}

// UseTagHeader 设置灰度标签请求头。
func UseTagHeader(name string) Option {
	return func(options *Options) {
		options.TagHeader = name
	}
}

// UseTimeoutHeader 设置超时预算请求头。
func UseTimeoutHeader(name string) Option {
	return func(options *Options) {
		options.TimeoutHeader = name
	}
}

// UseErrorHandler 设置上游失败回调。
func UseErrorHandler(handler func(ctx *leego.Context, target *url.URL, err error)) Option {
	return func(options *Options) {
		options.OnError = handler
	}
}

// errorKey 是请求上下文中的错误报告器键类型。
type errorKey struct{}

// errorReporter 在 ReverseProxy 的错误处理器里回调当前请求。
type errorReporter func(err error)

// proxy 保存反向代理的运行期状态。
type proxy struct {
	// balancer 为负载均衡器。
	balancer *Balancer
	// options 为规范化后的配置。
	options Options
	// proxies 按上游地址缓存 ReverseProxy，复用底层连接池。
	proxies map[string]*httputil.ReverseProxy
}

// NewProxy 创建反向代理处理器。
//
// 每个上游对应一个 ReverseProxy 实例，共享同一个 Transport；
// 灰度命中时只在同标签上游之间轮询，未命中则走全量加权轮询。
func NewProxy(balancer *Balancer, options ...Option) leego.HandleFunc {
	if balancer == nil || balancer.Len() == 0 {
		panic("gateway: balancer must contain at least one upstream")
	}

	settings := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	settings = settings.normalize()

	own := &proxy{
		balancer: balancer,
		options:  settings,
		proxies:  make(map[string]*httputil.ReverseProxy, balancer.Len()),
	}

	for _, upstream := range balancer.GetUpstreams() {
		own.proxies[upstream.URL.String()] = own.newReverseProxy(upstream.URL)
	}

	return own.serve
}

// newReverseProxy 为单个上游创建 ReverseProxy。
func (own *proxy) newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport:     own.options.Transport,
		FlushInterval: own.options.FlushInterval,
		Rewrite: func(request *httputil.ProxyRequest) {
			// SetURL 保留原始路径与查询串，只替换 scheme 与 host。
			request.SetURL(target)
			// SetXForwarded 补齐 X-Forwarded-For/Host/Proto。
			request.SetXForwarded()
		},
		ErrorHandler: func(resp http.ResponseWriter, request *http.Request, err error) {
			reporter, ok := request.Context().Value(errorKey{}).(errorReporter)
			if ok && reporter != nil {
				reporter(err)
				return
			}

			http.Error(resp, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		},
	}
}

// serve 选择上游并转发请求。
func (own *proxy) serve(ctx *leego.Context) {
	target := own.selectUpstream(ctx)
	if target == nil {
		ctx.ResponseError(http.StatusServiceUnavailable, "no upstream available")
		return
	}

	current := own.proxies[target.String()]
	if current == nil {
		ctx.ResponseError(http.StatusServiceUnavailable, "no upstream available")
		return
	}

	// 错误处理器需要访问当前请求上下文：通过请求上下文回传闭包。
	reporter := errorReporter(func(err error) {
		own.handleError(ctx, target, err)
	})
	request := ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), errorKey{}, reporter))

	cancel := func() {}
	if own.options.Timeout > 0 {
		var requestCtx context.Context
		requestCtx, cancel = context.WithTimeout(request.Context(), own.options.Timeout)
		request = request.WithContext(requestCtx)
		ctx.SetRequestHeader(own.options.TimeoutHeader, strconv.FormatInt(own.options.Timeout.Milliseconds(), 10))
	}

	ctx.Request = request
	current.ServeHTTP(ctx.Response, request)
	cancel()
}

// selectUpstream 先按灰度标签选择，未命中再按权重选择。
func (own *proxy) selectUpstream(ctx *leego.Context) *url.URL {
	if tag := strings.TrimSpace(ctx.GetRequestHeader(own.options.TagHeader)); tag != "" {
		if target := own.balancer.NextFor(tag); target != nil {
			return target
		}
	}

	return own.balancer.Next()
}

// handleError 输出统一的上游失败响应。
func (own *proxy) handleError(ctx *leego.Context, target *url.URL, err error) {
	if own.options.OnError != nil {
		own.options.OnError(ctx, target, err)
	}

	// 上游可能已经写入部分响应，此时只记录错误，不再追加响应体。
	if ctx.Response.GetStatusCode() != 0 {
		ctx.AddError(err)
		return
	}

	_ = ctx.ResponseJSON(http.StatusBadGateway, map[string]string{
		"error":  "upstream unavailable",
		"target": target.Redacted(),
	})
}
