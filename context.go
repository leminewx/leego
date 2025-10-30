package leego

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/sse"
)

const _NOT_FOUND_QUERY_PARAMETER = "leego: context: not found query parameter '%s'"

var maxMultipartMemory int64 = 10 << 20 // 10M

// SetMaxMultipartMemory 设置解析 multipart/form-data 多部分表单时最大使用的内存
func SetMaxMultipartMemory(size int64) {
	atomic.StoreInt64(&maxMultipartMemory, size)
}

// JSON 定义一个简单的 JSON 类型
type JSON map[string]any

// Context 定义上下文的结构
type Context struct {
	time   time.Time         // 创建时间
	url    *url.URL          // URL 消息
	query  url.Values        // 查询参数
	values map[string]string // 路由参数
	engine *Engine           // 绑定引擎
	token  *jwt.Token        // JWT 令牌

	// 原始请求何响应
	Request  *http.Request   // 原始请求
	Response response.Writer // 原始响应
}

// newContext 新建一个上下文
func newContext(engine *Engine, resp http.ResponseWriter, req *http.Request, values map[string]string) *Context {
	return &Context{
		time:     time.Now(),
		url:      req.URL,
		query:    req.URL.Query(),
		values:   values,
		engine:   engine,
		Request:  req,
		Response: response.NewWriter(resp),
	}
}

/**************************************************** HTTP Request ****************************************************/

// GetURL 获取 URL
func (own *Context) GetURL() *url.URL {
	return own.url
}

// GetJWT 获取 JWT 令牌
func (own *Context) GetJWT() (*jwt.Token, bool) {
	if own.token != nil {
		return own.token, true
	}

	return nil, false
}

// GetPathValue 获取动态路由参数
func (own *Context) GetPathValue(name string) string {
	if val, ok := own.values[name]; ok {
		return val
	}

	return ""
}

// GetContextValue 获取上下文附带的键值对
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

// QueryValue 获取查询参数
func (own *Context) QueryValue(key string) string {
	return own.query.Get(key)
}

/*
QueryPageAndLimit 查询分页信息

入参：
  - page：页数参数名
  - limit：数量参数名

出参：
  - p：页数
  - l：数量
  - e：错误
*/
func (own *Context) QueryPageAndLimit(page, limit string) (p, l int64, e error) {
	if p, e = strconv.ParseInt(own.query.Get(page), 10, 64); e != nil {
		return 0, 0, fmt.Errorf(_NOT_FOUND_QUERY_PARAMETER, page)
	}

	if l, e = strconv.ParseInt(own.query.Get(limit), 10, 64); e != nil {
		return 0, 0, fmt.Errorf(_NOT_FOUND_QUERY_PARAMETER, limit)
	}

	return
}

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
		_ = own.Request.ParseMultipartForm(atomic.LoadInt64(&maxMultipartMemory))
	}

	return own.Request.FormFile(filename)
}

// GetMultipartFormValue 仅支持查询 multipart/form-data 格式的 body
func (own *Context) GetMultipartFormValue(key string) string {
	if own.Request.MultipartForm == nil {
		_ = own.Request.ParseMultipartForm(atomic.LoadInt64(&maxMultipartMemory))
	}

	if params := own.Request.MultipartForm.Value[key]; len(params) > 0 {
		return params[0]
	}

	return ""
}

// GetServiceName 获取服务名称
func (own *Context) GetServiceName() string {
	return own.engine.GetServiceName()
}

// GetRequestHeader 获取请求头字段信息
func (own *Context) GetRequestHeader(key string) string {
	return own.Request.Header.Get(key)
}

// WithJWT 绑定 JWT 令牌
func (own *Context) WithJWT(token *jwt.Token) *Context {
	own.token = token
	return own
}

// WithContext 设置请求上下文
func (own *Context) WithContext(ctx context.Context) *Context {
	own.Request = own.Request.WithContext(ctx)
	return own
}

// WithRequestHeader 设置请求头
func (own *Context) WithRequestHeader(key, value string) *Context {
	own.Request.Header.Set(key, value)
	return own
}

// BindXML 绑定 XML 格式的数据到对象
func (own *Context) BindXML(obj any) error {
	err := xml.NewDecoder(own.Request.Body).Decode(&obj)
	_ = own.Request.Body.Close()
	return err
}

// BindJSON 绑定 JSON 格式的数据到对象
func (own *Context) BindJSON(data any) error {
	err := json.NewDecoder(own.Request.Body).Decode(&data)
	_ = own.Request.Body.Close()
	return err
}

/*************************************************** HTTP Response ****************************************************/

// GetStartTime 获取上下文创建时间
func (own *Context) GetStartTime() time.Time {
	return own.time
}

// GetStatusCode 获取响应状态码
func (own *Context) GetStatusCode() int {
	return own.Response.GetStatusCode()
}

// GetResponseHeader 获取响应头字段信息
func (own *Context) GetResponseHeader(key string) string {
	return own.Response.Header().Get(key)
}

// WithStatusCode 设置响应状态码
func (own *Context) WithStatusCode(code int) *Context {
	own.Response.WriteHeader(code)
	return own
}

// WithResponseHeader 设置响应头
func (own *Context) WithResponseHeader(key, value string) *Context {
	own.Response.Header().Set(key, value)
	return own
}

// ResponseError 响应错误信息
func (own *Context) ResponseError(code int, error string) {
	http.Error(own.Response, error, code)
}

// ResponseSSE 发起 SSE 事件响应
func (own *Context) ResponseSSE(handle func(ctx context.Context) <-chan *sse.Message) error {
	return sse.ServeHTTP(own.Response, own.Request, handle)
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
