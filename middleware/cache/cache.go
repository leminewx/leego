// Package cache 提供基于内存 LRU 的响应缓存中间件。
//
// 设计要点：
//   - 只缓存可安全共享的幂等响应：状态码 200、无 Set-Cookie、
//     Cache-Control 未声明 no-store/no-cache/private、非 SSE；
//   - 缓存键包含方法、路径、原始查询串与配置的 Vary 头，
//     避免"同一 URL 不同身份拿到同一份缓存"；
//   - 支持 stale-while-revalidate：新鲜期过后先返回旧值，
//     同时在后台用去重的方式刷新，保证客户端不为刷新付出延迟；
//   - 全程只使用标准库，容量按响应体字节数做 LRU 淘汰。
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
)

const (
	// _DEFAULT_TTL 为默认新鲜期。
	_DEFAULT_TTL = 30 * time.Second
	// _DEFAULT_MAX_BYTES 为默认缓存总容量（64 MiB）。
	_DEFAULT_MAX_BYTES = 64 << 20
	// _DEFAULT_MAX_BODY 为默认单条响应上限（1 MiB）。
	_DEFAULT_MAX_BODY = 1 << 20
	// _HEADER_X_CACHE 为缓存命中状态响应头。
	_HEADER_X_CACHE = "X-Cache"
	// _CACHE_HIT 表示命中新鲜缓存。
	_CACHE_HIT = "HIT"
	// _CACHE_STALE 表示命中陈旧缓存。
	_CACHE_STALE = "STALE"
	// _CACHE_MISS 表示未命中。
	_CACHE_MISS = "MISS"
	// _KEY_LENGTH 为缓存键保留的摘要字节数（128 位）。
	_KEY_LENGTH = 16
)

// keySeparator 用于分隔缓存键的各个组成部分，避免拼接歧义。
var keySeparator = []byte{0}

// Config 定义缓存行为。
type Config struct {
	// TTL 为新鲜期，到期后进入陈旧期；非正数使用默认值 30s。
	TTL time.Duration
	// MaxBytes 为缓存总容量（按响应体字节数计），非正数使用默认值 64 MiB。
	MaxBytes int64
	// MaxBody 为单条响应上限，0 表示使用默认值 1 MiB，负数表示不限制。
	MaxBody int64
	// Vary 为参与缓存键计算的请求头；为空时使用 Authorization 与 Accept-Encoding。
	Vary []string
	// Methods 为参与缓存的方法；为空时使用 GET 与 HEAD。
	Methods []string
	// Stale 为陈旧期长度，0 表示过期立即删除、不支持 stale-while-revalidate。
	Stale time.Duration
}

// NewDefaultConfig 返回推荐配置。
func NewDefaultConfig() Config {
	return Config{
		TTL:      _DEFAULT_TTL,
		MaxBytes: _DEFAULT_MAX_BYTES,
		MaxBody:  _DEFAULT_MAX_BODY,
		Vary:     []string{"Authorization", "Accept-Encoding"},
		Methods:  []string{http.MethodGet, http.MethodHead},
	}
}

// normalize 修正非法配置并填充默认值。
func (own Config) normalize() Config {
	defaults := NewDefaultConfig()

	if own.TTL <= 0 {
		own.TTL = defaults.TTL
	}

	if own.MaxBytes <= 0 {
		own.MaxBytes = defaults.MaxBytes
	}

	if own.MaxBody == 0 {
		own.MaxBody = defaults.MaxBody
	} else if own.MaxBody < 0 {
		// 负数表示不限制：包装器约定 0 为"不限制"。
		own.MaxBody = 0
	}

	if own.Stale < 0 {
		own.Stale = 0
	}

	if len(own.Vary) == 0 {
		own.Vary = defaults.Vary
	}

	if len(own.Methods) == 0 {
		own.Methods = defaults.Methods
	}

	return own
}

// middleware 保存缓存中间件的运行期状态。
type middleware struct {
	// options 为规范化后的配置。
	options Config
	// vary 为参与缓存键的请求头。
	vary []string
	// methods 为参与缓存的方法集合。
	methods map[string]struct{}
	// store 为缓存存储。
	store *store
}

// New 创建响应缓存中间件。
//
// 参数 config 可省略；多个配置时仅第一个生效，与其它中间件保持一致。
func New(config ...Config) leego.MiddlewareFunc {
	options := NewDefaultConfig()
	if len(config) > 0 {
		options = config[0].normalize()
	}

	own := &middleware{
		options: options,
		vary:    make([]string, 0, len(options.Vary)),
		methods: make(map[string]struct{}, len(options.Methods)),
		store:   newStore(options.TTL, options.Stale, options.MaxBytes),
	}

	for _, name := range options.Vary {
		if name = strings.TrimSpace(name); name != "" {
			own.vary = append(own.vary, name)
		}
	}

	for _, method := range options.Methods {
		if method = strings.ToUpper(strings.TrimSpace(method)); method != "" {
			own.methods[method] = struct{}{}
		}
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			own.handle(next, ctx)
		}
	}
}

// handle 执行缓存查找、回源或回放。
func (own *middleware) handle(next leego.HandleFunc, ctx *leego.Context) {
	request := ctx.Request
	if request == nil {
		next(ctx)
		return
	}

	if _, ok := own.methods[strings.ToUpper(request.Method)]; !ok {
		next(ctx)
		return
	}

	key := buildKey(request, own.vary)
	item, fresh, found := own.store.get(key, time.Now())
	if found {
		if fresh {
			writeEntry(ctx, item, _CACHE_HIT)
			return
		}

		// 陈旧期：先返回旧值，再用后台刷新取代它，保证客户端零等待。
		writeEntry(ctx, item, _CACHE_STALE)
		own.refresh(next, request, key)
		return
	}

	ctx.Response.Header().Set(_HEADER_X_CACHE, _CACHE_MISS)

	original := ctx.Response
	recorder := newRecorder(original, own.options.MaxBody)
	ctx.Response = recorder
	next(ctx)
	ctx.Response = original

	item = recorder.snapshot(key)
	if own.cacheable(recorder, item) {
		own.prepare(item)
		own.store.put(item)
	}
}

// cacheable 判断响应是否允许写入缓存。
func (own *middleware) cacheable(recorder *recorder, item *entry) bool {
	if recorder.hijacked || recorder.overflow {
		return false
	}

	if item.status != http.StatusOK {
		return false
	}

	// 带 Set-Cookie 的响应必然是"某一次会话专用"的，
	// 即使 Vary 命中也不应跨请求共享。
	if item.header.Get("Set-Cookie") != "" {
		return false
	}

	if strings.HasPrefix(strings.ToLower(item.header.Get("Content-Type")), "text/event-stream") {
		return false
	}

	if forbidsCache(item.header.Get("Cache-Control")) {
		return false
	}

	return true
}

// prepare 清理不应被回放的响应头。
func (own *middleware) prepare(item *entry) {
	item.header.Del(_HEADER_X_CACHE)

	// 逐跳首部描述的是"这一次连接"，回放会导致协议错误。
	for _, name := range hopByHopHeaders {
		item.header.Del(name)
	}
}

// refresh 触发一次去重的后台刷新。
func (own *middleware) refresh(next leego.HandleFunc, request *http.Request, key string) {
	if own.options.Stale <= 0 {
		return
	}

	if !own.store.beginRefresh(key) {
		return
	}

	go own.doRefresh(next, request, key)
}

// doRefresh 在后台重跑处理链并更新缓存。
//
// 刷新只有"成功替换"与"保持旧值"两种结果，任何 panic 都被吞掉：
// 缓存是加速手段，不能成为新的故障源。
func (own *middleware) doRefresh(next leego.HandleFunc, request *http.Request, key string) {
	defer func() {
		_ = recover()
		own.store.endRefresh(key)
	}()

	if _, fresh, found := own.store.get(key, time.Now()); found && fresh {
		// 已有其它请求完成了刷新，无需重复回源。
		return
	}

	// 刷新脱离原请求的生命周期：原请求结束后仍要继续执行。
	cloned := request.Clone(context.Background())
	cloned.Body = http.NoBody

	writer := newCaptureWriter(own.options.MaxBody)
	next(leego.NewContext(writer, cloned))

	item := &entry{
		key:    key,
		status: writer.status,
		header: writer.header.Clone(),
		body:   append([]byte(nil), writer.body.Bytes()...),
	}

	recorder := &recorder{overflow: writer.overflow}
	if own.cacheable(recorder, item) {
		own.prepare(item)
		own.store.put(item)
	}
}

// writeEntry 用缓存条目回写响应。
func writeEntry(ctx *leego.Context, item *entry, tag string) {
	header := ctx.Response.Header()
	for name, values := range item.header {
		if strings.EqualFold(name, _HEADER_X_CACHE) {
			continue
		}

		header[name] = append([]string(nil), values...)
	}

	header.Set(_HEADER_X_CACHE, tag)
	if header.Get("Content-Length") == "" {
		header.Set("Content-Length", strconv.Itoa(len(item.body)))
	}

	ctx.Response.WriteHeader(item.status)

	// HEAD 只需要响应头；写体会让部分客户端误判为协议错误。
	if ctx.Request != nil && ctx.Request.Method == http.MethodHead {
		return
	}

	if len(item.body) == 0 {
		return
	}

	_, _ = ctx.Response.Write(item.body)
}

// buildKey 计算缓存键。
//
// HEAD 与 GET 共享同一条缓存：两者语义完全一致，只差响应体是否回写，
// 这样 HEAD 探测不会白跑一次上游。
// 使用 SHA-256 截断到 128 位：既避免长键占用内存，
// 也避免把 Authorization 之类的敏感头以明文形式留在内存摘要里。
func buildKey(request *http.Request, vary []string) string {
	var builder strings.Builder
	builder.Grow(64 + 32*len(vary))

	method := request.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}

	builder.WriteString(method)
	builder.Write(keySeparator)

	path := request.URL.Path
	if path == "" {
		path = "/"
	}

	builder.WriteString(path)
	builder.Write(keySeparator)
	builder.WriteString(request.URL.RawQuery)

	for _, name := range vary {
		builder.Write(keySeparator)
		builder.WriteString(strings.ToLower(name))
		builder.WriteString(":")
		builder.WriteString(request.Header.Get(name))
	}

	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:_KEY_LENGTH])
}

// forbidsCache 判断 Cache-Control 是否禁止缓存。
func forbidsCache(value string) bool {
	if value == "" {
		return false
	}

	for _, directive := range strings.Split(value, ",") {
		directive = strings.ToLower(strings.TrimSpace(directive))
		if directive == "no-store" || directive == "no-cache" || directive == "private" {
			return true
		}
	}

	return false
}

// hopByHopHeaders 列出不应跨请求回放的逐跳首部。
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}
