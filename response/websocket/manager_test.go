package websocket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

// upgradeRequestKey 为 RFC 6455 示例中的合法握手密钥。
const upgradeRequestKey = "dGhlIHNhbXBsZSBub25jZQ=="

/*
fakeConn 是 net.Conn 的内存实现。

它让 WebSocket 层可以在完全没有网络的情况下完成握手、收发帧与关闭，
其中：
  - input 为“对端”会返回的字节流，为 nil 时读取立即返回 io.EOF；
  - written 为“本端”写出的全部字节，用于断言握手与业务帧内容。
*/
type fakeConn struct {
	// mutex 保护 written，避免写协程与测试协程并发访问。
	mutex sync.Mutex
	// written 保存本端写出的全部字节。
	written bytes.Buffer
	// input 为对端待发送的数据。
	input io.Reader
	// isClosed 表示连接是否已关闭。
	isClosed atomic.Bool
}

// Read 从输入流读取数据，输入为空时返回 io.EOF。
func (own *fakeConn) Read(buffer []byte) (int, error) {
	if own.input == nil {
		return 0, io.EOF
	}

	return own.input.Read(buffer)
}

// Write 把数据追加到内存缓冲区，始终成功以简化断言。
func (own *fakeConn) Write(data []byte) (int, error) {
	own.mutex.Lock()
	defer own.mutex.Unlock()

	return own.written.Write(data)
}

// Close 标记连接已关闭，并关闭输入流（若可关闭）。
func (own *fakeConn) Close() error {
	if !own.isClosed.CompareAndSwap(false, true) {
		return nil
	}

	if closer, isCloser := own.input.(io.Closer); isCloser {
		return closer.Close()
	}

	return nil
}

// LocalAddr 返回本端地址；测试不关心真实地址。
func (own *fakeConn) LocalAddr() net.Addr {
	return &net.TCPAddr{}
}

// RemoteAddr 返回对端地址；测试不关心真实地址。
func (own *fakeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{}
}

// SetDeadline 设置读写截止时间；内存连接直接忽略。
func (own *fakeConn) SetDeadline(deadline time.Time) error {
	return nil
}

// SetReadDeadline 设置读截止时间；内存连接直接忽略。
func (own *fakeConn) SetReadDeadline(deadline time.Time) error {
	return nil
}

// SetWriteDeadline 设置写截止时间；内存连接直接忽略。
func (own *fakeConn) SetWriteDeadline(deadline time.Time) error {
	return nil
}

// Snapshot 返回当前已写出字节的副本。
func (own *fakeConn) Snapshot() []byte {
	own.mutex.Lock()
	defer own.mutex.Unlock()

	return append([]byte(nil), own.written.Bytes()...)
}

/*
testResponseWriter 是 response.Writer 的测试实现。

Hijack 会把内存连接与缓冲读写器交给升级器，
从而完整走通“握手响应写回 + 复用缓冲读取器”的真实流程。
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
	if own.writer != nil {
		_ = own.writer.Flush()
	}
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

// createTestWriter 创建响应写入器，并返回随之创建的底层内存连接。
func createTestWriter(input io.Reader) (*testResponseWriter, *fakeConn) {
	raw := &fakeConn{input: input}

	return &testResponseWriter{
		header: make(http.Header),
		conn:   raw,
		reader: bufio.NewReader(raw),
		writer: bufio.NewWriter(raw),
	}, raw
}

// createUpgradeRequest 构造一个合法的握手请求。
func createUpgradeRequest() *http.Request {
	request, err := http.NewRequest(http.MethodGet, "http://example.com/ws", nil)
	if err != nil {
		panic(err)
	}

	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", upgradeRequestKey)

	return request
}

// encodeClientFrame 编码一个带掩码的客户端帧。
//
// 客户端帧必须掩码（RFC 6455），这里固定使用 0x01020304 作为掩码键，
// 使测试结果完全可重复。
func encodeClientFrame(opCode byte, payload []byte) []byte {
	maskKey := [4]byte{0x01, 0x02, 0x03, 0x04}

	frameBytes := make([]byte, 0, len(payload)+14)
	frameBytes = append(frameBytes, 0x80|opCode)

	switch {
	case len(payload) <= 125:
		frameBytes = append(frameBytes, 0x80|byte(len(payload)))

	case len(payload) <= 65535:
		frameBytes = append(frameBytes, 0x80|126, byte(len(payload)>>8), byte(len(payload)))

	default:
		frameBytes = append(frameBytes, 0x80|127)
		length := uint64(len(payload))
		for shift := 56; shift >= 0; shift -= 8 {
			frameBytes = append(frameBytes, byte(length>>uint(shift)))
		}
	}

	frameBytes = append(frameBytes, maskKey[:]...)
	for index, value := range payload {
		frameBytes = append(frameBytes, value^maskKey[index%4])
	}

	return frameBytes
}

// waitForCondition 在超时时间内轮询条件，命中返回 true。
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}

		time.Sleep(time.Millisecond)
	}

	return condition()
}

// readQueuedMessage 从客户端发送队列取出一条消息并反序列化。
func readQueuedMessage(t *testing.T, client *Client) *Message {
	t.Helper()

	select {
	case payload := <-client.SendChan:
		var message Message
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("消息反序列化失败: %v", err)
		}

		return &message

	case <-time.After(time.Second):
		t.Fatal("等待消息超时")
		return nil
	}
}

// TestNewManagerSetsDefaults 验证管理器的默认状态。
func TestNewManagerSetsDefaults(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	if manager.IsClosed() {
		t.Fatal("新建管理器不应处于关闭状态")
	}

	if got := manager.GetPingInterval(); got != _DEFAULT_PING_INTERVAL {
		t.Fatalf("心跳间隔 = %v, want %v", got, _DEFAULT_PING_INTERVAL)
	}

	if manager.CountConnections() != 0 {
		t.Fatalf("在线连接数 = %d, want 0", manager.CountConnections())
	}

	if manager.Done() == nil {
		t.Fatal("Done 不应返回 nil 通道")
	}

	select {
	case <-manager.Done():
		t.Fatal("Done 在关闭前不应就绪")
	default:
	}
}

// TestNewManagerCopiesConfig 验证配置被复制并生效。
func TestNewManagerCopiesConfig(t *testing.T) {
	config := &upgrader.Config{MaxReadLimit: 1024}
	manager := NewManager(config)
	defer manager.Close()

	config.MaxReadLimit = 1 << 20

	if got := manager.upgrader.MaxReadLimit; got != 1024 {
		t.Fatalf("MaxReadLimit = %d, want 1024", got)
	}
}

// TestManagerSetPingInterval 验证心跳间隔的读写与非正数忽略。
func TestManagerSetPingInterval(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	manager.SetPingInterval(5 * time.Second)
	if got := manager.GetPingInterval(); got != 5*time.Second {
		t.Fatalf("心跳间隔 = %v, want 5s", got)
	}

	manager.SetPingInterval(0)
	if got := manager.GetPingInterval(); got != 5*time.Second {
		t.Fatalf("非正数不应改变心跳间隔，当前为 %v", got)
	}

	manager.SetPingInterval(-time.Second)
	if got := manager.GetPingInterval(); got != 5*time.Second {
		t.Fatalf("负数不应改变心跳间隔，当前为 %v", got)
	}
}

// TestManagerServeRejectsClosedManager 验证关闭后拒绝新连接。
func TestManagerServeRejectsClosedManager(t *testing.T) {
	manager := NewManager(nil)
	if err := manager.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	writer, _ := createTestWriter(nil)
	if err := manager.Serve(writer, createUpgradeRequest()); !errors.Is(err, ErrManagerIsClosed) {
		t.Fatalf("Serve = %v, want ErrManagerIsClosed", err)
	}
}

// TestManagerServeReportsHijackFailure 验证劫持失败时返回错误且不登记连接。
func TestManagerServeReportsHijackFailure(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	writer := &testResponseWriter{
		header:    make(http.Header),
		hijackErr: errors.New("hijack is not supported"),
	}

	err := manager.Serve(writer, createUpgradeRequest())
	if err == nil || !strings.Contains(err.Error(), "failed to hijack") {
		t.Fatalf("Serve = %v, want 劫持错误", err)
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("在线连接数 = %d, want 0", count)
	}
}

/*
TestManagerServeHandshakeWelcomeAndCleanup 验证完整的连接生命周期。

流程按真实时序推进：
 1. 服务端写出 101 握手响应；
 2. 写协程写出欢迎消息；
 3. 客户端发送关闭帧；
 4. 读循环结束，连接被注销并关闭。

这里使用 io.Pipe 精确控制对端发送时机，避免依赖 sleep 造成的偶发失败。
*/
func TestManagerServeHandshakeWelcomeAndCleanup(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	pipeReader, pipeWriter := io.Pipe()
	writer, rawConn := createTestWriter(pipeReader)
	defer pipeWriter.Close()

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- manager.Serve(writer, createUpgradeRequest())
	}()

	if !waitForCondition(t, 2*time.Second, func() bool {
		return bytes.Contains(rawConn.Snapshot(), []byte("HTTP/1.1 101 Switching Protocols"))
	}) {
		t.Fatal("未写出 101 握手响应")
	}

	if !waitForCondition(t, 2*time.Second, func() bool {
		return bytes.Contains(rawConn.Snapshot(), []byte(`"type":"connected"`))
	}) {
		t.Fatalf("未写出欢迎消息，实际输出: %s", rawConn.Snapshot())
	}

	closePayload := append([]byte{0x03, 0xE8}, []byte("bye")...)
	if _, err := pipeWriter.Write(encodeClientFrame(byte(upgrader.OP_CODE_CLOSE), closePayload)); err != nil {
		t.Fatalf("写入关闭帧失败: %v", err)
	}

	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("Serve 返回错误: %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Serve 未在超时内返回")
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("连接结束后在线数 = %d, want 0", count)
	}

	if !rawConn.isClosed.Load() {
		t.Fatal("连接结束后底层连接应已关闭")
	}

	if !bytes.Contains(rawConn.Snapshot(), []byte("HTTP/1.1")) {
		t.Fatal("握手响应应已写出")
	}
}

// TestManagerServeSendsHeartbeatPing 验证写协程按间隔发送 PING 控制帧。
//
// 心跳直接写控制帧而不占用发送队列，因此即使业务消息积压，
// 连接保活也不会被慢消费者拖累。这里通过缩短心跳间隔观察真实输出。
func TestManagerServeSendsHeartbeatPing(t *testing.T) {
	manager := NewManager(nil)
	manager.SetPingInterval(10 * time.Millisecond)
	defer manager.Close()

	pipeReader, pipeWriter := io.Pipe()
	writer, rawConn := createTestWriter(pipeReader)
	defer pipeWriter.Close()

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- manager.Serve(writer, createUpgradeRequest())
	}()

	if !waitForCondition(t, 2*time.Second, func() bool {
		return bytes.Contains(rawConn.Snapshot(), []byte{0x89, 0x00})
	}) {
		t.Fatalf("未收到 PING 心跳帧，实际输出: %v", rawConn.Snapshot())
	}

	_ = pipeWriter.Close()

	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("Serve 返回错误: %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Serve 未在超时内返回")
	}
}

// TestManagerServeIgnoresBinaryFrames 验证二进制帧被忽略但不影响连接。
func TestManagerServeIgnoresBinaryFrames(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	pipeReader, pipeWriter := io.Pipe()
	writer, rawConn := createTestWriter(pipeReader)
	defer pipeWriter.Close()

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- manager.Serve(writer, createUpgradeRequest())
	}()

	if !waitForCondition(t, 2*time.Second, func() bool {
		return bytes.Contains(rawConn.Snapshot(), []byte("HTTP/1.1 101 Switching Protocols"))
	}) {
		t.Fatal("未写出 101 握手响应")
	}

	if _, err := pipeWriter.Write(encodeClientFrame(byte(upgrader.OP_CODE_BINARY), []byte{0x01, 0x02, 0x03})); err != nil {
		t.Fatalf("写入二进制帧失败: %v", err)
	}

	if _, err := pipeWriter.Write(encodeClientFrame(byte(upgrader.OP_CODE_CLOSE), []byte{0x03, 0xE8})); err != nil {
		t.Fatalf("写入关闭帧失败: %v", err)
	}

	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("Serve 返回错误: %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Serve 未在超时内返回")
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("在线连接数 = %d, want 0", count)
	}
}

// TestManagerBroadcastWithoutClients 验证空广播不返回错误。
func TestManagerBroadcastWithoutClients(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	if err := manager.Broadcast(MSG_TYPE_BROADCAST, "hello"); err != nil {
		t.Fatalf("Broadcast = %v, want nil", err)
	}
}

// TestManagerBroadcastDeliversToAllClients 验证广播投递给全部客户端。
func TestManagerBroadcastDeliversToAllClients(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	first := NewClient("client-1", "", nil)
	second := NewClient("client-2", "", nil)
	manager.clients.Store(first.Id, first)
	manager.clients.Store(second.Id, second)

	if err := manager.Broadcast(MSG_TYPE_BROADCAST, "hello"); err != nil {
		t.Fatalf("Broadcast = %v, want nil", err)
	}

	for _, client := range []*Client{first, second} {
		message := readQueuedMessage(t, client)
		if message.Type != MSG_TYPE_BROADCAST {
			t.Fatalf("%s 消息类型 = %q, want broadcast", client.Id, message.Type)
		}

		if message.Data != "hello" {
			t.Fatalf("%s 消息载荷 = %v, want hello", client.Id, message.Data)
		}

		if message.Timestamp <= 0 {
			t.Fatalf("%s 消息时间戳 = %d, want 正数", client.Id, message.Timestamp)
		}
	}
}

// TestManagerBroadcastRemovesClosedClients 验证广播会剔除已关闭客户端。
func TestManagerBroadcastRemovesClosedClients(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.clients.Store(client.Id, client)
	_ = client.Close()

	if err := manager.Broadcast(MSG_TYPE_BROADCAST, "hello"); err != nil {
		t.Fatalf("Broadcast = %v, want nil", err)
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("在线连接数 = %d, want 0", count)
	}
}

// TestManagerBroadcastKeepsSlowClients 验证队列满的客户端被保留（仅丢消息）。
func TestManagerBroadcastKeepsSlowClients(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	for index := 0; index < _DEFAULT_SEND_BUFFER_SIZE; index++ {
		_ = client.WriteMessage([]byte("x"))
	}
	manager.clients.Store(client.Id, client)

	if err := manager.Broadcast(MSG_TYPE_BROADCAST, "hello"); err != nil {
		t.Fatalf("Broadcast = %v, want nil", err)
	}

	if count := manager.CountConnections(); count != 1 {
		t.Fatalf("在线连接数 = %d, want 1", count)
	}
}

// TestManagerBroadcastAfterClose 验证关闭后广播返回 ErrManagerShuttingDown。
func TestManagerBroadcastAfterClose(t *testing.T) {
	manager := NewManager(nil)
	_ = manager.Close()

	if err := manager.Broadcast(MSG_TYPE_BROADCAST, "hello"); !errors.Is(err, ErrManagerShuttingDown) {
		t.Fatalf("Broadcast = %v, want ErrManagerShuttingDown", err)
	}
}

// TestManagerSendToClient 覆盖定向发送的成功与失败分支。
func TestManagerSendToClient(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.clients.Store(client.Id, client)

	if err := manager.SendToClient("client-1", MSG_TYPE_PRIVATE, "hi"); err != nil {
		t.Fatalf("SendToClient = %v, want nil", err)
	}

	message := readQueuedMessage(t, client)
	if message.Type != MSG_TYPE_PRIVATE || message.Data != "hi" {
		t.Fatalf("消息 = %+v", message)
	}

	err := manager.SendToClient("missing", MSG_TYPE_PRIVATE, "hi")
	if err == nil || !strings.Contains(err.Error(), "client not found") {
		t.Fatalf("SendToClient = %v, want 客户端不存在", err)
	}
}

// TestManagerSendToClientRemovesClosedClient 验证向已关闭客户端发送会清理登记。
func TestManagerSendToClientRemovesClosedClient(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.clients.Store(client.Id, client)
	_ = client.Close()

	err := manager.SendToClient("client-1", MSG_TYPE_PRIVATE, "hi")
	if err == nil || !strings.Contains(err.Error(), "client not found") {
		t.Fatalf("SendToClient = %v, want 客户端不存在", err)
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("在线连接数 = %d, want 0", count)
	}
}

// TestManagerSendToClientAfterClose 验证关闭后定向发送返回 ErrManagerShuttingDown。
func TestManagerSendToClientAfterClose(t *testing.T) {
	manager := NewManager(nil)
	_ = manager.Close()

	if err := manager.SendToClient("client-1", MSG_TYPE_PRIVATE, "hi"); !errors.Is(err, ErrManagerShuttingDown) {
		t.Fatalf("SendToClient = %v, want ErrManagerShuttingDown", err)
	}
}

// TestManagerGetClientsAndSnapshots 验证客户端快照查询。
func TestManagerGetClientsAndSnapshots(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	first := NewClient("client-1", "user-1", nil)
	first.SetMetadata("role", "admin")
	second := NewClient("client-2", "", nil)
	manager.clients.Store(first.Id, first)
	manager.clients.Store(second.Id, second)

	if count := manager.CountConnections(); count != 2 {
		t.Fatalf("在线连接数 = %d, want 2", count)
	}

	if clients := manager.GetClients(); len(clients) != 2 {
		t.Fatalf("GetClients 长度 = %d, want 2", len(clients))
	}

	snapshot, isFound := manager.GetClient("client-1")
	if !isFound || snapshot.UserId != "user-1" || snapshot.Metadata["role"] != "admin" {
		t.Fatalf("GetClient = %+v, found=%v", snapshot, isFound)
	}

	if _, isFound := manager.GetClient("missing"); isFound {
		t.Fatal("不存在的客户端不应被找到")
	}
}

// TestManagerCloseClient 验证主动关闭并移除客户端。
func TestManagerCloseClient(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.clients.Store(client.Id, client)

	if err := manager.CloseClient("client-1"); err != nil {
		t.Fatalf("CloseClient = %v, want nil", err)
	}

	if !client.IsClosed() {
		t.Fatal("客户端应已关闭")
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("在线连接数 = %d, want 0", count)
	}

	if err := manager.CloseClient("client-1"); err == nil || !strings.Contains(err.Error(), "client not found") {
		t.Fatalf("重复关闭 = %v, want 客户端不存在", err)
	}
}

// TestManagerCloseNotifiesClientsAndIsIdempotent 验证关闭通知与幂等性。
func TestManagerCloseNotifiesClientsAndIsIdempotent(t *testing.T) {
	manager := NewManager(nil)

	client := NewClient("client-1", "", nil)
	manager.clients.Store(client.Id, client)

	if err := manager.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}

	if err := manager.Close(); err != nil {
		t.Fatalf("重复 Close = %v, want nil", err)
	}

	if !manager.IsClosed() {
		t.Fatal("关闭后 IsClosed 应为 true")
	}

	select {
	case <-manager.Done():
	default:
		t.Fatal("关闭后 Done 应已就绪")
	}

	if !client.IsClosed() {
		t.Fatal("管理器关闭后客户端应被关闭")
	}

	if count := manager.CountConnections(); count != 0 {
		t.Fatalf("在线连接数 = %d, want 0", count)
	}

	message := readQueuedMessage(t, client)
	if message.Type != MessageType("server_shutdown") {
		t.Fatalf("关闭通知类型 = %q, want server_shutdown", message.Type)
	}
}

// TestManagerStartStopsOnContextCancellation 验证 Start 随上下文结束并关闭管理器。
func TestManagerStartStopsOnContextCancellation(t *testing.T) {
	manager := NewManager(nil)

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})

	go func() {
		manager.Start(ctx)
		close(finished)
	}()

	cancel()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Start 未在上下文取消后返回")
	}

	if !manager.IsClosed() {
		t.Fatal("上下文取消后管理器应已关闭")
	}
}

// TestManagerStartReturnsWhenClosed 验证管理器关闭后 Start 立即返回。
func TestManagerStartReturnsWhenClosed(t *testing.T) {
	manager := NewManager(nil)
	_ = manager.Close()

	finished := make(chan struct{})
	go func() {
		manager.Start(nil)
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("管理器关闭后 Start 应立即返回")
	}
}

// TestGetUserIdPriority 验证用户 ID 字段的优先级与类型限制。
func TestGetUserIdPriority(t *testing.T) {
	testCases := []struct {
		name      string
		data      map[string]any
		wantKey   string
		wantValue string
	}{
		{name: "user_id 优先", data: map[string]any{"user_id": "a", "id": "b"}, wantKey: "user_id", wantValue: "a"},
		{name: "userid 次之", data: map[string]any{"userid": "b", "uid": "c"}, wantKey: "userid", wantValue: "b"},
		{name: "userId 大小写变体", data: map[string]any{"userId": "c"}, wantKey: "userId", wantValue: "c"},
		{name: "userID 大小写变体", data: map[string]any{"userID": "d"}, wantKey: "userID", wantValue: "d"},
		{name: "uid", data: map[string]any{"uid": "e"}, wantKey: "uid", wantValue: "e"},
		{name: "id 兜底", data: map[string]any{"id": "f"}, wantKey: "id", wantValue: "f"},
		{name: "非字符串被跳过", data: map[string]any{"user_id": 1, "uid": "g"}, wantKey: "uid", wantValue: "g"},
		{name: "空字符串被跳过", data: map[string]any{"user_id": "", "id": "h"}, wantKey: "id", wantValue: "h"},
		{name: "无命中字段", data: map[string]any{"name": "x"}},
		{name: "空对象", data: map[string]any{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			key, value := getUserId(testCase.data)
			if key != testCase.wantKey || value != testCase.wantValue {
				t.Fatalf("getUserId = (%q, %q), want (%q, %q)", key, value, testCase.wantKey, testCase.wantValue)
			}
		})
	}
}

// TestGenerateClientIdIsUnique 验证客户端 ID 在并发场景下也不重复。
func TestGenerateClientIdIsUnique(t *testing.T) {
	const workers, perWorker = 8, 50

	var mutex sync.Mutex
	seen := make(map[string]struct{}, workers*perWorker)

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			for index := 0; index < perWorker; index++ {
				clientId := generateClientId()

				mutex.Lock()
				seen[clientId] = struct{}{}
				mutex.Unlock()
			}
		}()
	}

	waitGroup.Wait()

	if len(seen) != workers*perWorker {
		t.Fatalf("唯一 ID 数 = %d, want %d", len(seen), workers*perWorker)
	}
}

// TestHandleMessageRejectsInvalidJSON 验证非法 JSON 的应答。
func TestHandleMessageRejectsInvalidJSON(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte("not json"))

	message := readQueuedMessage(t, client)
	if message.Type != MessageType("invalid_message_format") {
		t.Fatalf("消息类型 = %q, want invalid_message_format", message.Type)
	}
}

// TestHandleMessageAnswersPing 验证 PING 请求被应答为 PONG。
func TestHandleMessageAnswersPing(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"ping","data":"ping"}`))

	message := readQueuedMessage(t, client)
	if message.Type != MSG_TYPE_PONG {
		t.Fatalf("消息类型 = %q, want pong", message.Type)
	}
}

// TestHandleMessageBroadcasts 验证广播消息被转发给全部连接。
func TestHandleMessageBroadcasts(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.clients.Store(client.Id, client)
	manager.handleMessage(client, []byte(`{"type":"broadcast","data":"hello"}`))

	message := readQueuedMessage(t, client)
	if message.Type != MSG_TYPE_BROADCAST || message.Data != "hello" {
		t.Fatalf("消息 = %+v", message)
	}
}

// TestHandleMessagePrivateRequiresRecipient 验证缺少接收方的私聊被忽略。
func TestHandleMessagePrivateRequiresRecipient(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"private","data":"hi"}`))

	if length := len(client.SendChan); length != 0 {
		t.Fatalf("发送队列长度 = %d, want 0", length)
	}
}

// TestHandleMessagePrivateDeliversToRecipient 验证私聊投递与发送方标注。
func TestHandleMessagePrivateDeliversToRecipient(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	sender := NewClient("client-1", "", nil)
	receiver := NewClient("client-2", "", nil)
	manager.clients.Store(sender.Id, sender)
	manager.clients.Store(receiver.Id, receiver)

	manager.handleMessage(sender, []byte(`{"type":"private","to":"client-2","data":"hi"}`))

	message := readQueuedMessage(t, receiver)
	if message.Type != MSG_TYPE_PRIVATE {
		t.Fatalf("消息类型 = %q, want private", message.Type)
	}

	data, isMap := message.Data.(map[string]any)
	if !isMap || data["from"] != "client-1" || data["message"] != "hi" {
		t.Fatalf("消息载荷 = %v", message.Data)
	}

	if length := len(sender.SendChan); length != 0 {
		t.Fatalf("发送方队列长度 = %d, want 0", length)
	}
}

// TestHandleMessagePrivateIgnoresMissingRecipient 验证接收方不存在时仅记录日志。
func TestHandleMessagePrivateIgnoresMissingRecipient(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	sender := NewClient("client-1", "", nil)
	manager.handleMessage(sender, []byte(`{"type":"private","to":"missing","data":"hi"}`))

	if length := len(sender.SendChan); length != 0 {
		t.Fatalf("发送方队列长度 = %d, want 0", length)
	}
}

// TestHandleMessageIdentifyWithString 验证以字符串完成身份识别。
func TestHandleMessageIdentifyWithString(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"identify","data":"user-42"}`))

	if got := client.GetUserId(); got != "user-42" {
		t.Fatalf("UserId = %q, want user-42", got)
	}

	metadata := client.GetMetadata()
	if metadata["user_id"] != "user-42" {
		t.Fatalf("Metadata[user_id] = %v, want user-42", metadata["user_id"])
	}

	if _, isExisted := metadata["identified_at"]; !isExisted {
		t.Fatal("Metadata 应记录识别时间")
	}

	message := readQueuedMessage(t, client)
	if message.Type != MessageType("identification_success") {
		t.Fatalf("消息类型 = %q, want identification_success", message.Type)
	}
}

// TestHandleMessageIdentifyWithMap 验证以对象完成身份识别并保留额外元数据。
func TestHandleMessageIdentifyWithMap(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"identify","data":{"user_id":"user-42","name":"alice"}}`))

	if got := client.GetUserId(); got != "user-42" {
		t.Fatalf("UserId = %q, want user-42", got)
	}

	metadata := client.GetMetadata()
	if metadata["name"] != "alice" {
		t.Fatalf("Metadata[name] = %v, want alice", metadata["name"])
	}

	if metadata["user_id"] != "user-42" {
		t.Fatalf("Metadata[user_id] = %v, want user-42", metadata["user_id"])
	}

	message := readQueuedMessage(t, client)
	if message.Type != MessageType("identification_success") {
		t.Fatalf("消息类型 = %q, want identification_success", message.Type)
	}
}

// TestHandleMessageIdentifyRejectsMissingId 验证缺少用户 ID 时拒绝识别。
func TestHandleMessageIdentifyRejectsMissingId(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"identify","data":{"name":"alice"}}`))

	if got := client.GetUserId(); got != "" {
		t.Fatalf("UserId = %q, want 空", got)
	}

	if _, isExisted := client.GetMetadata()["user_id"]; isExisted {
		t.Fatal("识别失败时不应写入 user_id 元数据")
	}

	message := readQueuedMessage(t, client)
	if message.Type != MessageType("identification_failed") {
		t.Fatalf("消息类型 = %q, want identification_failed", message.Type)
	}
}

// TestHandleMessageIdentifyIgnoresUnknownDataType 验证非字符串/对象载荷被忽略。
func TestHandleMessageIdentifyIgnoresUnknownDataType(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"identify","data":42}`))

	if got := client.GetUserId(); got != "" {
		t.Fatalf("UserId = %q, want 空", got)
	}

	if length := len(client.SendChan); length != 0 {
		t.Fatalf("发送队列长度 = %d, want 0", length)
	}
}

// TestHandleMessageRejectsUnknownType 验证未知消息类型的应答。
func TestHandleMessageRejectsUnknownType(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()

	client := NewClient("client-1", "", nil)
	manager.handleMessage(client, []byte(`{"type":"foo","data":null}`))

	message := readQueuedMessage(t, client)
	if message.Type != MessageType("unknown_message_type") {
		t.Fatalf("消息类型 = %q, want unknown_message_type", message.Type)
	}

	data, isMap := message.Data.(map[string]any)
	if !isMap || !strings.Contains(data["message"].(string), "foo") {
		t.Fatalf("消息载荷 = %v", message.Data)
	}
}
