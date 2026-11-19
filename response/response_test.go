package response

import (
	"bytes"
	"io"
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
