package response

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
)

// errNotHijack 表示底层 http.ResponseWriter 不支持连接劫持。
var errNotHijack = errors.New("leego: response: not supported Hijacker")

// Writer 定义框架内部使用的响应接口。
//
// 它在 http.ResponseWriter 的基础上补充了状态码与字节数记录能力，并显式
// 组合 Flusher / Hijacker，使 SSE、WebSocket、文件下载与指标采集可以复用
// 同一层抽象。Unwrap 用于兼容 http.ResponseController 与 sendfile 优化。
type Writer interface {
	// Flusher 继承 http.Flusher。
	http.Flusher
	// Hijacker 继承 http.Hijacker。
	http.Hijacker
	// ResponseWriter 继承 http.ResponseWriter。
	http.ResponseWriter
	// GetStatusCode 返回已写入的状态码；尚未写入时返回 0。
	GetStatusCode() int
	// GetSize 返回已写入响应体的字节数。
	GetSize() int64
	// Unwrap 返回被包装的原始 http.ResponseWriter。
	Unwrap() http.ResponseWriter
}

// 编译期断言：response 必须实现 Writer。
var _ Writer = &response{}

// response 是 Writer 的默认实现。
//
// 关键语义：
//   - 首次 Write 会自动回填状态码 200，保证指标与访问日志不会出现 0；
//   - WriteHeader 幂等，重复调用不会改变已经发送的状态码与已记录的状态码；
//   - size 使用 int64，避免大文件下载时在 32 位平台溢出。
type response struct {
	size        int64
	status      int
	wroteHeader bool
	writer      http.ResponseWriter
}

// NewWriter 基于原始 http.ResponseWriter 创建一个响应包装器。
func NewWriter(resp http.ResponseWriter) Writer {
	return &response{writer: resp}
}

// Reset 重置响应包装器，使其可以安全复用。
//
// 该函数只应由 WriterPool 在归还对象后调用，业务代码不应直接使用。
func (own *response) Reset(resp http.ResponseWriter) {
	own.size = 0
	own.status = 0
	own.wroteHeader = false
	own.writer = resp
}

// Flush 将缓冲区数据发送给客户端。
//
// 若底层实现不支持 http.Flusher，则静默忽略，保持与标准库一致的容错行为。
// 与 net/http 语义一致，Flush 会隐式发送 200 状态行，因此这里先同步记录
// 状态码，避免日志与指标把它误判为“未写响应”。
func (own *response) Flush() {
	if !own.wroteHeader {
		own.WriteHeader(http.StatusOK)
	}

	if flusher, ok := own.writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 劫持底层连接，供 WebSocket 等协议升级使用。
//
// 若底层实现不支持 http.Hijacker，返回 errNotHijack。
func (own *response) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := own.writer.(http.Hijacker)
	if !ok {
		return nil, nil, errNotHijack
	}

	connection, readWriter, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}

	// 劫持后由上层协议自行写响应（如 WebSocket 101 握手），
	// 因此只记录状态码，不调用底层 WriteHeader。
	own.wroteHeader = true
	own.status = http.StatusSwitchingProtocols
	return connection, readWriter, nil
}

// Header 返回底层响应头集合。
func (own *response) Header() http.Header {
	return own.writer.Header()
}

// WriteHeader 写入响应状态码。
//
// 重复调用会被忽略：HTTP 协议只允许发送一次状态行，重复写入既无法生效，
// 也会让指标记录与实际响应不一致。
func (own *response) WriteHeader(status int) {
	if own.wroteHeader {
		return
	}

	// 1xx 信息性响应不终止后续真实响应，按标准库语义直接透传且不锁定状态。
	if status >= 100 && status < 200 {
		own.writer.WriteHeader(status)
		return
	}

	own.wroteHeader = true
	own.status = status
	own.writer.WriteHeader(status)
}

// Write 写入响应体。
//
// 若业务代码未显式调用 WriteHeader，则按 net/http 语义自动回填 200，
// 同时同步到框架记录的状态码，保证日志与指标准确。
func (own *response) Write(data []byte) (int, error) {
	if !own.wroteHeader {
		own.WriteHeader(http.StatusOK)
	}

	size, err := own.writer.Write(data)
	own.size += int64(size)
	return size, err
}

// ReadFrom 实现 io.ReaderFrom，用于文件下载等场景。
//
// 若底层支持 io.ReaderFrom（例如 *http.response 支持 sendfile），则直接委托，
// 避免用户态拷贝；否则退化为 io.Copy。
func (own *response) ReadFrom(reader io.Reader) (int64, error) {
	if !own.wroteHeader {
		own.WriteHeader(http.StatusOK)
	}

	if readerFrom, ok := own.writer.(io.ReaderFrom); ok {
		size, err := readerFrom.ReadFrom(reader)
		own.size += size
		return size, err
	}

	size, err := io.Copy(own.writer, reader)
	own.size += size
	return size, err
}

// GetStatusCode 返回已写入的状态码；尚未写入时返回 0。
func (own *response) GetStatusCode() int {
	return own.status
}

// GetSize 返回已写入响应体的字节数。
func (own *response) GetSize() int64 {
	return own.size
}

// Unwrap 返回被包装的原始 http.ResponseWriter。
//
// 该方法使 http.ResponseController 能够继续访问底层连接能力。
func (own *response) Unwrap() http.ResponseWriter {
	return own.writer
}

// WriterPool 定义响应包装器对象池。
//
// 每个请求创建一个 response 对象会产生一次堆分配；通过对象池可以把该分配
// 从热路径中消除。对象池只复用包装器本身，不复用底层 ResponseWriter。
type WriterPool struct {
	pool sync.Pool
}

// NewWriterPool 创建一个响应包装器对象池。
func NewWriterPool() *WriterPool {
	return &WriterPool{
		pool: sync.Pool{
			New: func() any { return &response{} },
		},
	}
}

// Acquire 从池中取出一个响应包装器并绑定到底层 ResponseWriter。
func (own *WriterPool) Acquire(resp http.ResponseWriter) Writer {
	writer := own.pool.Get().(*response)
	writer.Reset(resp)
	return writer
}

// Release 将响应包装器归还对象池。
//
// 归还前会清除对底层 ResponseWriter 的引用，避免对象池持有请求级对象导致
// 内存无法回收。非本池创建的对象会被安全忽略。
func (own *WriterPool) Release(writer Writer) {
	wrapped, ok := writer.(*response)
	if !ok {
		return
	}

	wrapped.Reset(nil)
	own.pool.Put(wrapped)
}
