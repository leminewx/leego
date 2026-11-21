package upgrader

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testWebSocketKey 为 RFC 6455 示例中的合法握手密钥（16 字节随机数的 Base64）。
const testWebSocketKey = "dGhlIHNhbXBsZSBub25jZQ=="

/*
testResponseWriter 是 response.Writer 的测试实现。

它把响应写入内存中的 fakeConn，并让 Hijack 返回同一个连接与缓冲读写器，
从而在无网络的情况下完整验证握手流程。
*/
type testResponseWriter struct {
	// header 为响应头集合。
	header http.Header
	// status 为已写入的状态码。
	status int
	// size 为已写入的响应体字节数。
	size int64
	// conn 为劫持后返回的连接。
	conn net.Conn
	// reader 为缓冲读取器。
	reader *bufio.Reader
	// writer 为缓冲写入器。
	writer *bufio.Writer
	// hijackErr 非 nil 时 Hijack 直接返回该错误。
	hijackErr error
}

// Header 返回响应头集合。
func (own *testResponseWriter) Header() http.Header {
	return own.header
}

// Write 写入响应体并累计字节数。
func (own *testResponseWriter) Write(data []byte) (int, error) {
	written, err := own.writer.Write(data)
	own.size += int64(written)
	return written, err
}

// WriteHeader 记录首次写入的状态码。
func (own *testResponseWriter) WriteHeader(code int) {
	if own.status == 0 {
		own.status = code
	}
}

// GetStatusCode 返回已写入的状态码。
func (own *testResponseWriter) GetStatusCode() int {
	return own.status
}

// GetSize 返回已写入的响应体字节数。
func (own *testResponseWriter) GetSize() int64 {
	return own.size
}

// Flush 刷新缓冲。
func (own *testResponseWriter) Flush() {
	_ = own.writer.Flush()
}

// Hijack 返回底层连接与缓冲读写器。
func (own *testResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if own.hijackErr != nil {
		return nil, nil, own.hijackErr
	}

	if own.conn == nil {
		return nil, nil, errors.New("hijack is not supported")
	}

	return own.conn, bufio.NewReadWriter(own.reader, own.writer), nil
}

// Unwrap 返回被包装的原始写入器；测试实现不包装，返回 nil。
func (own *testResponseWriter) Unwrap() http.ResponseWriter {
	return nil
}

// createTestResponseWriter 创建响应写入器，并把 input 预置为对端待发送数据。
func createTestResponseWriter(input []byte) (*testResponseWriter, *fakeConn) {
	raw := &fakeConn{input: bytes.NewReader(input)}

	return &testResponseWriter{
		header: make(http.Header),
		conn:   raw,
		reader: bufio.NewReader(raw),
		writer: bufio.NewWriter(raw),
	}, raw
}

// createHandshakeRequest 构造一个合法的握手请求，便于按需修改。
func createHandshakeRequest(t *testing.T) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", testWebSocketKey)

	return request
}

// TestUpgradeBuildsHandshakeAndReusesBufferedReader 验证升级流程与缓冲复用。
//
// 客户端可能在收到 101 后立刻发送数据；这些字节可能已被劫持前的缓冲读取器读入，
// 因此升级器必须复用同一个 reader，否则首帧会丢失。
func TestUpgradeBuildsHandshakeAndReusesBufferedReader(t *testing.T) {
	input := encodeFrame(frameSpec{
		fin:     true,
		masked:  true,
		opCode:  OP_CODE_TEXT,
		payload: []byte("hello"),
	})

	resp, raw := createTestResponseWriter(input)
	instance := New(nil)

	conn, err := instance.Upgrade(createHandshakeRequest(t), resp, nil)
	if err != nil {
		t.Fatalf("升级失败: %v", err)
	}

	handshake := raw.written.String()
	expectedLines := []string{
		"HTTP/1.1 101 Switching Protocols\r\n",
		"Upgrade: websocket\r\n",
		"Connection: Upgrade\r\n",
		"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n",
	}

	for _, line := range expectedLines {
		if !strings.Contains(handshake, line) {
			t.Fatalf("握手响应缺少 %q：\n%s", line, handshake)
		}
	}

	if !strings.HasSuffix(handshake, "\r\n\r\n") {
		t.Fatalf("握手响应必须以空行结束：%q", handshake)
	}

	opCode, payload, err := conn.Read()
	if err != nil {
		t.Fatalf("读取升级后首帧失败: %v", err)
	}

	if opCode != OP_CODE_TEXT || string(payload) != "hello" {
		t.Fatalf("首帧 = (%#x, %q), want (OP_CODE_TEXT, hello)", byte(opCode), payload)
	}
}

// TestUpgradeRejectsNilArguments 验证空参数被拒绝。
func TestUpgradeRejectsNilArguments(t *testing.T) {
	resp, _ := createTestResponseWriter(nil)
	instance := New(nil)

	if _, err := instance.Upgrade(nil, resp, nil); err == nil {
		t.Fatal("nil 请求必须返回错误")
	}

	if _, err := instance.Upgrade(createHandshakeRequest(t), nil, nil); err == nil {
		t.Fatal("nil 响应写入器必须返回错误")
	}
}

// TestUpgradeRejectsInvalidRequest 验证非法握手不会触发劫持。
func TestUpgradeRejectsInvalidRequest(t *testing.T) {
	request := createHandshakeRequest(t)
	request.Method = http.MethodPost

	resp, raw := createTestResponseWriter(nil)

	if _, err := New(nil).Upgrade(request, resp, nil); err == nil {
		t.Fatal("非法握手必须返回错误")
	}

	if raw.written.Len() != 0 {
		t.Fatalf("校验失败时不应写出握手响应，实际 %d 字节", raw.written.Len())
	}
}

// TestUpgradeRejectsDisallowedOrigin 验证来源校验失败时拒绝升级。
func TestUpgradeRejectsDisallowedOrigin(t *testing.T) {
	config := NewDefaultConfig()
	config.OriginCheckFunc = func(*http.Request) bool { return false }

	resp, raw := createTestResponseWriter(nil)

	if _, err := New(config).Upgrade(createHandshakeRequest(t), resp, nil); err == nil {
		t.Fatal("来源被拒绝时必须返回错误")
	}

	if raw.written.Len() != 0 {
		t.Fatalf("拒绝来源时不应写出握手响应，实际 %d 字节", raw.written.Len())
	}
}

// TestUpgradeFailsWhenHijackFails 验证底层不支持劫持时返回错误。
func TestUpgradeFailsWhenHijackFails(t *testing.T) {
	resp, _ := createTestResponseWriter(nil)
	resp.hijackErr = errors.New("hijack unsupported")

	_, err := New(nil).Upgrade(createHandshakeRequest(t), resp, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to hijack") {
		t.Fatalf("err = %v, want failed to hijack", err)
	}
}

// TestUpgradeAppliesConfig 验证超时与消息上限被传递到连接。
func TestUpgradeAppliesConfig(t *testing.T) {
	config := &Config{
		ReadTimeout:  time.Second,
		WriteTimeout: 2 * time.Second,
		MaxReadLimit: 32,
	}

	resp, _ := createTestResponseWriter(nil)

	conn, err := New(config).Upgrade(createHandshakeRequest(t), resp, nil)
	if err != nil {
		t.Fatalf("升级失败: %v", err)
	}

	if conn.readTimeout != time.Second {
		t.Fatalf("readTimeout = %v, want 1s", conn.readTimeout)
	}

	if conn.writeTimeout != 2*time.Second {
		t.Fatalf("writeTimeout = %v, want 2s", conn.writeTimeout)
	}

	if conn.GetMaxReadLimit() != 32 {
		t.Fatalf("GetMaxReadLimit() = %d, want 32", conn.GetMaxReadLimit())
	}
}

// TestUpgradeNegotiatesSubprotocol 验证子协议按客户端顺序命中允许列表。
func TestUpgradeNegotiatesSubprotocol(t *testing.T) {
	config := NewDefaultConfig()
	config.Subprotocols = []string{"chat", "superchat"}

	request := createHandshakeRequest(t)
	request.Header.Set("Sec-WebSocket-Protocol", "unknown, superchat")

	resp, raw := createTestResponseWriter(nil)

	if _, err := New(config).Upgrade(request, resp, nil); err != nil {
		t.Fatalf("升级失败: %v", err)
	}

	handshake := raw.written.String()
	if !strings.Contains(handshake, "Sec-WebSocket-Protocol: superchat\r\n") {
		t.Fatalf("握手响应未协商 superchat：\n%s", handshake)
	}
}

// TestUpgradeSkipsReservedHeaders 验证自定义响应头不能覆盖握手必需头。
func TestUpgradeSkipsReservedHeaders(t *testing.T) {
	resp, raw := createTestResponseWriter(nil)

	respHeader := http.Header{
		"X-Trace":              {"abc"},
		"Upgrade":              {"h2c"},
		"Connection":           {"close"},
		"Sec-Websocket-Accept": {"forged"},
	}

	if _, err := New(nil).Upgrade(createHandshakeRequest(t), resp, respHeader); err != nil {
		t.Fatalf("升级失败: %v", err)
	}

	handshake := raw.written.String()

	if !strings.Contains(handshake, "X-Trace: abc\r\n") {
		t.Fatalf("自定义响应头缺失：\n%s", handshake)
	}

	if strings.Contains(handshake, "h2c") || strings.Contains(handshake, "forged") || strings.Contains(handshake, "Connection: close") {
		t.Fatalf("保留响应头被覆盖：\n%s", handshake)
	}

	if count := strings.Count(handshake, "Upgrade: "); count != 1 {
		t.Fatalf("Upgrade 头数量 = %d, want 1", count)
	}
}

// TestValidateRequest 验证握手请求的各项校验规则。
func TestValidateRequest(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*http.Request)
		originCheck func(*http.Request) bool
		wantErr     string
	}{
		{name: "合法请求", mutate: func(*http.Request) {}},
		{name: "非 GET 方法", mutate: func(req *http.Request) { req.Method = http.MethodPost }, wantErr: "method must be GET"},
		{name: "Upgrade 缺失", mutate: func(req *http.Request) { req.Header.Del("Upgrade") }, wantErr: "header 'Upgrade'"},
		{name: "Upgrade 错误", mutate: func(req *http.Request) { req.Header.Set("Upgrade", "h2c") }, wantErr: "header 'Upgrade'"},
		{name: "Connection 缺失", mutate: func(req *http.Request) { req.Header.Del("Connection") }, wantErr: "header 'Connection'"},
		{name: "Connection 含多个令牌", mutate: func(req *http.Request) { req.Header.Set("Connection", "keep-alive, Upgrade") }},
		{name: "版本错误", mutate: func(req *http.Request) { req.Header.Set("Sec-WebSocket-Version", "12") }, wantErr: "unsupported version"},
		{name: "版本缺失", mutate: func(req *http.Request) { req.Header.Del("Sec-WebSocket-Version") }, wantErr: "unsupported version"},
		{name: "Key 缺失", mutate: func(req *http.Request) { req.Header.Del("Sec-WebSocket-Key") }, wantErr: "missing header 'Sec-WebSocket-Key'"},
		{name: "Key 非 Base64", mutate: func(req *http.Request) { req.Header.Set("Sec-WebSocket-Key", "not-base64!") }, wantErr: "invalid header 'Sec-WebSocket-Key'"},
		{name: "Key 长度错误", mutate: func(req *http.Request) {
			req.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString([]byte("short")))
		}, wantErr: "invalid header 'Sec-WebSocket-Key'"},
		{name: "来源被拒绝", originCheck: func(*http.Request) bool { return false }, wantErr: "origin not allowed"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := NewDefaultConfig()
			if testCase.originCheck != nil {
				config.OriginCheckFunc = testCase.originCheck
			}

			request := createHandshakeRequest(t)
			if testCase.mutate != nil {
				testCase.mutate(request)
			}

			err := New(config).validateRequest(request)

			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("err = %v, want 包含 %q", err, testCase.wantErr)
			}
		})
	}
}

// TestNegotiateSubprotocol 验证子协议协商的边界情况。
func TestNegotiateSubprotocol(t *testing.T) {
	cases := []struct {
		name         string
		subprotocols []string
		header       string
		want         string
	}{
		{name: "未配置允许列表", subprotocols: nil, header: "chat", want: ""},
		{name: "客户端未声明", subprotocols: []string{"chat"}, header: "", want: ""},
		{name: "首个候选命中", subprotocols: []string{"chat"}, header: "chat", want: "chat"},
		{name: "跳过未允许项", subprotocols: []string{"chat"}, header: "unknown, chat", want: "chat"},
		{name: "全部未命中", subprotocols: []string{"chat"}, header: "unknown, other", want: ""},
		{name: "忽略空白项", subprotocols: []string{"chat"}, header: " , chat , ", want: "chat"},
		{name: "大小写敏感", subprotocols: []string{"chat"}, header: "Chat", want: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instance := New(&Config{Subprotocols: testCase.subprotocols})

			request := createHandshakeRequest(t)
			if testCase.header != "" {
				request.Header.Set("Sec-WebSocket-Protocol", testCase.header)
			}

			if got := instance.negotiateSubprotocol(request); got != testCase.want {
				t.Fatalf("negotiateSubprotocol() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestBuildHandshakeResponse 验证握手响应报文的结构。
func TestBuildHandshakeResponse(t *testing.T) {
	request := createHandshakeRequest(t)

	response := buildHandshakeResponse(request, nil, "")
	if strings.Contains(response, "Sec-WebSocket-Protocol") {
		t.Fatalf("未协商子协议时不应输出该响应头：\n%s", response)
	}

	if !strings.HasSuffix(response, "\r\n\r\n") {
		t.Fatalf("响应必须以空行结束：%q", response)
	}

	response = buildHandshakeResponse(request, nil, "chat")
	if !strings.Contains(response, "Sec-WebSocket-Protocol: chat\r\n") {
		t.Fatalf("缺少子协议响应头：\n%s", response)
	}
}

// TestBuildHandshakeResponseRejectsInjectedHeaders 验证手写报文不会发生响应头注入。
//
// 握手响应是裸字节拼接，绕过了 net/http 的头部校验；
// 若不对头名与头值做 token/CRLF 校验，攻击者可通过 Header 注入额外响应。
func TestBuildHandshakeResponseRejectsInjectedHeaders(t *testing.T) {
	request := createHandshakeRequest(t)

	header := http.Header{
		"X-Safe":   {"ok"},
		"X-Inject": {"value\r\nInjected: 1"},
		"Bad Name": {"value"},
	}

	response := buildHandshakeResponse(request, header, "")

	if !strings.Contains(response, "X-Safe: ok\r\n") {
		t.Fatalf("合法响应头丢失：\n%s", response)
	}

	if strings.Contains(response, "Injected: 1") {
		t.Fatalf("CRLF 注入未被拦截：\n%s", response)
	}

	if strings.Contains(response, "Bad Name") {
		t.Fatalf("非法头名未被拦截：\n%s", response)
	}
}

// TestIsValidHeaderField 验证头名校验的边界。
func TestIsValidHeaderField(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  bool
	}{
		{name: "合法 token", key: "X-Trace", value: "abc", want: true},
		{name: "空头名", key: "", value: "abc", want: false},
		{name: "头名含空格", key: "X Trace", value: "abc", want: false},
		{name: "值含换行", key: "X-Trace", value: "a\nb", want: false},
		{name: "值含回车", key: "X-Trace", value: "a\rb", want: false},
		{name: "值含 NUL", key: "X-Trace", value: "a\x00b", want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isValidHeaderField(testCase.key, testCase.value); got != testCase.want {
				t.Fatalf("isValidHeaderField(%q, %q) = %v, want %v", testCase.key, testCase.value, got, testCase.want)
			}
		})
	}
}

// TestComputeAcceptKey 验证 Sec-WebSocket-Accept 计算与 RFC 6455 示例一致。
func TestComputeAcceptKey(t *testing.T) {
	const expected = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="

	if got := computeAcceptKey(testWebSocketKey); got != expected {
		t.Fatalf("computeAcceptKey() = %q, want %q", got, expected)
	}
}

// TestNewKeepsOriginCheckNil 验证显式 nil 的来源校验函数保持 nil。
//
// nil 表示不做来源校验（与默认配置返回 true 等价），此处仅锁定复制语义。
func TestNewKeepsOriginCheckNil(t *testing.T) {
	instance := New(&Config{MaxReadLimit: 1024})

	if instance.OriginCheckFunc != nil {
		t.Fatal("OriginCheckFunc 应保持 nil")
	}
}
