package bench

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/leminewx/leego"
)

// discardWriter 是最小化的 http.ResponseWriter：只记录状态码与字节数。
//
// httptest.ResponseRecorder 每次请求都会分配 header map 与响应缓冲，
// 用它测框架热路径会把测试工具的开销算进来；这里只保留协议必需的行为，
// 并且支持跨迭代复用。
type discardWriter struct {
	header http.Header
	status int
	size   int
}

// newDiscardWriter 创建一个可复用的丢弃型写入器。
func newDiscardWriter() *discardWriter {
	return &discardWriter{header: make(http.Header, 8)}
}

// Header 返回响应头集合。
func (own *discardWriter) Header() http.Header { return own.header }

// WriteHeader 记录首个状态码，语义与 net/http 一致。
func (own *discardWriter) WriteHeader(status int) {
	if own.status == 0 {
		own.status = status
	}
}

// Write 丢弃响应体，只累计字节数。
func (own *discardWriter) Write(data []byte) (int, error) {
	if own.status == 0 {
		own.status = http.StatusOK
	}

	own.size += len(data)
	return len(data), nil
}

// Flush 满足 http.Flusher；丢弃型写入器无需真正刷新。
func (own *discardWriter) Flush() {}

// Hijack 满足 http.Hijacker；基准不涉及协议升级，直接返回不支持。
func (own *discardWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

// reset 清空状态，供下一次迭代复用。
func (own *discardWriter) reset() {
	if len(own.header) > 0 {
		clear(own.header)
	}

	own.status = 0
	own.size = 0
}

// newTestEngine 构造关闭启动日志的引擎，注册路由后冻结。
//
// 冻结与真实服务一致：Prepare 之后的请求路径不再持有注册期锁。
func newTestEngine(register func(*leego.Engine)) *leego.Engine {
	engine := leego.NewEngine(leego.DisableStartupLog())
	register(engine)
	engine.Prepare()
	return engine
}

// newRequest 构造一条可跨迭代复用的 GET 请求。
func newRequest(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, nil)
}

// benchRequest 测量引擎处理固定请求的开销。
func benchRequest(b *testing.B, engine *leego.Engine, request *http.Request) {
	b.Helper()

	writer := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		writer.reset()
		engine.ServeHTTP(writer, request)
	}
}

// benchRequestEach 测量需要每次新建请求的开销（例如带请求体）。
//
// build 里只做请求构造，不包含被测逻辑；请求构造本身的分配会计入结果，
// 因此这类基准用于比较同一构造方式下的相对差异。
func benchRequestEach(b *testing.B, engine *leego.Engine, build func() *http.Request) {
	b.Helper()

	writer := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		writer.reset()
		engine.ServeHTTP(writer, build())
	}
}

// pingHandle 是最小的响应处理器，用于隔离路由与中间件开销。
func pingHandle(ctx *leego.Context) {
	_ = ctx.ResponseText(http.StatusOK, "pong")
}

// noopMiddleware 是不做任何事的一层中间件，用于测量链式调用本身。
func noopMiddleware(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) { next(ctx) }
}
