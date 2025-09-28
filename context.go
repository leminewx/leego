package leego

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"text/template"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/websocket"
)

var maxMultipartMemory int64 = 10 << 20 // 10M

var (
	errNotFoundPage         = errors.New("leego: context: not found query parameter 'page'")
	errNotFoundLimit        = errors.New("leego: context: not found query parameter 'limit'")
	errNotSupportedFlusher  = errors.New("leego: context: http.ResponseWriter does not supported http.Flusher")
	errNotSupportedHijacker = errors.New("leego: context: http.ResponseWriter does not supported http.Hijacker")
	errFailToUpgrade        = errors.New("leego: context: failed to upgrade http connection to WebSocket")
	errUpgradeWithoutKey    = errors.New("leego: context: upgrade headers does not carry 'Sec-WebSocket-Key'")
)

// SetMaxMultipartMemory 设置解析 multipart/form-data 多部分表单时最大使用的内存
func SetMaxMultipartMemory(size int64) {
	atomic.StoreInt64(&maxMultipartMemory, size)
}

// Json 定义一个简单的 Json 类型
type Json map[string]any

// Context 定义上下文的结构
type Context struct {
	code     int                 // 状态码
	query    url.Values          // 查询参数
	values   map[string]string   // 路由参数
	engine   *Engine             // 绑定引擎
	request  *http.Request       // 原始请求
	response http.ResponseWriter // 原始响应
}

// newContext 新建一个上下文
func newContext(engine *Engine, resp http.ResponseWriter, req *http.Request, values map[string]string) *Context {
	return &Context{
		query:    req.URL.Query(),
		values:   values,
		engine:   engine,
		request:  req,
		response: resp,
	}
}

/**************************************************** HTTP Request ****************************************************/

// GetURL 获取 RUL 信息
func (own *Context) GetURL() *url.URL { return own.request.URL }

// GetHost 获取主机信息
func (own *Context) GetHost() string { return own.request.Host }

// GetMethod 获取请求方法
func (own *Context) GetMethod() string { return own.request.Method }

// GetPath 获取请求路径
func (own *Context) GetPath() string { return own.request.URL.Path }

// GetProtocol 获取请求协议
func (own *Context) GetProtocol() string { return own.request.Proto }

// GetPathValue 获取动态路由参数
func (own *Context) GetPathValue(name string) string {
	if val, ok := own.values[name]; ok {
		return val
	}

	return ""
}

// GetContextValue 获取上下文附带的键值对
func (own *Context) GetContextValue(key any) (any, bool) {
	ctx := own.request.Context()
	if ctx == nil {
		return nil, false
	}

	val := ctx.Value(key)
	if val == nil {
		return nil, false
	}

	return val, true
}

// QueryValue 获取查询参数
func (own *Context) QueryValue(key string) string { return own.query.Get(key) }

// QueryPageAndLimit 查询分页信息(仅支持 page 和 limit 字段)
func (own *Context) QueryPageAndLimit() (page, limit int, err error) {
	p, e := strconv.ParseInt(own.query.Get("page"), 10, 64)
	if e != nil {
		return 0, 0, errNotFoundPage
	}

	l, e := strconv.ParseInt(own.query.Get("limit"), 10, 64)
	if e != nil {
		return 0, 0, errNotFoundLimit
	}

	return int(p), int(l), nil
}

// GetFormValue 获取表单信息, 可查询 URL 参数和 x-www-form-urlencoded 格式的 body
func (own *Context) GetFormValue(key string) string {
	if own.request.Form == nil {
		_ = own.request.ParseForm()
	}

	return own.request.Form.Get(key)
}

// GetPostFormValue 仅支持查询 x-www-form-urlencoded 格式的 body
func (own *Context) GetPostFormValue(key string) string {
	if own.request.PostForm == nil {
		_ = own.request.ParseForm()
	}

	return own.request.PostForm.Get(key)
}

// GetFormFile 获取上传的文件信息
func (own *Context) GetFormFile(filename string) (multipart.File, *multipart.FileHeader, error) {
	if own.request.MultipartForm == nil {
		_ = own.request.ParseMultipartForm(maxMultipartMemory)
	}

	return own.request.FormFile(filename)
}

// GetMultipartFormValue 仅支持查询 multipart/form-data 格式的 body
func (own *Context) GetMultipartFormValue(key string) string {
	if own.request.MultipartForm == nil {
		_ = own.request.ParseMultipartForm(maxMultipartMemory)
	}

	if params := own.request.MultipartForm.Value[key]; len(params) > 0 {
		return params[0]
	}

	return ""
}

// GetServiceName 获取服务名称
func (own *Context) GetServiceName() string {
	return own.engine.GetServiceName()
}

// GetHeader 获取请求头字段信息
func (own *Context) GetHeader(key string) string {
	return own.request.Header.Get(key)
}

// GetRequest 获取原始请求
func (own *Context) GetRequest() *http.Request { return own.request }

// GetResponse 获取原始响应
func (own *Context) GetResponse() http.ResponseWriter { return own.response }

// BindXML 绑定 XML 格式的数据到对象
func (own *Context) BindXML(obj any) error {
	err := xml.NewDecoder(own.request.Body).Decode(&obj)
	_ = own.request.Body.Close()
	return err
}

// BindJSON 绑定 JSON 格式的数据到对象
func (own *Context) BindJSON(data any) error {
	err := json.NewDecoder(own.request.Body).Decode(&data)
	_ = own.request.Body.Close()
	return err
}

// WithContext 设置请求上下文
func (own *Context) WithContext(ctx context.Context) *Context {
	own.request = own.request.WithContext(ctx)
	return own
}

// WithRequestHeader 设置请求头
func (own *Context) WithRequestHeader(key, value string) *Context {
	own.request.Header.Set(key, value)
	return own
}

/*************************************************** HTTP Response ****************************************************/

// GetStatusCode 获取响应状态码
func (own *Context) GetStatusCode() int {
	return own.code
}

// WithResponseHeader 设置响应头
func (own *Context) WithResponseHeader(key, value string) *Context {
	own.response.Header().Set(key, value)
	return own
}

// WithStatusCode 设置响应状态码
func (own *Context) WithStatusCode(code int) *Context {
	own.code = code
	own.response.WriteHeader(code)
	return own
}

// ResponseError 响应错误信息
func (own *Context) ResponseError(code int, error string) {
	own.code = code
	http.Error(own.response, error, code)
}

// ResponseXML 响应 XML 格式的数据
func (own *Context) ResponseXML(code int, data any) error {
	own.WithStatusCode(code)
	return xml.NewEncoder(own.response).Encode(data)
}

// ResponseJSON 响应 JSON 格式的数据
func (own *Context) ResponseJSON(code int, data any) error {
	own.WithResponseHeader("Content-Type", "application/json")
	own.WithStatusCode(code)
	return json.NewEncoder(own.response).Encode(&data)
}

// ResponseBytes 响应字节数据
func (own *Context) ResponseBytes(code int, data []byte) error {
	own.WithStatusCode(code)
	_, err := own.response.Write(data)
	return err
}

// ResponseString 响应字符串
func (own *Context) ResponseString(code int, data string) error {
	own.WithStatusCode(code)
	_, err := own.response.Write([]byte(data))
	return err
}

// ResponseFile 响应文件（下载文件）
func (own *Context) ResponseFile(filename string) error {
	return own.responseFile(filename, "attachment")
}

// ResponseFileContent 响应文件（页面显示）
func (own *Context) ResponseFileContent(filename string) error {
	return own.responseFile(filename, "inline")
}

// responseFile 响应文件
func (own *Context) responseFile(filename, disposition string) error {
	// 打开文件
	file, err := os.Open(filename)
	if err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
		return err
	}
	defer func() { _ = file.Close() }()

	// 获取文件信息
	info, err := file.Stat()
	if err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
		return err
	}

	// 设置响应头
	own.WithResponseHeader("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"", disposition, info.Name()))
	own.WithResponseHeader("Content-Length", strconv.FormatInt(info.Size(), 10))

	// 设置 Content-Type
	if disposition == "inline" {
		own.WithResponseHeader("Content-Type", own.detectContentType(filename, file))
	}

	// 支持断点续传
	http.ServeContent(own.response, own.request, info.Name(), info.ModTime(), file)
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

var templates = make(map[string]*template.Template)

// ResponseHTML 响应 HTML 内容
func (own *Context) ResponseHTML(filename, tmplName string, data any) error {
	tmpl, ok := templates[filename]
	if !ok {
		tmpl = template.Must(template.ParseFiles(filename))
		templates[filename] = tmpl
	}

	if tmplName == "" {
		return tmpl.Execute(own.response, &data)
	}

	return tmpl.ExecuteTemplate(own.response, tmplName, &data)
}

// ResponseSSE 发起 SSE 响应
func (own *Context) ResponseSSE(sse func() <-chan []byte) (err error) {
	// 检查响应是否是 http.Flusher
	flusher, ok := own.response.(http.Flusher)
	if !ok {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
		return errNotSupportedFlusher
	}

	// 设置响应头
	own.WithResponseHeader("Content-Type", "text/event-stream")
	own.WithResponseHeader("Cache-Control", "no-cache")
	own.WithResponseHeader("Connection", "keep-alive")

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(own.request.Context())
	defer cancel()

	// 监听客户端断开连接
	go func() {
		<-ctx.Done()
		cancel()
	}()

	// 调用 SSE 处理函数
	events := sse()

	var buffer bytes.Buffer
	for {
		select {
		// 正常接收数据
		case data, ok := <-events:
			// 通道关闭
			if !ok {
				buffer.WriteString("data: [CLOSED]\n\n")
				_, err = own.response.Write(buffer.Bytes())
				flusher.Flush()
				return err
			}

			// 重置 buffer，并写入数据到 buffer
			buffer.Reset()
			buffer.WriteString("data: ")
			buffer.Write(data)
			buffer.WriteString("\n\n")

			// 将 buffer 中的数据写入响应
			if _, err := own.response.Write(buffer.Bytes()); err != nil {
				return err
			}
			flusher.Flush()

		// 客户端断开连接
		case <-ctx.Done():
			return nil
		}
	}
}

// ResponseUpgrade 发起 Websocket 响应
func (own *Context) ResponseUpgrade(upgrade func(conn *websocket.Conn)) error {
	// 校验请求头
	header := own.request.Header
	if !checkHeaderValues(header.Get("Connection"), "upgrade") || !strings.EqualFold(header.Get("Upgrade"), "websocket") {
		own.ResponseError(http.StatusBadRequest, response.STATUS_BAD_REQUEST)
		return errFailToUpgrade
	}

	// 必须包含有效的 Sec-WebSocket-Key
	if own.GetHeader("Sec-WebSocket-Key") == "" {
		own.ResponseError(http.StatusBadRequest, response.STATUS_BAD_REQUEST)
		return errUpgradeWithoutKey
	}

	// 获取 Hijacker 接口
	hijacker, ok := own.response.(http.Hijacker)
	if !ok {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
		return errNotSupportedHijacker
	}

	// 手动控制连接
	conn, reader, err := hijacker.Hijack()
	if err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
		return err
	}
	defer func() { _ = conn.Close() }()

	// 手动构建符合 WebSocket 规范的响应头
	if err := handshakeWebSocket(reader, header); err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
		return err
	}

	// 移交连接处理
	upgrade(websocket.NewConn(conn, reader))
	return nil
}

// checkHeaderValues 检查某个 Header 中是否包含目标值
func checkHeaderValues(values, target string) bool {
	targets := strings.Split(strings.ToLower(values), ",")
	for _, t := range targets {
		if strings.TrimSpace(t) == target {
			return true
		}
	}

	return false
}

// handshakeWebSocket 处理 WebSocket 的握手响应
func handshakeWebSocket(respWriter *bufio.ReadWriter, reqHeader http.Header) error {
	// 构建响应头
	headers := make(http.Header)
	headers.Set("Upgrade", "websocket")
	headers.Set("Connection", "Upgrade")
	headers.Set("Sec-WebSocket-Accept", computeAcceptKey(reqHeader.Get("Sec-WebSocket-Key")))

	// 处理子协议
	//if clientProtocols := reqHeader.Get("Sec-WebSocket-Protocol"); clientProtocols != "" {
	//	// 这里可以根据客户端发送的子协议选择一个，这里我们简单返回第一个
	//	protocols := strings.Split(clientProtocols, ",")
	//	if len(protocols) > 0 {
	//		headers.Set("Sec-WebSocket-Protocol", strings.TrimSpace(protocols[0]))
	//	}
	//}

	// 写入协议切换响应
	if _, err := respWriter.WriteString("HTTP/1.1 101 Switching Protocols\r\n"); err != nil {
		return err
	}

	// 写入头字段
	for key, values := range headers {
		for _, value := range values {
			if _, err := respWriter.WriteString(key + ": " + value + "\r\n"); err != nil {
				return err
			}
		}
	}

	// 结束头部分
	if _, err := respWriter.WriteString("\r\n"); err != nil {
		return err
	}

	return respWriter.Flush()
}

func computeAcceptKey(key string) string {
	hash := sha1.New()
	_, _ = io.WriteString(hash, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}

// validateWebSocketOrigin 验证 Origin
func (own *Context) validateWebSocketOrigin() error {
	origin := own.request.Header.Get("Origin")
	if origin == "" {
		return nil
	}

	// 自定义 Origin 验证逻辑
	if own.engine != nil {
		// 使用引擎的配置进行验证
	}

	return nil
}
