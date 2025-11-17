package leego

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/sse"
)

// JSON 定义一个简单的 JSON 类型
type JSON map[string]any

// Context 定义上下文的结构
type Context struct {
	url    *url.URL // URL 消息
	engine *Engine  // 绑定引擎

	// 原始请求何响应
	Request  *http.Request   // 原始请求
	Response response.Writer // 原始响应

	clientIP    string            // 客户端 IP
	sameSite    http.SameSite     // 允许服务器定义 cookie 属性，使浏览器无法将此cookie与跨站点请求一起发送
	startTime   time.Time         // 创建时间
	errorChain  []error           // 错误链
	pathValues  map[string]string // 路由参数
	tokenValues map[string]any    // JWT参数
	queryValues url.Values        // 查询参数，惰性加载
}

// newContext 新建一个上下文
func newContext(engine *Engine, resp http.ResponseWriter, req *http.Request, pathValues map[string]string) *Context {
	return &Context{
		url:        req.URL,
		engine:     engine,
		Request:    req,
		Response:   response.NewWriter(resp),
		startTime:  time.Now(),
		pathValues: pathValues,
	}
}

// AddError 添加错误信息
func (own *Context) AddError(err error) {
	if own.errorChain == nil {
		own.errorChain = make([]error, 0)
	}

	own.errorChain = append(own.errorChain, err)
}

// GetErrors 获取错误信息
func (own *Context) GetErrors() []error {
	return own.errorChain
}

// GetStartTime 获取上下文创建时间
func (own *Context) GetStartTime() time.Time {
	return own.startTime
}

// GetServiceName 获取服务名称
func (own *Context) GetServiceName() string {
	return own.engine.Name
}

// GetRemoteIP 获取请求端 IP
func (own *Context) GetRemoteIP() string {
	ip, _, err := net.SplitHostPort(strings.TrimSpace(own.Request.RemoteAddr))
	if err != nil {
		return ""
	}

	return ip
}

/******************************************************** URL *********************************************************/

// GetURL 获取 URL
func (own *Context) GetURL() *url.URL {
	return own.url
}

// GetPathValue 获取动态路由参数
func (own *Context) GetPathValue(name string) string {
	if val, ok := own.pathValues[name]; ok {
		return val
	}

	return ""
}

// GetQueryValue 获取查询参数
func (own *Context) GetQueryValue(key string) string {
	if own.queryValues == nil {
		own.queryValues = own.Request.URL.Query()
	}

	return own.queryValues.Get(key)
}

/******************************************************** JWT *********************************************************/

// GetJWTClaim 获取 JWT 声明参数
func (own *Context) GetJWTClaim(key string) (any, bool) {
	if own.tokenValues == nil {
		return nil, false
	}

	value, ok := own.tokenValues[key]
	return value, ok
}

// WithJWTClaims 绑定 JWT 声明信息
func (own *Context) WithJWTClaims(claims map[string]any) *Context {
	own.tokenValues = claims
	return own
}

/****************************************************** Context *******************************************************/

// GetContextValue 获取上下文附带的键值对参数
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

// WithContext 设置请求上下文
func (own *Context) WithContext(ctx context.Context) *Context {
	if ctx != nil {
		own.Request = own.Request.WithContext(ctx)
	}

	return own
}

/****************************************************** Header ********************************************************/

// GetClientIP 获取客户端 IP
func (own *Context) GetClientIP() string {
	if own.clientIP != "" {
		return own.clientIP
	}

	// 检查 X-Forwarded-For 头部
	if forwarded := own.Request.Header.Get(headers.X_FORWARDED_FOR); forwarded != "" {
		own.clientIP = strings.Split(forwarded, ",")[0]
		return own.clientIP
	}

	// 检查 X-Real-IP 头部
	if own.clientIP = own.Request.Header.Get(headers.X_REAL_IP); own.clientIP != "" {
		return own.clientIP
	}

	// 使用 RemoteAddr
	ip, _, err := net.SplitHostPort(own.Request.RemoteAddr)
	if err != nil {
		own.clientIP = own.Request.RemoteAddr
		return own.clientIP
	}

	own.clientIP = ip
	return ip
}

// GetContentType 获取内容的类型
func (own *Context) GetContentType() string {
	return headers.GetContentType(own.Request.Header)
}

// IsWebsocket returns true if the request headers indicate that a websocket
// handshake is being initiated by the client.
func (own *Context) IsWebsocket() bool {
	return strings.Contains(strings.ToLower(own.GetRequestHeader(headers.CONNECTION)), "upgrade") &&
		strings.EqualFold(own.GetRequestHeader(headers.UPGRADE), "websocket")
}

// GetRequestHeader 获取请求头字段信息
func (own *Context) GetRequestHeader(key string) string {
	return own.Request.Header.Get(key)
}

// GetResponseHeader 获取响应头字段信息
func (own *Context) GetResponseHeader(key string) string {
	return own.Response.Header().Get(key)
}

// WithRequestHeader 设置请求头
func (own *Context) WithRequestHeader(key, value string) *Context {
	if value == "" {
		own.Request.Header.Del(key)
		return own
	}

	own.Request.Header.Set(key, value)
	return own
}

// WithResponseHeader 设置响应头
func (own *Context) WithResponseHeader(key, value string) *Context {
	if value == "" {
		own.Response.Header().Del(key)
		return own
	}

	own.Response.Header().Set(key, value)
	return own
}

/*************************************************** Request Body *****************************************************/

// GetFormValue 获取表单信息, 可查询 URL 参数和 x-www-form-urlencoded 格式的 body
func (own *Context) GetFormValue(key string) string {
	if own.Request.Form == nil {
		_ = own.Request.ParseForm()
	}

	return own.Request.Form.Get(key)
}

// GetPostFormValue 仅支持查询 x-www-form-urlencoded 格式的 body
func (own *Context) GetPostFormValue(key string) string {
	if own.Request.PostForm == nil {
		_ = own.Request.ParseForm()
	}

	return own.Request.PostForm.Get(key)
}

// GetFormFile 获取上传的文件信息
func (own *Context) GetFormFile(filename string) (multipart.File, *multipart.FileHeader, error) {
	if own.Request.MultipartForm == nil {
		_ = own.Request.ParseMultipartForm(own.engine.MaxMultipartMemory)
	}

	return own.Request.FormFile(filename)
}

// SaveUploadedFile 将上传的表单文件保存到指定的路径
func (own *Context) SaveUploadedFile(file *multipart.FileHeader, target string, perm ...fs.FileMode) error {
	// 打开文件
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	// 获取目标文件权限
	var mode os.FileMode = 0o750
	if len(perm) > 0 {
		mode = perm[0]
	}

	// 创建目标目录
	dir := filepath.Dir(target)
	if err = os.MkdirAll(dir, mode); err != nil {
		return err
	}

	// 修改目录权限
	if err = os.Chmod(dir, mode); err != nil {
		return err
	}

	// 创建目标文件
	dst, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	// 将上传的文件写入目标文件
	_, err = io.Copy(dst, src)
	return err
}

// GetMultipartFormValue 仅支持查询 multipart/form-data 格式的 body
func (own *Context) GetMultipartFormValue(key string) string {
	if own.Request.MultipartForm == nil {
		_ = own.Request.ParseMultipartForm(own.engine.MaxMultipartMemory)
	}

	if params := own.Request.MultipartForm.Value[key]; len(params) > 0 {
		return params[0]
	}

	return ""
}

// BindXML 绑定 XML 格式的数据到指定对象
func (own *Context) BindXML(obj any) error {
	err := xml.NewDecoder(own.Request.Body).Decode(&obj)
	_ = own.Request.Body.Close()
	return err
}

// BindJSON 绑定 JSON 格式的数据到指定对象
func (own *Context) BindJSON(data any) error {
	err := json.NewDecoder(own.Request.Body).Decode(&data)
	_ = own.Request.Body.Close()
	return err
}

// GetRequestBody 获取请求体数据
func (own *Context) GetRequestBody() ([]byte, error) {
	res, err := io.ReadAll(own.Request.Body)
	_ = own.Request.Body.Close()
	return res, err
}

/****************************************************** Cookie ********************************************************/

// GetCookieValue 获取 cookie 值
func (own *Context) GetCookieValue(name string) (string, error) {
	cookie, err := own.Request.Cookie(name)
	if err != nil {
		return "", err
	}

	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

// SetSameSite 设置 cookie
func (own *Context) SetSameSite(sameSite http.SameSite) {
	own.sameSite = sameSite
}

// WithCookie 设置 cookie
func (own *Context) WithCookie(cookie *http.Cookie) *Context {
	if cookie.Path == "" {
		cookie.Path = "/"
	}

	if cookie.SameSite == http.SameSiteDefaultMode {
		cookie.SameSite = own.sameSite
	}

	http.SetCookie(own.Response, cookie)
	return own
}

// WithCookieData 设置 cookie
func (own *Context) WithCookieData(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	if path == "" {
		path = "/"
	}

	http.SetCookie(own.Response, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		SameSite: own.sameSite,
		Secure:   secure,
		HttpOnly: httpOnly,
	})
}

/**************************************************** Status Code *****************************************************/

// GetStatusCode 获取响应状态码
func (own *Context) GetStatusCode() int {
	return own.Response.GetStatusCode()
}

// WithStatusCode 设置响应状态码
func (own *Context) WithStatusCode(code int) *Context {
	own.Response.WriteHeader(code)
	return own
}

/***************************************************** Response *******************************************************/

// ResponseError 响应错误信息
func (own *Context) ResponseError(code int, error string) {
	http.Error(own.Response, error, code)
}

// ResponseSSEvent 发起 SSE 事件响应
func (own *Context) ResponseSSEvent(handle func(ctx context.Context) <-chan *sse.Message) error {
	return sse.Handle(own.Response, own.Request, handle)
}

// ResponseXML 响应 XML 格式的数据
func (own *Context) ResponseXML(code int, data any) error {
	own.WithResponseHeader(headers.CONTENT_TYPE, "text/xml; charset=utf-8")
	own.Response.WriteHeader(code)
	return xml.NewEncoder(own.Response).Encode(data)
}

// ResponseJSON 响应 JSON 格式的数据
func (own *Context) ResponseJSON(code int, data any) error {
	own.WithResponseHeader(headers.CONTENT_TYPE, "application/json; charset=utf-8")
	own.Response.WriteHeader(code)
	return json.NewEncoder(own.Response).Encode(&data)
}

// ResponseBytes 响应字节数据
func (own *Context) ResponseBytes(code int, data []byte) error {
	own.Response.WriteHeader(code)
	_, err := own.Response.Write(data)
	return err
}

// ResponseString 响应字符串
func (own *Context) ResponseString(code int, data string) error {
	own.WithResponseHeader(headers.CONTENT_TYPE, "text/plain")
	own.Response.WriteHeader(code)
	_, err := own.Response.Write([]byte(data))
	return err
}

// ResponseFile 响应文件（下载文件）
func (own *Context) ResponseFile(filename string) error {
	return own.responseFile(filename, "attachment")
}

// ResponseContent 响应文件（页面显示）
func (own *Context) ResponseContent(filename string) error {
	return own.responseFile(filename, "inline")
}

// responseFile 响应文件
func (own *Context) responseFile(filename, disposition string) error {
	// 打开文件
	file, err := os.Open(filename)
	if err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_500_INTERNAL_SERVER_ERROR)
		return err
	}
	defer func() { _ = file.Close() }()

	// 获取文件信息
	info, err := file.Stat()
	if err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_500_INTERNAL_SERVER_ERROR)
		return err
	}

	// 设置响应头
	own.WithResponseHeader(headers.CONTENT_DISPOSITION, fmt.Sprintf("%s; filename=\"%s\"", disposition, info.Name()))
	own.WithResponseHeader(headers.CONTENT_LENGTH, strconv.FormatInt(info.Size(), 10))

	// 设置 Content-Type
	if disposition == "inline" {
		own.WithResponseHeader(headers.CONTENT_TYPE, own.detectContentType(filename, file))
	}

	// 支持断点续传
	http.ServeContent(own.Response, own.Request, info.Name(), info.ModTime(), file)
	return nil
}

// detectContentType 检测文件内容类型
func (own *Context) detectContentType(filename string, file *os.File) string {
	// 优先使用文件扩展名检测
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
	}

	// 系统检测
	buffer := make([]byte, 512)
	if n, err := file.Read(buffer); err == nil {
		return http.DetectContentType(buffer[:n])
	}

	// 直接返回
	return "application/octet-stream"
}

// ResponseFileFromFS 将 http.FileSystem 中指定文件内容写入响应
func (own *Context) ResponseFileFromFS(filepath string, fs http.FileSystem) {
	defer func(old string) { own.Request.URL.Path = old }(own.Request.URL.Path)

	own.Request.URL.Path = filepath
	http.FileServer(fs).ServeHTTP(own.Response, own.Request)
}

var templates = &sync.Map{}

// ResponseHTML 响应 HTML 内容
func (own *Context) ResponseHTML(filename, tmplName string, data any) error {
	// 先尝试读锁
	tmpl1, ok := templates.Load(filename)
	if ok {
		return own.executeTemplate(tmpl1.(*template.Template), tmplName, data)
	}

	// 解析模板
	tmpl2, err := template.ParseFiles(filename)
	if err != nil {
		return fmt.Errorf("leego: context: failed to parse template: %w", err)
	}

	templates.Store(filename, tmpl2)
	return own.executeTemplate(tmpl2, tmplName, data)
}

func (own *Context) executeTemplate(tmpl *template.Template, tmplName string, data any) error {
	if tmplName == "" {
		return tmpl.Execute(own.Response, data)
	}

	return tmpl.ExecuteTemplate(own.Response, tmplName, data)
}
