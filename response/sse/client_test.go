package sse

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gitee.com/leminewx/leego/response"
)

// syncRecorder 是并发安全的响应记录器。
//
// httptest.ResponseRecorder 不是并发安全的，而 serve 会在独立协程中写响应、
// 测试协程同时轮询内容；使用带锁的实现可以在启用 -race 时保持测试稳定。
type syncRecorder struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	status  int
	flushed bool
}

// createSyncRecorder 创建并发安全的响应记录器。
func createSyncRecorder() *syncRecorder {
	return &syncRecorder{header: make(http.Header)}
}

// Header 返回响应头集合。
func (own *syncRecorder) Header() http.Header {
	return own.header
}

// Write 写入响应体；首次写入时回填 200。
func (own *syncRecorder) Write(data []byte) (int, error) {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.status == 0 {
		own.status = http.StatusOK
	}

	return own.body.Write(data)
}

// WriteHeader 记录响应状态码（仅首次生效）。
func (own *syncRecorder) WriteHeader(status int) {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.status == 0 {
		own.status = status
	}
}

// Flush 标记已刷新。
func (own *syncRecorder) Flush() {
	own.mu.Lock()
	own.flushed = true
	own.mu.Unlock()
}

// Code 返回已写入的状态码。
func (own *syncRecorder) Code() int {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.status
}

// BodyString 返回已写入的响应体。
func (own *syncRecorder) BodyString() string {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.body.String()
}

// IsFlushed 返回是否已刷新。
func (own *syncRecorder) IsFlushed() bool {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.flushed
}

// createSafeResponse 创建绑定并发安全记录器的响应写入器。
func createSafeResponse() (response.Writer, *syncRecorder) {
	recorder := createSyncRecorder()
	return response.NewWriter(recorder), recorder
}

// waitForCondition 在超时时间内轮询条件，用于等待异步写协程产生输出。
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("等待条件超时")
}

// createServeRequest 创建带可取消上下文的测试请求。
func createServeRequest() (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	return req, cancel
}

// TestNewClientUsesDefaults 验证客户端默认参数。
func TestNewClientUsesDefaults(t *testing.T) {
	client := NewClient("client-1", 0)

	if client.Id != "client-1" {
		t.Fatalf("Id = %q, want client-1", client.Id)
	}
	if capacity := cap(client.messageChan); capacity != _DEFAULT_MESSAGE_CACHE_NUMBER {
		t.Fatalf("队列容量 = %d, want %d", capacity, _DEFAULT_MESSAGE_CACHE_NUMBER)
	}
	if timeout := client.GetWriteTimeout(); timeout != _DEFAULT_WRITE_TIMEOUT {
		t.Fatalf("写超时 = %v, want %v", timeout, _DEFAULT_WRITE_TIMEOUT)
	}
	if period := client.GetHeartbeatPeriod(); period != _DEFAULT_HEARTBEAT_PERIOD {
		t.Fatalf("心跳周期 = %v, want %v", period, _DEFAULT_HEARTBEAT_PERIOD)
	}
	if client.IsClosed() {
		t.Fatal("新建客户端不应处于关闭状态")
	}
}

// TestNewClientAcceptsCustomCapacity 验证自定义队列容量生效。
func TestNewClientAcceptsCustomCapacity(t *testing.T) {
	client := NewClient("client-1", 32)
	if capacity := cap(client.messageChan); capacity != 32 {
		t.Fatalf("队列容量 = %d, want 32", capacity)
	}
}

// TestSetWriteTimeoutIgnoresNonPositive 验证非正数写超时被忽略。
func TestSetWriteTimeoutIgnoresNonPositive(t *testing.T) {
	client := NewClient("client-1", 8)
	client.SetWriteTimeout(200 * time.Millisecond)

	if got := client.GetWriteTimeout(); got != 200*time.Millisecond {
		t.Fatalf("GetWriteTimeout() = %v, want 200ms", got)
	}

	client.SetWriteTimeout(0)
	client.SetWriteTimeout(-time.Second)
	if got := client.GetWriteTimeout(); got != 200*time.Millisecond {
		t.Fatalf("非正数不应修改配置，得到 %v", got)
	}
}

// TestSetHeartbeatPeriodIgnoresNonPositive 验证非正数心跳周期被忽略。
func TestSetHeartbeatPeriodIgnoresNonPositive(t *testing.T) {
	client := NewClient("client-1", 8)
	client.SetHeartbeatPeriod(time.Second)

	if got := client.GetHeartbeatPeriod(); got != time.Second {
		t.Fatalf("GetHeartbeatPeriod() = %v, want 1s", got)
	}

	client.SetHeartbeatPeriod(0)
	client.SetHeartbeatPeriod(-time.Second)
	if got := client.GetHeartbeatPeriod(); got != time.Second {
		t.Fatalf("非正数不应修改配置，得到 %v", got)
	}
}

// TestGetHeartbeatPeriodFallsBackToDefault 验证零值客户端的读取回退到默认周期。
func TestGetHeartbeatPeriodFallsBackToDefault(t *testing.T) {
	client := &Client{}
	if got := client.GetHeartbeatPeriod(); got != _DEFAULT_HEARTBEAT_PERIOD {
		t.Fatalf("零值心跳周期 = %v, want %v", got, _DEFAULT_HEARTBEAT_PERIOD)
	}
}

// TestCloseIsIdempotent 验证 Close 可被重复调用且状态一致。
func TestCloseIsIdempotent(t *testing.T) {
	client := NewClient("client-1", 8)

	if err := client.Close(); err != nil {
		t.Fatalf("首次关闭失败: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("重复关闭失败: %v", err)
	}

	if !client.IsClosed() {
		t.Fatal("关闭后 IsClosed 应为 true")
	}

	select {
	case <-client.closeChan:
	default:
		t.Fatal("closeChan 应已关闭")
	}
}

// TestWriteMessageIgnoresEmptyMessage 验证 nil 与空消息被忽略且不返回错误。
func TestWriteMessageIgnoresEmptyMessage(t *testing.T) {
	client := NewClient("client-1", 8)

	if err := client.WriteMessage(nil); err != nil {
		t.Fatalf("nil 消息不应报错: %v", err)
	}
	if err := client.WriteMessage(&Message{}); err != nil {
		t.Fatalf("空消息不应报错: %v", err)
	}

	if count := len(client.messageChan); count != 0 {
		t.Fatalf("无效消息不应入队，队列长度 = %d", count)
	}
}

// TestWriteMessageEnqueuesMessage 验证有效消息入队并可被消费。
func TestWriteMessageEnqueuesMessage(t *testing.T) {
	client := NewClient("client-1", 8)

	msg := &Message{Data: []byte("hello")}
	if err := client.WriteMessage(msg); err != nil {
		t.Fatalf("投递失败: %v", err)
	}

	select {
	case received := <-client.messageChan:
		if received != msg {
			t.Fatal("出队消息与投递消息不一致")
		}
	default:
		t.Fatal("消息未入队")
	}
}

// TestWriteMessageTimesOutWhenChannelIsFull 验证队列满且超过写超时后返回 ErrMessageChannelIsFull。
func TestWriteMessageTimesOutWhenChannelIsFull(t *testing.T) {
	client := NewClient("client-1", 2)
	client.SetWriteTimeout(10 * time.Millisecond)

	for i := 0; i < 2; i++ {
		if err := client.WriteMessage(&Message{Data: []byte("fill")}); err != nil {
			t.Fatalf("填充队列失败: %v", err)
		}
	}

	err := client.WriteMessage(&Message{Data: []byte("overflow")})
	if !errors.Is(err, ErrMessageChannelIsFull) {
		t.Fatalf("err = %v, want ErrMessageChannelIsFull", err)
	}
}

// TestWriteMessageRejectsClosedClient 验证关闭后的客户端拒绝新消息。
func TestWriteMessageRejectsClosedClient(t *testing.T) {
	client := NewClient("client-1", 8)
	if err := client.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	err := client.WriteMessage(&Message{Data: []byte("late")})
	if !errors.Is(err, ErrClientIsClosed) {
		t.Fatalf("err = %v, want ErrClientIsClosed", err)
	}
}

// TestWriteMessageWakesOnClose 验证阻塞中的投递会在客户端关闭时被唤醒，
// 而不是一直等到写超时。
func TestWriteMessageWakesOnClose(t *testing.T) {
	client := NewClient("client-1", 1)
	client.SetWriteTimeout(5 * time.Second)

	// 填满队列，使后续投递进入阻塞。
	if err := client.WriteMessage(&Message{Data: []byte("filler")}); err != nil {
		t.Fatalf("填充队列失败: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		result <- client.WriteMessage(&Message{Data: []byte("blocked")})
	}()

	// 等待投递协程阻塞后再关闭客户端。
	time.Sleep(20 * time.Millisecond)
	if err := client.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, ErrClientIsClosed) {
			t.Fatalf("err = %v, want ErrClientIsClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("关闭后阻塞的投递未被唤醒")
	}
}

// TestWriteResponseHeaderIsIdempotent 验证响应头只写一次且内容完整。
func TestWriteResponseHeaderIsIdempotent(t *testing.T) {
	client := NewClient("client-1", 8)
	resp, recorder := createSafeResponse()

	client.WriteResponseHeader(resp)
	client.WriteResponseHeader(resp)

	if code := recorder.Code(); code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", code)
	}

	expectedHeaders := map[string]string{
		"Content-Type":      "text/event-stream; charset=utf-8",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for name, expected := range expectedHeaders {
		if actual := recorder.Header().Get(name); actual != expected {
			t.Fatalf("%s = %q, want %q", name, actual, expected)
		}
	}

	if body := recorder.BodyString(); body != "" {
		t.Fatalf("写入响应头不应产生响应体，实际 %q", body)
	}
	if !recorder.IsFlushed() {
		t.Fatal("写入响应头后必须立即 Flush")
	}
}

// TestServeSendsLifecycleMessages 验证连接建立、普通消息与关闭消息的完整生命周期。
func TestServeSendsLifecycleMessages(t *testing.T) {
	client := NewClient("client-1", 8)
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.serve(resp, req, nil)
	}()

	waitForCondition(t, time.Second, func() bool {
		return strings.Contains(recorder.BodyString(), "connect")
	})

	if err := client.WriteMessage(&Message{Event: "update", Data: []byte("payload")}); err != nil {
		t.Fatalf("投递消息失败: %v", err)
	}

	waitForCondition(t, time.Second, func() bool {
		return strings.Contains(recorder.BodyString(), "payload")
	})

	if err := client.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("正常关闭时 serve 应返回 nil: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve 未在关闭后退出")
	}

	body := recorder.BodyString()
	for _, keyword := range []string{"connect", "client-1", "event: update", "data: payload", "close"} {
		if !strings.Contains(body, keyword) {
			t.Fatalf("响应缺少 %q: %q", keyword, body)
		}
	}
}

// TestServeReturnsContextErrorWhenRequestCanceled 验证请求上下文取消后 serve 返回取消错误。
func TestServeReturnsContextErrorWhenRequestCanceled(t *testing.T) {
	client := NewClient("client-1", 8)
	resp, _ := createSafeResponse()
	req, cancel := createServeRequest()

	done := make(chan error, 1)
	go func() {
		done <- client.serve(resp, req, nil)
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("请求取消后 serve 未退出")
	}
}

// TestServeSendsHeartbeat 验证心跳消息按配置周期下发。
func TestServeSendsHeartbeat(t *testing.T) {
	client := NewClient("client-1", 8)
	client.SetHeartbeatPeriod(10 * time.Millisecond)
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.serve(resp, req, nil)
	}()

	waitForCondition(t, time.Second, func() bool {
		return strings.Contains(recorder.BodyString(), "heartbeat")
	})

	_ = client.Close()
	<-done
}

// TestServeConsumesEventsChannel 验证业务事件通道的转发与关闭语义。
func TestServeConsumesEventsChannel(t *testing.T) {
	client := NewClient("client-1", 8)
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	events := make(chan *Message, 4)
	done := make(chan error, 1)
	go func() {
		done <- client.serve(resp, req, events)
	}()

	events <- &Message{Event: "custom", Data: []byte("from-events")}
	waitForCondition(t, time.Second, func() bool {
		return strings.Contains(recorder.BodyString(), "from-events")
	})

	// 关闭事件通道 ⇒ 写关闭消息并正常返回。
	close(events)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("事件通道关闭时 serve 应返回 nil: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("事件通道关闭后 serve 未退出")
	}

	if !strings.Contains(recorder.BodyString(), "close") {
		t.Fatalf("响应缺少关闭消息: %q", recorder.BodyString())
	}
}

// createWriteErrorWriter 包装真实响应写入器，使 Write 始终失败，用于覆盖错误分支。
type createWriteErrorWriter struct {
	response.Writer
}

// Write 始终返回错误。
func (own createWriteErrorWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// createFlakyWriter 第一次写入成功、后续写入失败，用于覆盖“队列消息写入失败”分支。
type createFlakyWriter struct {
	response.Writer
	lock  sync.Mutex
	wrote int
}

// Write 首次委托真实写入器，之后返回错误。
func (own *createFlakyWriter) Write(data []byte) (int, error) {
	own.lock.Lock()
	own.wrote++
	attempt := own.wrote
	own.lock.Unlock()

	if attempt > 1 {
		return 0, errors.New("broken pipe")
	}

	return own.Writer.Write(data)
}

// TestServeReturnsErrorWhenConnectedMessageFails 验证连接消息写入失败时立即返回错误。
func TestServeReturnsErrorWhenConnectedMessageFails(t *testing.T) {
	client := NewClient("client-1", 8)
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	err := client.serve(createWriteErrorWriter{Writer: resp}, req, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to write connected message") {
		t.Fatalf("err = %v, want 连接消息写入错误", err)
	}
	if code := recorder.Code(); code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", code)
	}
}

// TestServeReturnsErrorWhenQueuedMessageFails 验证队列消息写入失败时 serve 退出并返回错误。
func TestServeReturnsErrorWhenQueuedMessageFails(t *testing.T) {
	client := NewClient("client-1", 8)
	resp, _ := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	failing := &createFlakyWriter{Writer: resp}
	done := make(chan error, 1)
	go func() {
		done <- client.serve(failing, req, nil)
	}()

	// 等待连接消息写入完成后再投递，确保失败发生在队列消息分支。
	time.Sleep(20 * time.Millisecond)
	_ = client.WriteMessage(&Message{Data: []byte("will-fail")})

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "write message") {
			t.Fatalf("err = %v, want 消息写入错误", err)
		}
	case <-time.After(time.Second):
		t.Fatal("写入失败后 serve 未退出")
	}
}

// TestServeEventStreamRejectsNilRequest 验证 ServeEventStream 拒绝 nil 请求。
func TestServeEventStreamRejectsNilRequest(t *testing.T) {
	resp, _ := createSafeResponse()

	if err := ServeEventStream(resp, nil, nil, 8); err == nil {
		t.Fatal("nil 请求必须返回错误")
	}
}

// TestServeEventStreamStreamsEvents 验证一次性事件流函数的完整推送流程。
func TestServeEventStreamStreamsEvents(t *testing.T) {
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	event := func(ctx context.Context) <-chan *Message {
		channel := make(chan *Message, 2)
		go func() {
			channel <- &Message{Event: "greeting", Data: []byte("hello stream")}
			close(channel)
		}()

		return channel
	}

	err := ServeEventStream(resp, req, event, 8)
	if err != nil {
		t.Fatalf("ServeEventStream 失败: %v", err)
	}

	body := recorder.BodyString()
	for _, keyword := range []string{"connect", "greeting", "hello stream", "close"} {
		if !strings.Contains(body, keyword) {
			t.Fatalf("响应缺少 %q: %q", keyword, body)
		}
	}
}

// TestServeEventStreamWaitsForCancellation 验证 event 为 nil 时事件流保持连接，
// 直到请求上下文被取消。
func TestServeEventStreamWaitsForCancellation(t *testing.T) {
	resp, _ := createSafeResponse()
	req, cancel := createServeRequest()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := ServeEventStream(resp, req, nil, 8)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestClientConcurrentWriteMessage 验证多协程并发投递不会丢失消息。
func TestClientConcurrentWriteMessage(t *testing.T) {
	const goroutines, perGoroutine = 8, 10

	client := NewClient("client-1", goroutines*perGoroutine+8)
	client.SetWriteTimeout(time.Second)

	var waitGroup sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for j := 0; j < perGoroutine; j++ {
				if err := client.WriteMessage(&Message{Data: []byte("msg")}); err != nil {
					t.Errorf("投递失败: %v", err)
					return
				}
			}
		}()
	}

	waitGroup.Wait()

	if count := len(client.messageChan); count != goroutines*perGoroutine {
		t.Fatalf("队列长度 = %d, want %d", count, goroutines*perGoroutine)
	}
}

// BenchmarkClientWriteMessage 测量投递消息的耗时与内存分配。
func BenchmarkClientWriteMessage(b *testing.B) {
	client := NewClient("client-1", b.N+1)
	msg := &Message{Data: []byte("payload")}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := client.WriteMessage(msg); err != nil {
			b.Fatalf("投递失败: %v", err)
		}
	}
}
