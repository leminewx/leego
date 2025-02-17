package response

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

var errNotHijack = errors.New("leego: not supported HiJacker")

type (
	// ResponseWriter 自定义 HTTP 的 响应接口，用于继承 http.ResponseWriter
	ResponseWriter interface {
		// 继承 http.Flusher
		http.Flusher

		// 继承 http.Hijacker
		http.Hijacker

		// 继承 http.ResponseWriter
		http.ResponseWriter

		// GetStatusCode 获取响应状态码
		GetStatusCode() int
	}

	// response 定义响应的数据结构
	response struct {
		status int
		writer http.ResponseWriter
	}
)

// NewResponse 创建一个自定义的响应
func NewResponseWriter(resp http.ResponseWriter) *response {
	return &response{
		status: http.StatusOK,
		writer: resp,
	}
}

func (own *response) Header() http.Header {
	return own.writer.Header()
}

func (own *response) Write(data []byte) (int, error) {
	return own.writer.Write(data)
}

func (own *response) WriteHeader(status int) {
	own.status = status
	own.writer.WriteHeader(own.status)
}

func (own *response) GetStatusCode() int {
	return own.status
}

func (own *response) Flush() {
	if flusher, ok := own.writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (own *response) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := own.writer.(http.Hijacker); ok {
		return hijacker.Hijack()
	}

	return nil, nil, errNotHijack
}
