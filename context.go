package leego

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"

	"gitee.com/leminewx/gokit/pool"
)

var maxMultipartMemory int64 = 30 << 20 // 30M

func SetMaxMultipartMemory(size int64) {
	maxMultipartMemory = size
}

// Json 定义一个简单的 Json 类型
type Json map[string]any

// Context 定义上下文的结构
type Context struct {
	StatusCode int

	queryValues url.Values

	engine   *Engine
	request  *http.Request
	response http.ResponseWriter
}

// newContext 新建一个上下文
func newContext(engine *Engine, resp http.ResponseWriter, req *http.Request) *Context {
	return &Context{
		queryValues: req.URL.Query(),
		engine:      engine,
		request:     req,
		response:    resp,
	}
}

func (own *Context) GetURL() *url.URL { return own.request.URL }

func (own *Context) GetHost() string { return own.request.Host }

func (own *Context) GetMethod() string { return own.request.Method }

func (own *Context) GetProtocol() string { return own.request.Proto }

func (own *Context) GetPathValue(name string) string { return own.request.PathValue(name) }

func (own *Context) GetQueryValue(key string) string { return own.queryValues.Get(key) }

func (own *Context) GetFormValue(key string) string {
	if own.request.Form == nil {
		own.request.ParseForm()
	}

	return own.request.Form.Get(key)
}

func (own *Context) GetFormFile(filename string) (multipart.File, *multipart.FileHeader, error) {
	if own.request.MultipartForm == nil {
		own.request.ParseMultipartForm(maxMultipartMemory)
	}

	return own.request.FormFile(filename)
}

func (own *Context) GetPostFormValue(key string) string {
	if own.request.PostForm == nil {
		own.request.ParseForm()
	}

	return own.request.PostForm.Get(key)
}

func (own *Context) GetMultipartFormValue(key string) string {
	if own.request.MultipartForm == nil {
		own.request.ParseMultipartForm(maxMultipartMemory)
	}

	if params := own.request.MultipartForm.Value[key]; len(params) > 0 {
		return params[0]
	}

	return ""
}

func (own *Context) GetSeriveName() string {
	return own.engine.service
}

func (own *Context) GetHeader(key string) string {
	return own.request.Header.Get(key)
}

func (own *Context) ReadJson(data any) error {
	buf := pool.DefualtByteBufferPool.Alloc()
	if _, err := io.Copy(buf, own.request.Body); err != nil {
		return err
	}
	own.request.Body.Close()

	err := json.Unmarshal(buf.Bytes(), &data)
	pool.DefualtByteBufferPool.Free(buf)
	return err
}

func (own *Context) GetRequest() *http.Request { return own.request }

func (own *Context) GetResponse() http.ResponseWriter { return own.response }

func (own *Context) SetHeader(key, value string) { own.response.Header().Set(key, value) }

func (own *Context) WriteCode(code int) {
	own.StatusCode = code
	own.response.WriteHeader(code)
}

func (own *Context) WithContext(ctx context.Context) *Context {
	own.request = own.request.WithContext(ctx)
	return own
}

func (own *Context) ResponseSSE(sse func() <-chan []byte) (err error) {
	own.SetHeader("Connection", "keep-alive")
	own.SetHeader("Content-Type", "text/event-stream")
	own.SetHeader("Cache-Control", "no-cache")

	var buffer bytes.Buffer
	for data := range sse() {
		if _, err = buffer.Write([]byte("data: ")); err != nil {
			return err
		}
		if _, err = buffer.Write(data); err != nil {
			return err
		}
		if _, err = buffer.Write([]byte("\n\n")); err != nil {
			return err
		}
		if _, err = own.response.Write(buffer.Bytes()); err != nil {
			return err
		}

		own.response.(http.Flusher).Flush()
		buffer.Reset()
	}

	_, err = fmt.Fprintln(own.response, "data: Connection closed by server")
	return err
}

func (own *Context) ResponseJSON(code int, data any) error {
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		return err
	}

	own.StatusCode = code
	own.response.WriteHeader(code)
	_, err = own.response.Write(dataBytes)
	return err
}

func (own *Context) ResponseFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	own.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", info.Name()))
	own.SetHeader("Content-Length", fmt.Sprintf("%d", info.Size()))

	_, err = io.Copy(own.response, file)
	return err
}

func (own *Context) ResponseFileContent(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	own.SetHeader("Content-Type", http.DetectContentType([]byte(info.Name())))
	own.SetHeader("Content-Length", fmt.Sprintf("%d", info.Size()))

	_, err = io.Copy(own.response, file)
	return err
}

func (own *Context) ResponseFail(code int, error string) {
	own.StatusCode = code
	http.Error(own.response, error, code)
}

var templates = make(map[string]*template.Template)

func (own *Context) ResponseHTML(filename, tmplname string, data any) error {
	tmpl, ok := templates[filename]
	if !ok {
		tmpl = template.Must(template.ParseFiles(filename))
		templates[filename] = tmpl
	}

	if tmplname == "" {
		return tmpl.Execute(own.response, &data)
	}

	return tmpl.ExecuteTemplate(own.response, tmplname, &data)
}

func (own *Context) ResponseBytes(code int, data []byte) error {
	own.StatusCode = code
	own.response.WriteHeader(code)
	_, err := own.response.Write(data)
	return err
}

func (own *Context) ResponseString(code int, data string) error {
	own.StatusCode = code
	own.response.WriteHeader(code)
	_, err := own.response.Write([]byte(data))
	return err
}

func (own *Context) ResponseUpgrade(upgrade func(conn net.Conn)) error {
	header := own.request.Header
	if !strings.Contains(header.Get("Connection"), "upgrade") || !strings.EqualFold(header.Get("Upgrade"), "websocket") {
		return fmt.Errorf("failed to upgrade connection to WebSocket")
	}

	// set the header of WebSocket
	own.SetHeader("Upgrade", "websocket")
	own.SetHeader("Connection", "Upgrade")
	own.SetHeader("Sec-WebSocket-Accept", computeAcceptKey(header.Get("Sec-WebSocket-Key")))

	own.response.WriteHeader(http.StatusSwitchingProtocols)

	// get the HiJacker from the response
	hijacker, ok := own.response.(http.Hijacker)
	if !ok {
		http.Error(own.response, "Not supported HiJacker", http.StatusBadRequest)
		return fmt.Errorf("http.ResponseWriter not supported HiJacker")
	}

	// get the tcp connection from the HiJacker
	conn, _, err := hijacker.Hijack()
	if err != nil {
		return err
	}

	upgrade(conn)
	return conn.Close()
}

func computeAcceptKey(key string) string {
	hash := sha1.New()
	io.WriteString(hash, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}
