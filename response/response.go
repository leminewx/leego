package response

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

var errNotHijack = errors.New("leego: response: not supported Hijacker")

// Writer 自定义 HTTP 的 响应接口，继承 http.ResponseWriter
type Writer interface {
	// Flusher 继承 http.Flusher
	http.Flusher

	// Hijacker 继承 http.Hijacker
	http.Hijacker

	// ResponseWriter 继承 http.ResponseWriter
	http.ResponseWriter

	// GetStatusCode 获取响应状态码
	GetStatusCode() int
}

var _ Writer = &response{}

// response 定义响应的数据结构
type response struct {
	size   int
	status int
	writer http.ResponseWriter
}

// NewWriter 创建一个基于 http.ResponseWriter 的响应
func NewWriter(resp http.ResponseWriter) Writer {
	return &response{writer: resp}
}

// Flush 继承 http.Flusher
func (own *response) Flush() {
	if flusher, ok := own.writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 继承 http.Hijacker
func (own *response) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := own.writer.(http.Hijacker); ok {
		return hijacker.Hijack()
	}

	return nil, nil, errNotHijack
}

// Header 继承 http.ResponseWriter
func (own *response) Header() http.Header {
	return own.writer.Header()
}

// WriteHeader 继承 http.ResponseWriter
func (own *response) WriteHeader(status int) {
	own.status = status
	own.writer.WriteHeader(own.status)
}

// Write 继承 http.ResponseWriter
func (own *response) Write(data []byte) (int, error) {
	size, err := own.writer.Write(data)
	own.size += size
	return size, err
}

// GetStatusCode 获取响应状态码
func (own *response) GetStatusCode() int {
	return own.status
}
