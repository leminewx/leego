package leego

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/sse"
)

// JSON 定义一个简单的 JSON 类型
type JSON map[string]any

// Context 定义上下文的结构
type Context struct {
	Request  *http.Request   // 原始请求
	Response response.Writer // 原始响应

	url         *url.URL          // URL 消息
	engine      *Engine           // 绑定引擎
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
	addr := strings.TrimSpace(own.Request.RemoteAddr)
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}

	return ip
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

// SetJWTClaims 设置 JWT 声明信息
func (own *Context) SetJWTClaims(claims map[string]any) {
	own.tokenValues = claims
}

// GetJWTClaim 获取 JWT 声明参数
func (own *Context) GetJWTClaim(key string) (any, bool) {
	if own.tokenValues == nil {
		return nil, false
	}

	value, ok := own.tokenValues[key]
	return value, ok
}

/****************************************************** Context *******************************************************/

// WithContext 返回一个携带指定上下文的请求
func (own *Context) WithContext(ctx context.Context) *Context {
	errorChain := make([]error, 0, len(own.errorChain))
	for _, err := range own.errorChain {
		errorChain = append(errorChain, err)
	}

	pathValues := make(map[string]string)
	for key, value := range own.pathValues {
		pathValues[key] = value
	}

	tokenValues := make(map[string]any)
	for key, value := range own.tokenValues {
		tokenValues[key] = value
	}

	request := own.Request.WithContext(ctx)
	return &Context{
		url:         request.URL,
		engine:      own.engine,
		Request:     request,
		Response:    own.Response,
		clientIP:    own.clientIP,
		sameSite:    own.sameSite,
		startTime:   own.startTime,
		errorChain:  errorChain,
		pathValues:  pathValues,
		tokenValues: tokenValues,
	}
}

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

/****************************************************** Header ********************************************************/

// SetRequestHeader 设置请求头
func (own *Context) SetRequestHeader(key, value string) {
	if value == "" {
		own.Request.Header.Del(key)
	}

	own.Request.Header.Set(key, value)
}

// SetResponseHeader 设置响应头
func (own *Context) SetResponseHeader(key, value string) {
	if value == "" {
		own.Response.Header().Del(key)
	}

	own.Response.Header().Set(key, value)
}

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

// GetRequestHeader 获取请求头字段信息
func (own *Context) GetRequestHeader(key string) string {
	return own.Request.Header.Get(key)
}

// GetResponseHeader 获取响应头字段信息
func (own *Context) GetResponseHeader(key string) string {
	return own.Response.Header().Get(key)
}

// IsWebsocket 判断是否为 WebSocket 请求
func (own *Context) IsWebsocket() bool {
	return strings.Contains(strings.ToLower(own.GetRequestHeader(headers.CONNECTION)), "upgrade") &&
		strings.EqualFold(own.GetRequestHeader(headers.UPGRADE), "websocket")
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

// SetCookie 设置 cookie
func (own *Context) SetCookie(cookie *http.Cookie) {
	if cookie.Path == "" {
		cookie.Path = "/"
	}

	if cookie.SameSite == http.SameSiteDefaultMode {
		cookie.SameSite = own.sameSite
	}

	http.SetCookie(own.Response, cookie)
}

// SetCookieData 设置 cookie
func (own *Context) SetCookieData(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
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

// SetSameSite 设置 cookie
func (own *Context) SetSameSite(sameSite http.SameSite) {
	own.sameSite = sameSite
}

// GetCookieValue 获取 cookie 值
func (own *Context) GetCookieValue(name string) (string, error) {
	cookie, err := own.Request.Cookie(name)
	if err != nil {
		return "", err
	}

	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

/**************************************************** Status Code *****************************************************/

// SetStatusCode 设置响应状态码
func (own *Context) SetStatusCode(code int) {
	own.Response.WriteHeader(code)
}

// GetStatusCode 获取响应状态码
func (own *Context) GetStatusCode() int {
	return own.Response.GetStatusCode()
}

/***************************************************** Response *******************************************************/

// ResponseError 响应错误信息
func (own *Context) ResponseError(code int, error string) {
	http.Error(own.Response, error, code)
}

// ResponseXML 响应 XML 格式的数据
func (own *Context) ResponseXML(code int, data any) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "text/xml; charset=utf-8")
	own.Response.WriteHeader(code)
	return xml.NewEncoder(own.Response).Encode(data)
}

// ResponseJSON 响应 JSON 格式的数据
func (own *Context) ResponseJSON(code int, data any) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "application/json; charset=utf-8")
	own.Response.WriteHeader(code)
	return json.NewEncoder(own.Response).Encode(&data)
}

// ResponseRedirect 响应重定向
func (own *Context) ResponseRedirect(code int, location string) {
	if (code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect) && code != http.StatusCreated {
		panic(fmt.Sprintf("Cannot redirect with status code %d", code))
	}

	http.Redirect(own.Response, own.Request, location, code)
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

// ResponsePlain 响应纯文本内容
func (own *Context) ResponsePlain(code int, data string) error {
	own.SetResponseHeader(headers.CONTENT_TYPE, "text/plain; charset=utf-8")
	own.Response.WriteHeader(code)
	_, err := own.Response.Write([]byte(data))
	return err
}

// ResponseBytes 响应字节数据
func (own *Context) ResponseBytes(code int, data []byte) error {
	own.Response.WriteHeader(code)
	_, err := own.Response.Write(data)
	return err
}

// ResponseSSEvent 发起 SSE 事件响应
func (own *Context) ResponseSSEvent(event func(ctx context.Context) <-chan *sse.Message) error {
	return sse.Handle(own.Response, own.Request, event)
}

// ResponseFileContent 响应文件内容（页面显示）
func (own *Context) ResponseFileContent(filepath string) {
	http.ServeFile(own.Response, own.Request, filepath)
}

// ResponseFileContentFromFS 从指定的文件系统响应文件内容（页面显示）
func (own *Context) ResponseFileContentFromFS(filepath string, fs http.FileSystem) {
	defer func(old string) { own.Request.URL.Path = old }(own.Request.URL.Path)

	own.Request.URL.Path = filepath
	http.FileServer(fs).ServeHTTP(own.Response, own.Request)
}

// ResponseFileAttachment 响应文件附件（下载文件）
func (own *Context) ResponseFileAttachment(filepath string) {
	filename := path.Base(filepath)
	if isASCII(filename) {
		own.Response.Header().Set(headers.CONTENT_DISPOSITION, `attachment; filename="`+escapeQuotes(filename)+`"`)
	} else {
		own.Response.Header().Set(headers.CONTENT_DISPOSITION, `attachment; filename*=UTF-8''`+url.QueryEscape(filename))
	}

	http.ServeFile(own.Response, own.Request, filepath)
}

// ResponseFileAttachmentFromFS 从指定的文件系统响应文件附件（下载文件）
func (own *Context) ResponseFileAttachmentFromFS(filepath string, fs http.FileSystem) {
	defer func(old string) { own.Request.URL.Path = old }(own.Request.URL.Path)

	filename := path.Base(filepath)
	if isASCII(filename) {
		own.Response.Header().Set(headers.CONTENT_DISPOSITION, `attachment; filename="`+escapeQuotes(filename)+`"`)
	} else {
		own.Response.Header().Set(headers.CONTENT_DISPOSITION, `attachment; filename*=UTF-8''`+url.QueryEscape(filename))
	}

	own.Request.URL.Path = filepath
	http.FileServer(fs).ServeHTTP(own.Response, own.Request)
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}

	return true
}
