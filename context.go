package leego

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/sse"
)

// JSON 定义一个简单的 JSON 类型，用于快速构造 JSON 响应体。
type JSON map[string]any

// ErrUnsupportedMediaType 表示无法根据 Content-Type 选择绑定方式。
//
// 该错误可通过 errors.Is 判断，便于调用方统一转换为 415 响应。
var ErrUnsupportedMediaType = errors.New("leego: context: unsupported content type")

// _BODY_CACHE_LIMIT 为上下文中保留请求体缓存的容量上限（64 KiB）。
//
// 超过上限的请求体在上下文回池时直接丢弃，避免对象池长期驻留
// 接近 MaxBodyBytes（默认 8 MiB）的大块内存。
const _BODY_CACHE_LIMIT = 64 << 10

// 编译期断言：*Context 必须实现 http.ResponseWriter 的常用辅助能力。
var _ = (*Context)(nil)

// Context 定义请求上下文。
//
// Context 是框架对单次 HTTP 请求的完整封装，包含原始请求、响应包装器、
// 路由参数、查询参数、错误链、客户端 IP、模板渲染等能力。
//
// 生命周期约定：
//   - Context 由 Engine 在请求进入时创建，在 handler 返回后回收；
//   - 业务代码不得在 handler 返回后继续持有 Context；
//   - 长连接（SSE/WebSocket）必须在 handler 内阻塞到连接结束，或使用
//     框架提供的独立连接对象，不得把 Context 传入后台协程。
type Context struct {
	// Request 保存原始 HTTP 请求。
	Request *http.Request
	// Response 保存响应包装器，提供状态码与字节数记录能力。
	Response response.Writer

	// url 缓存请求 URL，避免重复访问 Request.URL。
	url *url.URL
	// engine 指向创建该上下文的引擎；通过 NewContext 构造时可能为 nil。
	engine *Engine
	// clientIP 缓存解析后的客户端 IP。
	clientIP string
	// sameSite 保存 SetCookieData 使用的 SameSite 策略。
	sameSite http.SameSite
	// jwtClaims 保存认证中间件写入的 JWT 声明。
	jwtClaims any
	// startTime 保存上下文创建时间，用于统计请求耗时。
	startTime time.Time
	// errorChain 保存处理过程中收集到的错误。
	errorChain []error
	// pathValues 保存路由参数；静态路由命中时可能为 nil。
	pathValues map[string]string
	// queryValues 惰性解析的查询参数。
	queryValues url.Values
	// bodyCache 缓存已读取的请求体，保证多次读取结果一致。
	bodyCache []byte
	// bodyErr 缓存读取请求体时产生的错误。
	bodyErr error
	// bodyRead 标记请求体是否已经读取。
	bodyRead bool
	// bodyLimited 标记请求体是否已经套用 MaxBodyBytes 上限。
	// 表单解析与 ReadBody 共用同一层包装，避免重复包裹导致错误类型嵌套。
	bodyLimited bool
	// internal 标记该上下文是框架内部回源（如缓存后台刷新），
	// 可观测性中间件应跳过它，避免把内部请求计为真实流量。
	internal bool
	// pooled 标记当前上下文是否来自对象池；派生上下文不进入对象池。
	pooled bool
}

// newContext 创建框架内部使用的上下文。
//
// 参数 writer 必须是由 response.WriterPool 或 response.NewWriter 创建的
// 响应包装器；参数 req 不允许为 nil。
func newContext(engine *Engine, writer response.Writer, req *http.Request, pathValues map[string]string) *Context {
	ctx := &Context{
		Request:    req,
		Response:   writer,
		engine:     engine,
		startTime:  time.Now(),
		pathValues: pathValues,
	}

	if req != nil {
		ctx.url = req.URL
	}

	return ctx
}

// NewContext 创建一个独立的上下文。
//
// 该函数用于框架外部（例如标准库中间件适配器）构造 Context。与内部构造相比：
//   - engine 为 nil，与引擎相关的能力（模板、可信代理、请求体上限）使用默认值；
//   - startTime 会被正确初始化，避免耗时统计出现异常值；
//   - 该上下文不会进入对象池，调用方无需关心归还问题。
func NewContext(resp http.ResponseWriter, req *http.Request) *Context {
	return newContext(nil, response.NewWriter(resp), req, nil)
}

// reset 重置上下文以便对象池复用。
//
// 该函数会保留可复用的切片容量，但会清除所有请求级引用，防止内存泄漏。
func (own *Context) reset(engine *Engine, writer response.Writer, req *http.Request) {
	own.Request = req
	own.Response = writer
	own.engine = engine
	own.startTime = time.Now()
	own.url = nil
	own.clientIP = ""
	own.sameSite = http.SameSiteDefaultMode
	own.jwtClaims = nil
	own.errorChain = own.errorChain[:0]
	own.pathValues = nil
	own.queryValues = nil
	own.releaseBodyCache()
	own.bodyErr = nil
	own.bodyRead = false
	own.bodyLimited = false
	own.internal = false
	own.pooled = true

	if req != nil {
		own.url = req.URL
	}
}

// clear 清除上下文对请求级对象的引用。
//
// 对象池在回收上下文时调用该函数，避免池中长期持有 Request/Response。
func (own *Context) clear() {
	own.Request = nil
	own.Response = nil
	own.engine = nil
	own.url = nil
	own.clientIP = ""
	own.jwtClaims = nil
	own.errorChain = own.errorChain[:0]
	own.pathValues = nil
	own.queryValues = nil
	own.releaseBodyCache()
	own.bodyErr = nil
	own.bodyRead = false
	own.bodyLimited = false
	own.internal = false
	own.pooled = false
}

// releaseBodyCache 释放过大的请求体缓存，小缓存保留容量以便复用。
func (own *Context) releaseBodyCache() {
	if cap(own.bodyCache) > _BODY_CACHE_LIMIT {
		own.bodyCache = nil
		return
	}

	own.bodyCache = own.bodyCache[:0]
}

// contextPool 复用 Context 对象，降低高并发下的堆分配压力。
//
// 池中只保存已经调用 clear 的上下文，绝不保存请求级引用。
var contextPool = sync.Pool{
	New: func() any { return &Context{} },
}

// acquireContext 从对象池取出一个空上下文。
func acquireContext() *Context {
	return contextPool.Get().(*Context)
}

// releaseContext 清理并归还上下文。
func releaseContext(ctx *Context) {
	if ctx == nil {
		return
	}

	ctx.clear()
	contextPool.Put(ctx)
}

// GetStartTime 返回上下文创建时间。
func (own *Context) GetStartTime() time.Time {
	return own.startTime
}

// MarkInternal 把上下文标记为框架内部请求（例如缓存后台回源）。
//
// 日志、指标、追踪等可观测性中间件应跳过此类上下文，避免把内部
// 回源计为真实流量，或在访问日志中凭空多出一条“请求”。
func (own *Context) MarkInternal() {
	own.internal = true
}

// IsInternal 返回上下文是否为框架内部请求。
func (own *Context) IsInternal() bool {
	return own.internal
}

// GetEngine 返回创建该上下文的引擎；独立上下文中可能为 nil。
func (own *Context) GetEngine() *Engine {
	return own.engine
}

// GetServiceName 返回引擎名称；未绑定引擎时返回空字符串。
func (own *Context) GetServiceName() string {
	if own.engine == nil {
		return ""
	}

	return own.engine.getConfig().Name
}

// GetRemoteIP 返回直连对端 IP。
//
// 与 GetClientIP 不同，该函数不会读取任何代理头，适合在未配置可信代理时
// 作为安全的兜底值。解析失败时返回去除首尾空白后的 RemoteAddr。
func (own *Context) GetRemoteIP() string {
	if own.Request == nil {
		return ""
	}

	addr := strings.TrimSpace(own.Request.RemoteAddr)
	if addr == "" {
		return ""
	}

	if ip, _, err := net.SplitHostPort(addr); err == nil {
		return ip
	}

	return addr
}

// AddError 向错误链追加一个错误；nil 会被忽略。
func (own *Context) AddError(err error) {
	if err == nil {
		return
	}

	own.errorChain = append(own.errorChain, err)
}

// GetErrors 返回错误链。
//
// 返回值是内部切片的引用，调用方不得修改其内容。
func (own *Context) GetErrors() []error {
	return own.errorChain
}

// GetLastError 返回最后一个错误；没有错误时返回 nil。
func (own *Context) GetLastError() error {
	if len(own.errorChain) == 0 {
		return nil
	}

	return own.errorChain[len(own.errorChain)-1]
}

// Fail 记录错误并交给引擎的错误处理管道输出响应。
//
// 若引擎未配置 ErrorHandler，则使用默认的错误响应格式。该函数不会重复
// 写入已经发送过的响应，只记录错误并调用错误处理器。
func (own *Context) Fail(err error) {
	own.AddError(err)
	if own.engine != nil {
		if handler := own.engine.getErrorHandler(); handler != nil {
			handler(own, err)
			return
		}
	}

	WriteDefaultError(own, err)
}

// WriteDefaultError 使用默认格式输出错误响应。
//
// 默认格式为 application/json：
//
//	{"error":{"message":"..."}}
//
// 若响应已经写入，则只记录错误，不再改变响应内容。
func WriteDefaultError(ctx *Context, err error) {
	if ctx == nil || err == nil {
		return
	}

	var httpErr *HTTPError
	status := http.StatusInternalServerError
	// 默认文案不透出原始错误，避免把数据库地址、文件路径等内部细节返回给客户端；
	// 原始错误仍保留在错误链与日志中，便于服务端排障。
	message := http.StatusText(http.StatusInternalServerError)
	code := "internal_error"

	switch {
	case errors.As(err, &httpErr):
		status = httpErr.Status
		message = httpErr.Message
		if message == "" {
			message = http.StatusText(status)
		}
		if httpErr.Code != "" {
			code = httpErr.Code
		}
	case isBodyTooLarge(err):
		// 请求体超过 MaxBodyBytes 属于客户端错误，应按 413 返回而非 500。
		status = http.StatusRequestEntityTooLarge
		message = http.StatusText(status)
		code = "request_entity_too_large"
	}

	if ctx.GetStatusCode() != 0 {
		return
	}

	ctx.SetResponseHeader(headers.CONTENT_TYPE, "application/json; charset=utf-8")
	ctx.Response.WriteHeader(status)
	_ = json.NewEncoder(ctx.Response).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

// isBodyTooLarge 判断错误是否由请求体超过 MaxBodyBytes 引起。
func isBodyTooLarge(err error) bool {
	var magnitude *http.MaxBytesError
	return errors.As(err, &magnitude)
}

// HTTPError 定义可映射为 HTTP 响应的业务错误。
type HTTPError struct {
	// Status 为 HTTP 状态码，必须为合法状态码。
	Status int
	// Code 为业务错误码，用于客户端区分具体错误类型。
	Code string
	// Message 为面向用户展示的错误信息。
	Message string
	// Err 为底层错误，可通过 errors.Unwrap 获取。
	Err error
}

// Error 实现 error 接口。
func (own *HTTPError) Error() string {
	if own.Err != nil {
		return fmt.Sprintf("%s: %v", own.Message, own.Err)
	}

	return own.Message
}

// Unwrap 返回底层错误。
func (own *HTTPError) Unwrap() error {
	return own.Err
}

// NewHTTPError 创建一个 HTTP 业务错误。
func NewHTTPError(status int, code, message string, err error) *HTTPError {
	return &HTTPError{Status: status, Code: code, Message: message, Err: err}
}

/******************************************************** URL *********************************************************/

// GetURL 返回请求 URL。
func (own *Context) GetURL() *url.URL {
	return own.url
}

// GetPathValue 返回指定名称的路由参数；不存在时返回空字符串。
func (own *Context) GetPathValue(name string) string {
	if own.pathValues == nil {
		return ""
	}

	return own.pathValues[name]
}

// GetPathValues 返回全部路由参数。
//
// 静态路由命中时返回 nil，调用方应做 nil 判断。
func (own *Context) GetPathValues() map[string]string {
	return own.pathValues
}

// setPathValues 写入路由参数。
//
// 该方法仅供 Router 在匹配成功后调用，业务代码不应修改路由参数。
func (own *Context) setPathValues(values map[string]string) {
	own.pathValues = values
}

// GetQueryValue 返回指定名称的查询参数。
func (own *Context) GetQueryValue(key string) string {
	if own.queryValues == nil {
		own.queryValues = own.Request.URL.Query()
	}

	return own.queryValues.Get(key)
}

/******************************************************** JWT *********************************************************/

// SetJWTClaims 设置 JWT 声明。
func (own *Context) SetJWTClaims(claims any) {
	own.jwtClaims = claims
}

// GetJWTClaims 返回 JWT 声明及其是否存在。
func (own *Context) GetJWTClaims() (any, bool) {
	return own.jwtClaims, own.jwtClaims != nil
}

/****************************************************** Context *******************************************************/

// DeriveContext 返回一个携带指定标准库上下文的新 Context。
//
// 派生上下文与原上下文共享响应包装器，但拥有独立的参数表与错误链副本，
// 且不会进入对象池。该函数用于把请求上下文替换为带超时或取消能力的上下文。
func (own *Context) DeriveContext(ctx context.Context) *Context {
	if ctx == nil {
		ctx = context.Background()
	}

	errorChain := make([]error, len(own.errorChain))
	copy(errorChain, own.errorChain)

	var pathValues map[string]string
	if own.pathValues != nil {
		pathValues = make(map[string]string, len(own.pathValues))
		for key, value := range own.pathValues {
			pathValues[key] = value
		}
	}

	request := own.Request.WithContext(ctx)
	derived := &Context{
		Request:    request,
		Response:   own.Response,
		url:        request.URL,
		engine:     own.engine,
		clientIP:   own.clientIP,
		jwtClaims:  own.jwtClaims,
		sameSite:   own.sameSite,
		startTime:  own.startTime,
		internal:   own.internal,
		errorChain: errorChain,
		pathValues: pathValues,
		bodyCache:  own.bodyCache,
		bodyErr:    own.bodyErr,
		bodyRead:   own.bodyRead,
		pooled:     false,
	}

	return derived
}

// GetContextValue 返回标准库上下文中指定键对应的值。
func (own *Context) GetContextValue(key any) (any, bool) {
	ctx := own.Request.Context()
	if ctx == nil {
		return nil, false
	}

	if val := ctx.Value(key); val != nil {
		return val, true
	}

	return nil, false
}

/****************************************************** Header ********************************************************/

// SetRequestHeader 设置请求头。
//
// 当 value 为空字符串时删除该请求头，而非写入空值。
func (own *Context) SetRequestHeader(key, value string) {
	if value == "" {
		own.Request.Header.Del(key)
		return
	}

	own.Request.Header.Set(key, value)
}

// SetResponseHeader 设置响应头。
//
// 当 value 为空字符串时删除该响应头，而非写入空值。
func (own *Context) SetResponseHeader(key, value string) {
	if value == "" {
		own.Response.Header().Del(key)
		return
	}

	own.Response.Header().Set(key, value)
}

// DeleteRequestHeader 删除一个或多个请求头。
func (own *Context) DeleteRequestHeader(keys ...string) {
	for _, key := range keys {
		own.Request.Header.Del(key)
	}
}

// DeleteResponseHeader 删除一个或多个响应头。
func (own *Context) DeleteResponseHeader(keys ...string) {
	for _, key := range keys {
		own.Response.Header().Del(key)
	}
}

// GetClientIP 返回客户端 IP。
//
// 安全策略：
//   - 未配置可信代理时，只返回直连对端 IP，忽略任何代理头；
//   - 配置可信代理后，仅当直连对端本身属于可信网段时才解析代理头，
//     否则代理头可能由任意客户端伪造，仍只返回直连对端 IP；
//   - 解析时从右向左遍历 X-Forwarded-For（X-Real-IP 视作最右侧一跳），
//     跳过可信代理，返回第一个不可信地址；
//   - 若全部地址均可信，则回退为直连对端 IP。
func (own *Context) GetClientIP() string {
	if own.clientIP != "" {
		return own.clientIP
	}

	remote := own.GetRemoteIP()
	if own.engine == nil || len(own.engine.getTrustedProxies()) == 0 || !own.isTrustedProxy(remote) {
		// 直连对端不可信：代理头由客户端自行携带，采信即可被伪造 IP。
		own.clientIP = remote
		return own.clientIP
	}

	forwarded := own.Request.Header.Values("X-Forwarded-For")
	addresses := make([]string, 0, len(forwarded)+1)
	for _, header := range forwarded {
		for _, item := range strings.Split(header, ",") {
			if item = strings.TrimSpace(item); item != "" {
				addresses = append(addresses, item)
			}
		}
	}

	if realIP := strings.TrimSpace(own.Request.Header.Get("X-Real-IP")); realIP != "" {
		addresses = append(addresses, realIP)
	}

	// 从右向左查找第一个不可信地址。
	for index := len(addresses) - 1; index >= 0; index-- {
		candidate := normalizeIP(addresses[index])
		if candidate == "" {
			continue
		}

		if own.isTrustedProxy(candidate) {
			continue
		}

		own.clientIP = candidate
		return own.clientIP
	}

	own.clientIP = remote
	return own.clientIP
}

// isTrustedProxy 判断给定 IP 是否属于可信代理网段。
func (own *Context) isTrustedProxy(ip string) bool {
	if own.engine == nil {
		return false
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}

	for _, prefix := range own.engine.getTrustedProxies() {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

// normalizeIP 去除端口与 IPv6 方括号，返回可用于比较的 IP 文本。
func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}

	return strings.Trim(value, "[]")
}

// GetContentType 返回请求的 Content-Type。
func (own *Context) GetContentType() string {
	return headers.GetContentType(own.Request.Header)
}

// GetRequestHeader 返回指定请求头的值。
func (own *Context) GetRequestHeader(key string) string {
	return own.Request.Header.Get(key)
}

// GetResponseHeader 返回指定响应头的值。
func (own *Context) GetResponseHeader(key string) string {
	return own.Response.Header().Get(key)
}

// IsWebsocket 判断当前请求是否为 WebSocket 升级请求。
func (own *Context) IsWebsocket() bool {
	return strings.Contains(strings.ToLower(own.GetRequestHeader(headers.CONNECTION)), "upgrade") &&
		strings.EqualFold(own.GetRequestHeader(headers.UPGRADE), "websocket")
}

/*************************************************** Request Body *****************************************************/

// GetFormValue 返回表单字段值，同时覆盖 URL 查询参数与表单体。
func (own *Context) GetFormValue(key string) string {
	if own.Request.Form == nil {
		own.limitRequestBody()
		_ = own.Request.ParseForm()
	}

	return own.Request.Form.Get(key)
}

// GetPostFormValue 仅返回 x-www-form-urlencoded 请求体中的字段值。
func (own *Context) GetPostFormValue(key string) string {
	if own.Request.PostForm == nil {
		own.limitRequestBody()
		_ = own.Request.ParseForm()
	}

	return own.Request.PostForm.Get(key)
}

// ParseMultipartForm 解析 multipart/form-data 请求体。
//
// 解析前会先为请求体套上 MaxBodyBytes 上限，超限返回 http.MaxBytesError。
// 需要区分 "字段不存在" 与 "请求体超限" 的调用方应使用本方法，
// 而不是静默返回零值的 GetMultipartFormValue。
func (own *Context) ParseMultipartForm() error {
	if own.Request.MultipartForm != nil {
		return nil
	}

	own.limitRequestBody()
	return own.Request.ParseMultipartForm(own.getMultipartMemory())
}

// GetMultipartFormValue 返回 multipart/form-data 请求体中的字段值。
//
// 解析过程受引擎的 MaxBodyBytes 限制；超限时返回空字符串。需要区分
// "字段不存在" 与 "请求体超限" 的调用方应改用 ParseMultipartForm 或 BindForm。
func (own *Context) GetMultipartFormValue(key string) string {
	_ = own.ParseMultipartForm()

	if own.Request.MultipartForm == nil {
		return ""
	}

	if params := own.Request.MultipartForm.Value[key]; len(params) > 0 {
		return params[0]
	}

	return ""
}

// GetFormFile 返回上传的文件。
//
// 调用方负责关闭返回的 multipart.File。
func (own *Context) GetFormFile(filename string) (multipart.File, *multipart.FileHeader, error) {
	if err := own.ParseMultipartForm(); err != nil {
		return nil, nil, err
	}

	return own.Request.FormFile(filename)
}

// getMultipartMemory 返回解析 multipart 表单时允许使用的最大内存。
func (own *Context) getMultipartMemory() int64 {
	if own.engine != nil {
		return own.engine.getMultipartMemory()
	}

	return _DEFAULT_MULTIPART_MEMORY
}

// getMaxBodyBytes 返回请求体允许的最大字节数；返回 0 表示不限制。
func (own *Context) getMaxBodyBytes() int64 {
	if own.engine == nil {
		// NewContext 构造的独立上下文没有引擎配置，按框架默认值兜底，
		// 避免第三方适配器完全失去请求体上限保护。
		return _DEFAULT_MAX_BODY_BYTES
	}

	return own.engine.getConfig().MaxBodyBytes
}

// limitRequestBody 为请求体套上 MaxBodyBytes 上限。
//
// 该方法幂等：重复调用只会包装一次。表单解析（ParseForm /
// ParseMultipartForm）直接读取 Request.Body，因此必须在解析之前调用，
// 否则 MaxBodyBytes 形同虚设。
func (own *Context) limitRequestBody() {
	if own.bodyLimited || own.Request == nil || own.Request.Body == nil {
		return
	}

	own.bodyLimited = true

	limit := own.getMaxBodyBytes()
	if limit <= 0 {
		return
	}

	own.Request.Body = http.MaxBytesReader(own.Response, own.Request.Body, limit)
}

// SaveUploadedFile 将上传文件保存到指定路径。
//
// 安全说明：target 由业务方提供，框架不会自动限制其目录。若 target 来源于
// 用户输入，调用方必须先做白名单校验或使用 filepath.IsLocal 校验。
func (own *Context) SaveUploadedFile(file *multipart.FileHeader, target string, perm ...fs.FileMode) error {
	if file == nil {
		return errors.New("leego: context: nil multipart file header")
	}

	src, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	var mode os.FileMode = 0o750
	if len(perm) > 0 {
		mode = perm[0]
	}

	dir := filepath.Dir(target)
	if _, statErr := os.Stat(dir); statErr != nil {
		if !os.IsNotExist(statErr) {
			return statErr
		}

		if err = os.MkdirAll(dir, mode); err != nil {
			return err
		}

		// 仅收紧本次新建目录的权限，避免误改业务既有目录（如共享上传根）的权限。
		if err = os.Chmod(dir, mode); err != nil {
			return err
		}
	}

	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	_, err = io.Copy(dst, src)
	return err
}

// ReadBody 读取并缓存请求体。
//
// 语义保证：
//   - 不关闭 Request.Body，关闭责任仍由 net/http 承担；
//   - 多次调用返回同一份数据，便于日志、审计与绑定复用；
//   - 受引擎的 MaxBodyBytes 限制，超限返回 http.MaxBytesError。
func (own *Context) ReadBody() ([]byte, error) {
	if own.bodyRead {
		return own.bodyCache, own.bodyErr
	}

	own.bodyRead = true
	if own.Request == nil || own.Request.Body == nil {
		own.bodyCache = nil
		return nil, nil
	}

	own.limitRequestBody()

	data, err := io.ReadAll(own.Request.Body)
	own.bodyCache = data
	own.bodyErr = err
	return data, err
}

// GetRequestBody 返回请求体数据，等同于 ReadBody。
//
// 该名称保留用于兼容旧代码；新代码建议直接调用 ReadBody。
func (own *Context) GetRequestBody() ([]byte, error) {
	return own.ReadBody()
}

// Bind 根据 Content-Type 自动选择绑定方式。
//
// 支持 application/json、application/xml、text/xml、application/x-www-form-urlencoded
// 与 multipart/form-data。无法识别的类型返回 ErrUnsupportedMediaType。
func (own *Context) Bind(object any) error {
	contentType := strings.ToLower(own.GetContentType())
	switch {
	case strings.Contains(contentType, "json"):
		return own.BindJSON(object)
	case strings.Contains(contentType, "xml"):
		return own.BindXML(object)
	case strings.Contains(contentType, "multipart/form-data"):
		return own.BindForm(object)
	case strings.Contains(contentType, "x-www-form-urlencoded"):
		return own.BindForm(object)
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedMediaType, contentType)
	}
}

// BindJSON 将 JSON 请求体绑定到指定对象。
//
// 参数 object 必须为非 nil 指针。函数不会关闭请求体，并且复用 Body 的缓存，
// 因此可以在绑定后继续读取原始请求体。
func (own *Context) BindJSON(object any) error {
	if err := requireNonNilPointer(object); err != nil {
		return err
	}

	data, err := own.ReadBody()
	if err != nil {
		return err
	}

	// 直接用字节切片构造解码器：转成 string 会额外拷贝一份请求体，
	// 对接近 MaxBodyBytes 的大请求相当于双倍内存峰值。
	decoder := json.NewDecoder(bytes.NewReader(data))
	if own.engine != nil {
		if config := own.engine.getConfig(); config.StrictJSON {
			decoder.DisallowUnknownFields()
		}
	}

	if err := decoder.Decode(object); err != nil {
		return err
	}

	return validateBound(object)
}

// BindXML 将 XML 请求体绑定到指定对象。
//
// 参数 object 必须为非 nil 指针。函数不会关闭请求体。
func (own *Context) BindXML(object any) error {
	if err := requireNonNilPointer(object); err != nil {
		return err
	}

	data, err := own.ReadBody()
	if err != nil {
		return err
	}

	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(object); err != nil {
		return err
	}

	return validateBound(object)
}

// BindForm 将表单请求体绑定到指定对象。
//
// 支持 string、bool、整数、浮点数及其切片类型；字段通过 `form` 标签匹配，
// 未声明标签时使用字段名。
func (own *Context) BindForm(object any) error {
	if err := requireNonNilPointer(object); err != nil {
		return err
	}

	// 必须在 ParseForm 之前套上限：x-www-form-urlencoded 的解析同样读取
	// Request.Body，放到后面会让 MaxBodyBytes 对表单体失效。
	own.limitRequestBody()

	if err := own.Request.ParseForm(); err != nil {
		return err
	}

	if strings.Contains(strings.ToLower(own.GetContentType()), "multipart/form-data") {
		if err := own.ParseMultipartForm(); err != nil {
			return err
		}
	}

	if err := bindFormValues(own.Request.Form, object); err != nil {
		return err
	}

	return validateBound(object)
}

// requireNonNilPointer 校验绑定目标必须为非 nil 指针。
func requireNonNilPointer(object any) error {
	if object == nil {
		return errors.New("leego: context: bind target must not be nil")
	}

	value := reflect.ValueOf(object)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("leego: context: bind target must be a non-nil pointer")
	}

	return nil
}

// bindFormValues 将 url.Values 绑定到结构体指针。
func bindFormValues(values url.Values, object any) error {
	value := reflect.ValueOf(object).Elem()
	typ := value.Type()

	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if !field.CanSet() {
			continue
		}

		structField := typ.Field(index)
		if structField.PkgPath != "" {
			continue
		}

		tag := structField.Tag.Get("form")
		if tag == "-" {
			continue
		}

		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = structField.Name
		}

		rawValues, ok := values[name]
		if !ok || len(rawValues) == 0 {
			continue
		}

		if err := setFormField(field, rawValues); err != nil {
			return fmt.Errorf("leego: context: bind field %s: %w", structField.Name, err)
		}
	}

	return nil
}

// setFormField 将字符串切片写入受支持的字段类型。
func setFormField(field reflect.Value, values []string) error {
	raw := values[0]

	if field.Kind() == reflect.Slice {
		slice := reflect.MakeSlice(field.Type(), 0, len(values))
		for _, item := range values {
			element := reflect.New(field.Type().Elem()).Elem()
			if err := setScalarField(element, item); err != nil {
				return err
			}
			slice = reflect.Append(slice, element)
		}

		field.Set(slice)
		return nil
	}

	return setScalarField(field, raw)
}

// setScalarField 将单个字符串写入标量字段。
func setScalarField(field reflect.Value, raw string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(value)
	default:
		return fmt.Errorf("unsupported field type %s", field.Type())
	}

	return nil
}

/****************************************************** Cookie ********************************************************/

// SetCookie 设置响应 Cookie。
func (own *Context) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}

	if cookie.Path == "" {
		cookie.Path = "/"
	}

	if cookie.SameSite == http.SameSiteDefaultMode {
		cookie.SameSite = own.sameSite
	}

	http.SetCookie(own.Response, cookie)
}

// SetCookieData 设置响应 Cookie，并对值执行 URL 路径段编码。
//
// 这里必须与 GetCookieValue 的 url.PathUnescape 对称：
// url.QueryEscape 会把空格编码成 “+”，而路径解码不会把 “+” 还原成空格，
// 值中的加号也会因此产生歧义。
func (own *Context) SetCookieData(name, value string, maxAge int, cookiePath, domain string, secure, httpOnly bool) {
	if cookiePath == "" {
		cookiePath = "/"
	}

	http.SetCookie(own.Response, &http.Cookie{
		Name:     name,
		Value:    url.PathEscape(value),
		MaxAge:   maxAge,
		Path:     cookiePath,
		Domain:   domain,
		SameSite: own.sameSite,
		Secure:   secure,
		HttpOnly: httpOnly,
	})
}

// SetSameSite 设置后续 Cookie 使用的 SameSite 策略。
func (own *Context) SetSameSite(sameSite http.SameSite) {
	own.sameSite = sameSite
}

// GetCookieValue 返回指定 Cookie 的值。
//
// Cookie 值按百分号编码解码；与 url.QueryUnescape 不同，
// 这里的加号是字面量而不是空格，否则值中含 “+” 的 Cookie 会被静默篡改。
func (own *Context) GetCookieValue(name string) (string, error) {
	cookie, err := own.Request.Cookie(name)
	if err != nil {
		return "", err
	}

	value, err := url.PathUnescape(cookie.Value)
	if err != nil {
		// 非法转义时退回原始值：Cookie 可能由其它系统写入，
		// 不应因为解码失败就变成空串。
		return cookie.Value, nil
	}

	return value, nil
}

/**************************************************** Status Code *****************************************************/

// SetStatusCode 写入响应状态码。
func (own *Context) SetStatusCode(code int) {
	own.Response.WriteHeader(code)
}

// GetStatusCode 返回已写入的响应状态码；尚未写入时返回 0。
func (own *Context) GetStatusCode() int {
	return own.Response.GetStatusCode()
}

/***************************************************** Response *******************************************************/

// ResponseError 以纯文本形式输出错误响应，并记录错误链。
//
// 该格式与 http.Error 保持一致（text/plain; charset=utf-8，末尾换行），
// 适用于 404/405/423 等框架级错误。若响应已经开始写入，则只记录错误，
// 不再追加响应体，避免出现 “superfluous WriteHeader” 与响应体拼接错乱。
func (own *Context) ResponseError(code int, message string) {
	if message == "" {
		message = http.StatusText(code)
	}

	own.AddError(errors.New(message))
	if own.GetStatusCode() != 0 {
		return
	}

	http.Error(own.Response, message, code)
}

// ResponseXML 输出 XML 响应。
//
// Content-Type 固定为 application/xml; charset=utf-8。编码失败时返回错误，
// 但此时状态码与响应头已经写出，调用方不应再次写入响应。
func (own *Context) ResponseXML(code int, data any) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "application/xml; charset=utf-8")
	own.Response.WriteHeader(code)
	return xml.NewEncoder(own.Response).Encode(data)
}

// ResponseJSON 输出 JSON 响应。
//
// Content-Type 固定为 application/json; charset=utf-8。编码失败时返回错误，
// 但此时状态码与响应头已经写出，调用方不应再次写入响应。
func (own *Context) ResponseJSON(code int, data any) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "application/json; charset=utf-8")
	own.Response.WriteHeader(code)
	return json.NewEncoder(own.Response).Encode(data)
}

// ResponseRedirect 输出重定向响应。
//
// 仅接受 3xx 状态码，其他状态码会触发 panic，避免误用导致响应语义混乱。
// location 会原样写入 Location 响应头，调用方需自行完成必要的 URL 编码。
func (own *Context) ResponseRedirect(code int, location string) {
	if code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect {
		panic(fmt.Sprintf("leego: context: cannot redirect with status code %d", code))
	}

	http.Redirect(own.Response, own.Request, location, code)
}

// ResponseTemplate 使用引擎预加载的模板渲染 HTML。
//
// 模板需通过 Engine.LoadHTMLGlob 或 Engine.LoadHTMLFiles 预先加载；
// 未绑定引擎或模板不存在时返回错误。模板执行失败时可能已经写入部分
// 响应内容，此时调用方只能记录错误，不能再改写状态码。
func (own *Context) ResponseTemplate(templateName string, data any) error {
	if own.engine == nil {
		return errors.New("leego: context: template rendering requires an engine")
	}

	tmpl := own.engine.getTemplate()
	if tmpl == nil {
		return errors.New("leego: context: no template has been loaded")
	}

	return own.executeTemplate(tmpl, templateName, data)
}

// ResponseTemplateFile 直接解析指定文件并渲染 HTML。
//
// 模板名由 filepath.Base(filename) 推导，与 html/template 对 ParseFiles 的
// 命名规则一致；该函数不缓存模板，适合配置文件或调试页面，高频场景建议
// 使用 ResponseTemplate。
func (own *Context) ResponseTemplateFile(filename, templateName string, data any) error {
	var funcs template.FuncMap
	if own.engine != nil {
		funcs = own.engine.getTemplateFuncs()
	}

	tmpl, err := template.New(filepath.Base(filename)).Funcs(funcs).ParseFiles(filename)
	if err != nil {
		return fmt.Errorf("leego: context: failed to parse template: %w", err)
	}

	return own.executeTemplate(tmpl, templateName, data)
}

// executeTemplate 执行模板并写入响应。
func (own *Context) executeTemplate(tmpl *template.Template, templateName string, data any) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "text/html; charset=utf-8")
	if templateName == "" {
		return tmpl.Execute(own.Response, data)
	}

	return tmpl.ExecuteTemplate(own.Response, templateName, data)
}

// ResponseHTML 直接输出一段 HTML 字符串。
//
// Content-Type 固定为 text/html; charset=utf-8。本方法**不做任何转义**：
// 内容会被原样写入响应体，参数含用户输入时存在 XSS 风险，
// 必须改用 ResponseHTMLTemplate（内联模板）或 ResponseTemplate（预加载模板）。
func (own *Context) ResponseHTML(code int, html string) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "text/html; charset=utf-8")
	own.Response.WriteHeader(code)
	_, err := own.Response.Write([]byte(html))
	return err
}

// ResponseHTMLTemplate 用内联模板字符串渲染 HTML。
//
// 模板经 html/template 自动转义后输出，参数含用户输入时优先使用本方法。
// 每次调用都会重新解析模板，高频场景请改用 Engine.LoadHTMLGlob 配合
// ResponseTemplate。渲染失败时可能已经写入部分内容，调用方只能记录错误。
func (own *Context) ResponseHTMLTemplate(source string, data any) error {
	var funcs template.FuncMap
	if own.engine != nil {
		funcs = own.engine.getTemplateFuncs()
	}

	tmpl, err := template.New("inline").Funcs(funcs).Parse(source)
	if err != nil {
		return fmt.Errorf("leego: context: failed to parse inline template: %w", err)
	}

	return own.executeTemplate(tmpl, "", data)
}

// ResponseText 输出纯文本响应。
//
// Content-Type 固定为 text/plain; charset=utf-8。与 ResponseError 的区别是：
// 本方法不记录错误链、不追加换行，适用于正常的文本型业务响应。
func (own *Context) ResponseText(code int, data string) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "text/plain; charset=utf-8")
	own.Response.WriteHeader(code)
	_, err := own.Response.Write([]byte(data))
	return err
}

// ResponseBytes 输出原始字节响应。
//
// 本方法不设置 Content-Type，由调用方按内容类型自行写入响应头，
// 适用于二进制流、图片或已序列化好的负载。
func (own *Context) ResponseBytes(code int, data []byte) error {
	own.Response.WriteHeader(code)
	_, err := own.Response.Write(data)
	return err
}

// ResponseSSE 建立 SSE 连接并持续推送事件。
//
// 该函数会阻塞到连接结束（客户端断开、事件通道关闭或服务器关闭），
// 通道关闭后向客户端补发一条 close 事件。handler 必须等待其返回，
// 不能在后台协程中继续使用 Context。
func (own *Context) ResponseSSE(event func(ctx context.Context) <-chan *sse.Message) error {
	return sse.ServeEventStream(own.Response, own.Request, event, 8)
}

// ResponseFile 将文件内容直接输出到页面（inline）。
//
// 底层调用 http.ServeFile，自动处理 Range、If-Modified-Since 与 Last-Modified；
// 若希望浏览器弹出下载框，请使用 ResponseAttachment。
func (own *Context) ResponseFile(filename string) {
	http.ServeFile(own.Response, own.Request, filename)
}

// ResponseFileFromFS 从指定文件系统输出文件内容（inline）。
//
// 实现上会临时把 Request.URL.Path 改写为 filename 并在 defer 中恢复，
// 因此同一个 Context 不可被并发复用；文件系统实现需自行完成路径穿越防护。
func (own *Context) ResponseFileFromFS(filename string, fs http.FileSystem) {
	original := own.Request.URL.Path
	defer func() { own.Request.URL.Path = original }()

	own.Request.URL.Path = filename
	http.FileServer(fs).ServeHTTP(own.Response, own.Request)
}

// ResponseAttachment 以附件形式下载文件。
//
// 除调用 http.ServeFile 外，还会写入 Content-Disposition: attachment：
// 纯 ASCII 文件名使用 filename="..." 形式，非 ASCII 文件名使用 RFC 5987
// 的 filename*=UTF-8”... 形式，避免浏览器把中文文件名解析为乱码。
func (own *Context) ResponseAttachment(filename string) {
	own.setAttachmentHeader(filename)
	http.ServeFile(own.Response, own.Request, filename)
}

// ResponseAttachmentFromFS 从指定文件系统以附件形式下载文件。
//
// 与 ResponseFileFromFS 一样会临时改写 Request.URL.Path 并在 defer 中恢复；
// 附件头的编码规则见 ResponseAttachment。
func (own *Context) ResponseAttachmentFromFS(filename string, fs http.FileSystem) {
	original := own.Request.URL.Path
	defer func() { own.Request.URL.Path = original }()

	own.setAttachmentHeader(filename)
	own.Request.URL.Path = filename
	http.FileServer(fs).ServeHTTP(own.Response, own.Request)
}

// setAttachmentHeader 设置 Content-Disposition 响应头。
//
// 文件名使用 filepath.Base 截取，兼容 Windows 反斜杠与 POSIX 斜杠；
// 直接使用 path.Base 在 Windows 上会把整个路径当成文件名。
func (own *Context) setAttachmentHeader(filename string) {
	name := filepath.Base(filename)
	if isASCII(name) {
		own.Response.Header().Set(headers.CONTENT_DISPOSITION, `attachment; filename="`+escapeQuotes(name)+`"`)
		return
	}

	own.Response.Header().Set(headers.CONTENT_DISPOSITION, `attachment; filename*=UTF-8''`+url.QueryEscape(name))
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// escapeQuotes 转义文件名中的双引号与反斜杠。
func escapeQuotes(value string) string {
	return quoteEscaper.Replace(value)
}

// isASCII 判断字符串是否全部由 ASCII 字符组成。
func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > unicode.MaxASCII {
			return false
		}
	}

	return true
}
