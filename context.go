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
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"

	"gitee.com/leminewx/gokit/util/pool"
	"gitee.com/leminewx/leego/response/status"
	"gitee.com/leminewx/leego/response/websocket"
)

var maxMultipartMemory int64 = 10 << 20 // 10M

var (
	errNotFoundPage      = errors.New("leego: not found parameter 'page'")
	errNotFoundLimit     = errors.New("leego: not found parameter 'limit'")
	errNotFlusher        = errors.New("leego: http.ResponseWriter is not a Flusher")
	errNotHijacker       = errors.New("leego: http.ResponseWriter is not a Hijacker")
	errFailToUpgrade     = errors.New("leego: failed to upgrade connection to WebSocket")
	errUpgradeWithoutKey = errors.New("leego: upgrade header is without key")
)

func SetMaxMultipartMemory(size int64) {
	maxMultipartMemory = size
}

// Json 定义一个简单的 Json 类型
type Json map[string]any

// Context 定义上下文的结构
type Context struct {
	StatusCode int

	query    url.Values
	engine   *Engine
	request  *http.Request
	response http.ResponseWriter
}

// newContext 新建一个上下文
func newContext(engine *Engine, resp http.ResponseWriter, req *http.Request) *Context {
	return &Context{
		query:    req.URL.Query(),
		engine:   engine,
		request:  req,
		response: resp,
	}
}

/***********************************************************************************************************************
***************************************************** HTTP Request *****************************************************
***********************************************************************************************************************/

// GetURL 获取 RUL 信息
func (own *Context) GetURL() *url.URL { return own.request.URL }

// GetHost 获取主机信息
func (own *Context) GetHost() string { return own.request.Host }

// GetMethod 获取请求方法
func (own *Context) GetMethod() string { return own.request.Method }

// GetProtocol 获取请求协议
func (own *Context) GetProtocol() string { return own.request.Proto }

// QueryValue 获取查询参数
func (own *Context) QueryValue(key string) string { return own.query.Get(key) }

// GetPathValue 获取动态路由参数
func (own *Context) GetPathValue(name string) string { return own.request.PathValue(name) }

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
		own.request.ParseForm()
	}

	return own.request.Form.Get(key)
}

// GetPostFormValue 仅支持查询 x-www-form-urlencoded 格式的 body
func (own *Context) GetPostFormValue(key string) string {
	if own.request.PostForm == nil {
		own.request.ParseForm()
	}

	return own.request.PostForm.Get(key)
}

// GetFormFile 获取上传的文件信息
func (own *Context) GetFormFile(filename string) (multipart.File, *multipart.FileHeader, error) {
	if own.request.MultipartForm == nil {
		own.request.ParseMultipartForm(maxMultipartMemory)
	}

	return own.request.FormFile(filename)
}

// GetMultipartFormValue 仅支持查询 multipart/form-data 格式的 body
func (own *Context) GetMultipartFormValue(key string) string {
	if own.request.MultipartForm == nil {
		own.request.ParseMultipartForm(maxMultipartMemory)
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

// ReadXml 读取 XML 格式的数据
func (own *Context) ReadXml(data any) error {
	buf := pool.DefaultBytesBufferPool.Alloc()
	if _, err := io.Copy(buf, own.request.Body); err != nil {
		return err
	}
	own.request.Body.Close()

	err := xml.Unmarshal(buf.Bytes(), &data)
	pool.DefaultBytesBufferPool.Dealloc(buf)
	return err
}

// ReadJson 读取 JSON 格式的数据
func (own *Context) ReadJson(data any) error {
	buf := pool.DefaultBytesBufferPool.Alloc()
	if _, err := io.Copy(buf, own.request.Body); err != nil {
		return err
	}
	own.request.Body.Close()

	err := json.Unmarshal(buf.Bytes(), &data)
	pool.DefaultBytesBufferPool.Dealloc(buf)
	return err
}

// GetRequest 获取原始请求
func (own *Context) GetRequest() *http.Request { return own.request }

// GetResponse 获取原始响应
func (own *Context) GetResponse() http.ResponseWriter { return own.response }

// SetHeader 设置响应头
func (own *Context) SetHeader(key, value string) { own.response.Header().Set(key, value) }

// WriteCode 写入响应状态码
func (own *Context) WriteCode(code int) {
	own.StatusCode = code
	own.response.WriteHeader(code)
}

// WithContext 设置请求上下文
func (own *Context) WithContext(ctx context.Context) *Context {
	own.request = own.request.WithContext(ctx)
	return own
}

/***********************************************************************************************************************
**************************************************** HTTP Response *****************************************************
***********************************************************************************************************************/

// ResponseFail 响应错误信息
func (own *Context) ResponseFail(code int, error string) {
	own.StatusCode = code
	http.Error(own.response, error, code)
}

// ResponseXML 响应 XML 格式的数据
func (own *Context) ResponseXML(code int, data any) error {
	dataBytes, err := xml.Marshal(&data)
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}

	own.WriteCode(code)
	_, err = own.response.Write(dataBytes)
	return err
}

// ResponseJSON 响应 JSON 格式的数据
func (own *Context) ResponseJSON(code int, data any) error {
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}

	own.SetHeader("Content-Type", "application/json")
	own.WriteCode(code)
	_, err = own.response.Write(dataBytes)
	return err
}

// ResponseBytes 响应字节数据
func (own *Context) ResponseBytes(code int, data []byte) error {
	own.WriteCode(code)
	_, err := own.response.Write(data)
	return err
}

// ResponseString 响应字符串
func (own *Context) ResponseString(code int, data string) error {
	own.WriteCode(code)
	_, err := own.response.Write([]byte(data))
	return err
}

// ResponseFile 响应文件（下载文件）
func (own *Context) ResponseFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}

	own.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", info.Name()))
	own.SetHeader("Content-Length", fmt.Sprintf("%d", info.Size()))

	_, err = io.Copy(own.response, file)
	return err
}

// ResponseFileContent 响应文件内容（页面显示）
func (own *Context) ResponseFileContent(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}

	own.SetHeader("Content-Type", http.DetectContentType([]byte(info.Name())))
	own.SetHeader("Content-Length", fmt.Sprintf("%d", info.Size()))

	_, err = io.Copy(own.response, file)
	return err
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
	own.SetHeader("Connection", "keep-alive")
	own.SetHeader("Content-Type", "text/event-stream")
	own.SetHeader("Cache-Control", "no-cache")

	flusher, ok := own.response.(http.Flusher)
	if !ok {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return errNotFlusher
	}

	events := sse()
	var buffer bytes.Buffer
	for data := range events {
		buffer.Grow(len(data) + 8)
		if _, err = buffer.WriteString("data: "); err != nil {
			return err
		}

		if _, err = buffer.Write(data); err != nil {
			return err
		}

		if _, err = buffer.WriteString("\n\n"); err != nil {
			return err
		}

		if _, err = own.response.Write(buffer.Bytes()); err != nil {
			return err
		}

		flusher.Flush()
		buffer.Reset()
	}

	_, err = own.response.Write([]byte("data: Connection closed by server\n\n"))
	return err
}

// ResponseUpgrade 发起 Websocket 响应
func (own *Context) ResponseUpgrade(upgrade func(conn *websocket.Conn)) error {
	// 校验请求头
	header := own.request.Header
	if !CheckHeaderValues(header.Get("Connection"), "upgrade") || !strings.EqualFold(header.Get("Upgrade"), "websocket") {
		own.ResponseFail(http.StatusBadRequest, status.BAD_REQUEST)
		return errFailToUpgrade
	}

	// 必须包含有效的 Sec-WebSocket-Key
	if len(header.Get("Sec-WebSocket-Key")) == 0 {
		own.ResponseFail(http.StatusBadRequest, status.BAD_REQUEST)
		return errUpgradeWithoutKey
	}

	// 获取 Hijacker 接口
	hijacker, ok := own.response.(http.Hijacker)
	if !ok {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return errNotHijacker
	}

	// 手动控制连接
	conn, reader, err := hijacker.Hijack()
	if err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}
	defer conn.Close()

	// 手动构建符合 WebSocket 规范的响应头
	if err := HandshakeWebSocket(reader, header); err != nil {
		own.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
		return err
	}

	// 移交连接处理
	upgrade(websocket.NewConn(conn, reader))
	return nil
}

// CheckHeaderValues 检查某个 Header 中是否包含目标值
func CheckHeaderValues(values, target string) bool {
	targets := strings.Split(strings.ToLower(values), ",")
	for _, t := range targets {
		if strings.TrimSpace(t) == target {
			return true
		}
	}

	return false
}

// HandshakeWebSocket 处理 WebSocket 的握手响应
func HandshakeWebSocket(respWriter *bufio.ReadWriter, reqHeader http.Header) error {
	// 构建响应头
	headers := make(http.Header)
	headers.Set("Upgrade", "websocket")
	headers.Set("Connection", "Upgrade")
	headers.Set("Sec-WebSocket-Accept", computeAcceptKey(reqHeader.Get("Sec-WebSocket-Key")))

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
	io.WriteString(hash, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}
