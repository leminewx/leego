package leego

import (
	"bufio"
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
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/sse"
	"gitee.com/leminewx/leego/response/websocket"
)

var maxMultipartMemory int64 = 10 << 20 // 10M

var (
	errNotFoundPage      = errors.New("leego: context: not found query parameter 'page'")
	errNotFoundLimit     = errors.New("leego: context: not found query parameter 'limit'")
	errFailToUpgrade     = errors.New("leego: context: failed to upgrade http connection to WebSocket")
	errUpgradeWithoutKey = errors.New("leego: context: upgrade headers does not carry 'Sec-WebSocket-Key'")
)

// SetMaxMultipartMemory 设置解析 multipart/form-data 多部分表单时最大使用的内存
func SetMaxMultipartMemory(size int64) {
	atomic.StoreInt64(&maxMultipartMemory, size)
}

// JSON 定义一个简单的 JSON 类型
type JSON map[string]any

// Context 定义上下文的结构
type Context struct {
	time     time.Time         // 创建时间
	query    url.Values        // 查询参数
	values   map[string]string // 路由参数
	engine   *Engine           // 绑定引擎
	Request  *http.Request     // 原始请求
	Response response.Writer   // 原始响应
}

// newContext 新建一个上下文
func newContext(engine *Engine, resp http.ResponseWriter, req *http.Request, values map[string]string) *Context {
	return &Context{
		time:     time.Now(),
		query:    req.URL.Query(),
		values:   values,
		engine:   engine,
		Request:  req,
		Response: response.NewWriter(resp),
	}
}

/**************************************************** HTTP Request ****************************************************/

// GetURL 获取 RUL 信息
func (own *Context) GetURL() *url.URL {
	return own.Request.URL
}

// GetPath 获取请求路径
func (own *Context) GetPath() string {
	return own.Request.URL.Path
}

// GetHost 获取主机信息
func (own *Context) GetHost() string {
	return own.Request.Host
}

// GetMethod 获取请求方法
func (own *Context) GetMethod() string {
	return own.Request.Method
}

// GetProtocol 获取请求协议
func (own *Context) GetProtocol() string {
	return own.Request.Proto
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
		_ = own.Request.ParseMultipartForm(maxMultipartMemory)
	}

	return own.Request.FormFile(filename)
}

// GetMultipartFormValue 仅支持查询 multipart/form-data 格式的 body
func (own *Context) GetMultipartFormValue(key string) string {
	if own.Request.MultipartForm == nil {
		_ = own.Request.ParseMultipartForm(maxMultipartMemory)
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

// GetHeader 获取请求头字段信息
func (own *Context) GetHeader(key string) string {
	return own.Request.Header.Get(key)
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

/*************************************************** HTTP Response ****************************************************/

// GetStartTime 获取上下文创建时间
func (own *Context) GetStartTime() time.Time {
	return own.time
}

// GetStatusCode 获取响应状态码
func (own *Context) GetStatusCode() int {
	return own.Response.GetStatusCode()
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

// ResponseXML 响应 XML 格式的数据
func (own *Context) ResponseXML(code int, data any) error {
	own.WithStatusCode(code)
	return xml.NewEncoder(own.Response).Encode(data)
}

// ResponseJSON 响应 JSON 格式的数据
func (own *Context) ResponseJSON(code int, data any) error {
	own.WithResponseHeader("Content-Type", "application/json")
	own.WithStatusCode(code)
	return json.NewEncoder(own.Response).Encode(&data)
}

// ResponseBytes 响应字节数据
func (own *Context) ResponseBytes(code int, data []byte) error {
	own.WithStatusCode(code)
	_, err := own.Response.Write(data)
	return err
}

// ResponseString 响应字符串
func (own *Context) ResponseString(code int, data string) error {
	own.WithStatusCode(code)
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
	own.WithResponseHeader("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"", disposition, info.Name()))
	own.WithResponseHeader("Content-Length", strconv.FormatInt(info.Size(), 10))

	// 设置 Content-Type
	if disposition == "inline" {
		own.WithResponseHeader("Content-Type", own.detectContentType(filename, file))
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

var (
	templates     = make(map[string]*template.Template)
	templatesLock sync.RWMutex
)

// ResponseHTML 响应 HTML 内容
func (own *Context) ResponseHTML(filename, tmplName string, data any) error {
	// 先尝试读锁
	templatesLock.RLock()
	tmpl, ok := templates[filename]
	if templatesLock.RUnlock(); ok {
		return own.executeTemplate(tmpl, tmplName, data)
	}

	// 不存在时需要写锁
	templatesLock.Lock()
	defer templatesLock.Unlock()

	// 双重检查
	if tmpl, exists := templates[filename]; exists {
		return own.executeTemplate(tmpl, tmplName, data)
	}

	// 解析模板
	tmpl, err := template.ParseFiles(filename)
	if err != nil {
		return fmt.Errorf("leego: context: failed to parse template: %w", err)
	}

	templates[filename] = tmpl
	return own.executeTemplate(tmpl, tmplName, data)
}

func (own *Context) executeTemplate(tmpl *template.Template, tmplName string, data any) error {
	if tmplName == "" {
		return tmpl.Execute(own.Response, data)
	}

	return tmpl.ExecuteTemplate(own.Response, tmplName, data)
}

// ResponseSSE 发起 SSE 事件响应
func (own *Context) ResponseSSE(sseMessages func(ctx context.Context) <-chan *sse.Message) (err error) {
	// 设置 SSE 响应头
	sse.SetResponseHeader(own.Response)

	// 调用 SSE 处理函数
	ctx := own.Request.Context()
	messages := sseMessages(ctx)

	for {
		select {
		// 正常接收数据
		case msg, ok := <-messages:
			// 通道关闭
			if !ok {
				// 关闭前发送数据
				if msg != nil && len(msg.Data) > 0 {
					if err = msg.Write(own.Response); err != nil {
						return err
					}
				}

				// 尝试获取事件 ID
				var id string
				if msg != nil {
					id = msg.Id
				}

				// 发送关闭事件
				return sse.NewClosedMessage(id).Write(own.Response)
			}

			// 跳过空数据
			if msg == nil || len(msg.Data) == 0 {
				continue
			}

			// 发送事件数据
			if err := msg.Write(own.Response); err != nil {
				return err
			}

		// 客户端断开连接
		case <-ctx.Done():
			return nil
		}
	}
}

// ResponseUpgrade 发起 Websocket 响应
func (own *Context) ResponseUpgrade(upgrade func(conn *websocket.Conn)) error {
	header := own.Request.Header

	// 校验请求头
	switch {
	// Connection 头必须为 upgrade
	case !checkHeaderValues(header.Get("Connection"), "upgrade"):
		own.ResponseError(http.StatusBadRequest, response.STATUS_400_BAD_REQUEST)
		return fmt.Errorf("leego: context: ")

	// Upgrade 头必须为 websocket
	case !strings.EqualFold(header.Get("Upgrade"), "websocket"):
		own.ResponseError(http.StatusBadRequest, response.STATUS_400_BAD_REQUEST)
		return errFailToUpgrade

	// Sec-WebSocket-Key 头必须为有效值
	case own.GetHeader("Sec-WebSocket-Key") == "":
		own.ResponseError(http.StatusBadRequest, response.STATUS_400_BAD_REQUEST)
		return errUpgradeWithoutKey
	}

	// 手动控制连接
	conn, reader, err := own.Response.Hijack()
	if err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_500_INTERNAL_SERVER_ERROR)
		return err
	}
	defer func() { _ = conn.Close() }()

	// 手动构建符合 WebSocket 规范的响应头
	if err := handshakeWebSocket(reader, header); err != nil {
		own.ResponseError(http.StatusInternalServerError, response.STATUS_500_INTERNAL_SERVER_ERROR)
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

const _UPGRADE_ACCEPT_KEY = "258WAFA5-E914-47DA-95CA-C5AB0DC85B11"

func computeAcceptKey(key string) string {
	hash := sha1.New()
	_, _ = io.WriteString(hash, key+_UPGRADE_ACCEPT_KEY)
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}

// validateWebSocketOrigin 验证 Origin
func (own *Context) validateWebSocketOrigin() error {
	origin := own.Request.Header.Get("Origin")
	if origin == "" {
		return nil
	}

	// 自定义 Origin 验证逻辑
	if own.engine != nil {
		// 使用引擎的配置进行验证
	}

	return nil
}
