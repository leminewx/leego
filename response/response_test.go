package response

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// readerFromRecorder 记录 ReadFrom 调用次数，用于验证委托行为。
type readerFromRecorder struct {
	*httptest.ResponseRecorder
	readFromCalls int
	written       bytes.Buffer
}

// ReadFrom 实现 io.ReaderFrom，模拟支持 sendfile 的底层实现。
func (own *readerFromRecorder) ReadFrom(reader io.Reader) (int64, error) {
	own.readFromCalls++
	return own.written.ReadFrom(reader)
}

// TestNewWriterDefaults 验证新建包装器的初始状态。
func TestNewWriterDefaults(t *testing.T) {
	writer := NewWriter(httptest.NewRecorder())

	if writer.GetStatusCode() != 0 {
		t.Fatalf("初始状态码 = %d, want 0", writer.GetStatusCode())
	}

	if writer.GetSize() != 0 {
		t.Fatalf("初始字节数 = %d, want 0", writer.GetSize())
	}
}

// TestWriteFillsStatus 验证首次 Write 会自动回填 200。
func TestWriteFillsStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewWriter(recorder)

	size, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if size != 5 {
		t.Fatalf("Write() size = %d, want 5", size)
	}

	if writer.GetStatusCode() != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", writer.GetStatusCode())
	}

	if writer.GetSize() != 5 {
		t.Fatalf("字节数 = %d, want 5", writer.GetSize())
	}

	if recorder.Body.String() != "hello" {
		t.Fatalf("响应体 = %q, want hello", recorder.Body.String())
	}
}

// TestWriteHeaderIsIdempotent 验证重复写入状态码只有第一次生效。
func TestWriteHeaderIsIdempotent(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewWriter(recorder)

	writer.WriteHeader(http.StatusCreated)
	writer.WriteHeader(http.StatusInternalServerError)

	if writer.GetStatusCode() != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201", writer.GetStatusCode())
	}

	if recorder.Code != http.StatusCreated {
		t.Fatalf("底层状态码 = %d, want 201", recorder.Code)
	}
}

// TestWriteHeaderInformationalKeepsStatus 验证 1xx 响应不锁定最终状态码。
func TestWriteHeaderInformationalKeepsStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewWriter(recorder)

	writer.WriteHeader(http.StatusEarlyHints)
	if writer.GetStatusCode() != 0 {
		t.Fatalf("1xx 后状态码 = %d, want 0（不应锁定）", writer.GetStatusCode())
	}

	writer.WriteHeader(http.StatusAccepted)
	if writer.GetStatusCode() != http.StatusAccepted {
		t.Fatalf("最终状态码 = %d, want 202", writer.GetStatusCode())
	}
}

// TestReadFromFallsBackToCopy 验证底层不支持 ReaderFrom 时退化为 io.Copy。
func TestReadFromFallsBackToCopy(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewWriter(recorder)

	readerFrom, ok := writer.(io.ReaderFrom)
	if !ok {
		t.Fatal("Writer 必须实现 io.ReaderFrom，以支持文件下载优化")
	}

	size, err := readerFrom.ReadFrom(strings.NewReader("abcdef"))
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}

	if size != 6 {
		t.Fatalf("ReadFrom() size = %d, want 6", size)
	}

	if writer.GetSize() != 6 {
		t.Fatalf("字节数 = %d, want 6", writer.GetSize())
	}

	if writer.GetStatusCode() != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", writer.GetStatusCode())
	}
}

// TestReadFromDelegates 验证底层支持 ReaderFrom 时直接委托。
func TestReadFromDelegates(t *testing.T) {
	recorder := &readerFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	writer := NewWriter(recorder)

	readerFrom, ok := writer.(io.ReaderFrom)
	if !ok {
		t.Fatal("Writer 必须实现 io.ReaderFrom，以支持文件下载优化")
	}

	size, err := readerFrom.ReadFrom(strings.NewReader("sendfile"))
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}

	if size != 8 {
		t.Fatalf("ReadFrom() size = %d, want 8", size)
	}

	if recorder.readFromCalls != 1 {
		t.Fatalf("ReadFrom 委托次数 = %d, want 1", recorder.readFromCalls)
	}

	if recorder.written.String() != "sendfile" {
		t.Fatalf("委托写入内容 = %q, want sendfile", recorder.written.String())
	}
}

// hijackRecorder 模拟支持连接劫持的底层 ResponseWriter。
type hijackRecorder struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

// Hijack 返回预先建立的管道连接与缓冲读写器。
func (own *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return own.conn, bufio.NewReadWriter(bufio.NewReader(own.conn), bufio.NewWriter(own.conn)), nil
}

// TestFlushRecordsStatus 验证 Flush 隐式写入的 200 会被记录。
//
// 修复前 Flush 只转发到底层，访问日志与指标会把 SSE 响应误判为“未写响应”。
func TestFlushRecordsStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewWriter(recorder)

	writer.Flush()

	if writer.GetStatusCode() != http.StatusOK {
		t.Fatalf("Flush 后状态码 = %d, want 200", writer.GetStatusCode())
	}

	if !recorder.Flushed {
		t.Fatal("Flush 必须转发到底层写入器")
	}
}

// TestHijackRecordsSwitchingProtocols 验证劫持成功后记录 101。
//
// 劫持后由上层协议（如 WebSocket）自行写响应行，因此只记录状态码；
// 不记录会让访问日志与指标把升级请求当成 0 状态。
func TestHijackRecordsSwitchingProtocols(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	writer := NewWriter(&hijackRecorder{ResponseRecorder: httptest.NewRecorder(), conn: server})

	connection, readWriter, err := writer.Hijack()
	if err != nil || connection == nil || readWriter == nil {
		t.Fatalf("Hijack() = (%v, %v, %v)", connection, readWriter, err)
	}

	if writer.GetStatusCode() != http.StatusSwitchingProtocols {
		t.Fatalf("劫持后状态码 = %d, want 101", writer.GetStatusCode())
	}
}

// TestHijackUnsupported 验证底层不支持劫持时返回约定错误。
func TestHijackUnsupported(t *testing.T) {
	writer := NewWriter(httptest.NewRecorder())

	connection, readWriter, err := writer.Hijack()
	if err == nil {
		t.Fatal("期望返回 errNotHijack")
	}

	if connection != nil || readWriter != nil {
		t.Fatal("失败时必须返回 nil 连接与 nil 读写器")
	}

	// 劫持失败不得记录 101，否则日志会把普通响应误判为协议升级。
	if writer.GetStatusCode() != 0 {
		t.Fatalf("劫持失败后状态码 = %d, want 0", writer.GetStatusCode())
	}
}

// TestUnwrap 验证能够取回原始 ResponseWriter。
func TestUnwrap(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewWriter(recorder)

	if writer.Unwrap() != recorder {
		t.Fatal("Unwrap() 未返回原始 ResponseWriter")
	}
}

// TestWriterPoolReuse 验证对象池复用后状态被正确重置。
func TestWriterPoolReuse(t *testing.T) {
	pool := NewWriterPool()

	first := pool.Acquire(httptest.NewRecorder())
	_, _ = first.Write([]byte("first"))
	first.WriteHeader(http.StatusTeapot)

	if first.GetStatusCode() != http.StatusOK {
		t.Fatalf("首次状态码 = %d, want 200", first.GetStatusCode())
	}

	pool.Release(first)

	secondRecorder := httptest.NewRecorder()
	second := pool.Acquire(secondRecorder)

	if second.GetStatusCode() != 0 {
		t.Fatalf("复用后状态码 = %d, want 0", second.GetStatusCode())
	}

	if second.GetSize() != 0 {
		t.Fatalf("复用后字节数 = %d, want 0", second.GetSize())
	}

	if second.Unwrap() != secondRecorder {
		t.Fatal("复用后未绑定新的 ResponseWriter")
	}

	// 归还外来对象应被安全忽略，不产生 panic。
	pool.Release(foreignWriter{})
}

// foreignWriter 是一个不来自对象池的 Writer 实现。
type foreignWriter struct {
	Writer
}

// BenchmarkNewWriter 测量每次新建包装器的开销。
func BenchmarkNewWriter(b *testing.B) {
	recorder := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		writer := NewWriter(recorder)
		_, _ = writer.Write([]byte("x"))
	}
}

// BenchmarkWriterPool 测量对象池路径的开销。
func BenchmarkWriterPool(b *testing.B) {
	pool := NewWriterPool()
	recorder := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		writer := pool.Acquire(recorder)
		_, _ = writer.Write([]byte("x"))
		pool.Release(writer)
	}
}
