package cache

import (
	"bufio"
	"bytes"
	"net"
	"net/http"

	"gitee.com/leminewx/leego/response"
)

// recorder 在透传响应的同时镜像一份副本，用于写入缓存。
//
// 镜像是"尽力而为"的：
//   - 超过 limit 后置 overflow 并丢弃已镜像的数据，后续字节继续透传；
//   - 连接被劫持（WebSocket）后不再镜像；
//   - 透传行为与直接使用原始 response.Writer 完全一致。
type recorder struct {
	// inner 为原始响应包装器。
	inner response.Writer
	// buffer 保存镜像的响应体。
	buffer bytes.Buffer
	// header 保存 WriteHeader 时刻的响应头快照。
	header http.Header
	// status 保存已写入的状态码。
	status int
	// limit 为镜像体上限，非正数表示不限制。
	limit int64
	// overflow 标记镜像体超过上限，此时不得写缓存。
	overflow bool
	// hijacked 标记连接已被劫持，此时不得写缓存。
	hijacked bool
	// wroteHeader 标记是否已经写出状态行。
	wroteHeader bool
}

// newRecorder 创建镜像响应包装器。
func newRecorder(inner response.Writer, limit int64) *recorder {
	return &recorder{inner: inner, limit: limit}
}

// Header 返回底层响应头。
func (own *recorder) Header() http.Header {
	return own.inner.Header()
}

// WriteHeader 透传状态码并快照响应头。
func (own *recorder) WriteHeader(status int) {
	// 1xx 信息性响应不锁定状态，按标准库语义直接透传。
	if status >= 100 && status < 200 {
		own.inner.WriteHeader(status)
		return
	}

	if own.wroteHeader {
		return
	}

	own.wroteHeader = true
	own.status = status
	own.header = own.inner.Header().Clone()
	own.inner.WriteHeader(status)
}

// Write 透传响应体并镜像到缓冲区。
func (own *recorder) Write(data []byte) (int, error) {
	if own.hijacked {
		return own.inner.Write(data)
	}

	if !own.wroteHeader {
		own.WriteHeader(http.StatusOK)
	}

	if !own.overflow && len(data) > 0 {
		if own.limit > 0 && int64(own.buffer.Len())+int64(len(data)) > own.limit {
			// 超限后放弃镜像：缓存"半截响应"比不缓存危险得多。
			own.overflow = true
			own.buffer.Reset()
		} else {
			_, _ = own.buffer.Write(data)
		}
	}

	return own.inner.Write(data)
}

// Flush 透传刷新请求。
func (own *recorder) Flush() {
	own.inner.Flush()
}

// Hijack 透传连接劫持，并停止镜像。
func (own *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	own.hijacked = true
	own.buffer.Reset()
	return own.inner.Hijack()
}

// GetStatusCode 返回底层记录的状态码。
func (own *recorder) GetStatusCode() int {
	return own.inner.GetStatusCode()
}

// GetSize 返回底层记录的字节数。
func (own *recorder) GetSize() int64 {
	return own.inner.GetSize()
}

// Unwrap 返回底层原始 http.ResponseWriter。
func (own *recorder) Unwrap() http.ResponseWriter {
	return own.inner.Unwrap()
}

// snapshot 生成缓存条目所需的响应快照。
//
// 调用方需自行判断是否可缓存；这里只负责把当前已写内容取出。
func (own *recorder) snapshot(key string) *entry {
	status := own.status
	if !own.wroteHeader {
		// 未显式写头时，底层可能已由 Write 回填 200。
		status = own.inner.GetStatusCode()
		if status == 0 {
			status = http.StatusOK
		}
	}

	header := own.header
	if header == nil {
		header = own.inner.Header().Clone()
	}

	return &entry{
		key:    key,
		status: status,
		header: header,
		body:   append([]byte(nil), own.buffer.Bytes()...),
	}
}

// captureWriter 是后台刷新专用的内存响应写入器。
//
// 后台刷新没有真实连接，因此只需要实现 http.ResponseWriter；
// response.NewWriter 会为它补上状态码与字节数记录。
type captureWriter struct {
	// header 为响应头。
	header http.Header
	// body 为响应体。
	body bytes.Buffer
	// status 为已写入的状态码。
	status int
	// limit 为响应体上限，非正数表示不限制。
	limit int64
	// overflow 标记响应体超过上限。
	overflow bool
}

// newCaptureWriter 创建内存响应写入器。
func newCaptureWriter(limit int64) *captureWriter {
	return &captureWriter{header: make(http.Header), limit: limit}
}

// Header 返回响应头集合。
func (own *captureWriter) Header() http.Header {
	return own.header
}

// WriteHeader 记录状态码。
func (own *captureWriter) WriteHeader(status int) {
	if own.status != 0 || (status >= 100 && status < 200) {
		return
	}

	own.status = status
}

// Write 记录响应体。
func (own *captureWriter) Write(data []byte) (int, error) {
	if own.status == 0 {
		own.status = http.StatusOK
	}

	if !own.overflow {
		if own.limit > 0 && int64(own.body.Len())+int64(len(data)) > own.limit {
			own.overflow = true
			own.body.Reset()
		} else {
			_, _ = own.body.Write(data)
		}
	}

	return len(data), nil
}

// Flush 为空实现，仅为兼容 http.Flusher 语义。
func (own *captureWriter) Flush() {}
