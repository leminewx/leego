package sse

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewManagerUsesDefaults 验证管理器的默认参数。
func TestNewManagerUsesDefaults(t *testing.T) {
	manager := NewManager()

	if got := manager.getMessageCacheNumber(); got != 16 {
		t.Fatalf("消息队列长度 = %d, want 16", got)
	}
	if got := manager.getWriteTimeout(); got != _DEFAULT_WRITE_TIMEOUT {
		t.Fatalf("写超时 = %v, want %v", got, _DEFAULT_WRITE_TIMEOUT)
	}
	if got := manager.getHeartbeatPeriod(); got != _DEFAULT_HEARTBEAT_PERIOD {
		t.Fatalf("心跳周期 = %v, want %v", got, _DEFAULT_HEARTBEAT_PERIOD)
	}
	if capacity := cap(manager.GetErrorChan()); capacity != _DEFAULT_ERROR_BUFFER_SIZE {
		t.Fatalf("错误通道容量 = %d, want %d", capacity, _DEFAULT_ERROR_BUFFER_SIZE)
	}
	if manager.IsClosed() {
		t.Fatal("新建管理器不应处于关闭状态")
	}
	if manager.CountClients() != 0 {
		t.Fatalf("新建管理器客户端数 = %d, want 0", manager.CountClients())
	}
}

// TestManagerSettersIgnoreNonPositive 验证非正数配置被忽略。
func TestManagerSettersIgnoreNonPositive(t *testing.T) {
	manager := NewManager()
	manager.SetMessageCacheNumber(32)
	manager.SetWriteTimeout(500 * time.Millisecond)
	manager.SetHeartbeatPeriod(5 * time.Second)

	manager.SetMessageCacheNumber(0)
	manager.SetWriteTimeout(0)
	manager.SetHeartbeatPeriod(0)

	if got := manager.getMessageCacheNumber(); got != 32 {
		t.Fatalf("消息队列长度 = %d, want 32", got)
	}
	if got := manager.getWriteTimeout(); got != 500*time.Millisecond {
		t.Fatalf("写超时 = %v, want 500ms", got)
	}
	if got := manager.getHeartbeatPeriod(); got != 5*time.Second {
		t.Fatalf("心跳周期 = %v, want 5s", got)
	}
}

// TestManagerGettersFallBackToDefaults 验证零值管理器的读取回退到默认配置。
func TestManagerGettersFallBackToDefaults(t *testing.T) {
	manager := &Manager{}

	if got := manager.getMessageCacheNumber(); got != _DEFAULT_MESSAGE_CACHE_NUMBER {
		t.Fatalf("消息队列长度 = %d, want %d", got, _DEFAULT_MESSAGE_CACHE_NUMBER)
	}
	if got := manager.getWriteTimeout(); got != _DEFAULT_WRITE_TIMEOUT {
		t.Fatalf("写超时 = %v, want %v", got, _DEFAULT_WRITE_TIMEOUT)
	}
	if got := manager.getHeartbeatPeriod(); got != _DEFAULT_HEARTBEAT_PERIOD {
		t.Fatalf("心跳周期 = %v, want %v", got, _DEFAULT_HEARTBEAT_PERIOD)
	}
}

// TestManagerServeRejectsNilArguments 验证 Serve 拒绝 nil 响应或请求。
func TestManagerServeRejectsNilArguments(t *testing.T) {
	manager := NewManager()

	if err := manager.Serve(nil, nil, "c1"); err == nil {
		t.Fatal("nil 参数必须返回错误")
	}

	resp, _ := createSafeResponse()
	if err := manager.Serve(resp, nil, "c1"); err == nil {
		t.Fatal("nil 请求必须返回错误")
	}
}

// TestManagerServeRegistersAndUnregisters 验证 Serve 的注册、推送与自动注销。
func TestManagerServeRegistersAndUnregisters(t *testing.T) {
	manager := NewManager()
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()

	done := make(chan error, 1)
	go func() {
		done <- manager.Serve(resp, req, "client-1")
	}()

	waitForCondition(t, time.Second, func() bool {
		return manager.IsClientConnected("client-1")
	})

	if count := manager.CountClients(); count != 1 {
		t.Fatalf("客户端数 = %d, want 1", count)
	}
	if ids := manager.GetAllClientIds(); len(ids) != 1 || ids[0] != "client-1" {
		t.Fatalf("客户端列表 = %v, want [client-1]", ids)
	}

	if err := manager.SendTo("client-1", &Message{Event: "notice", Data: []byte("hello client")}); err != nil {
		t.Fatalf("定向发送失败: %v", err)
	}

	waitForCondition(t, time.Second, func() bool {
		return strings.Contains(recorder.BodyString(), "hello client")
	})

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("请求取消后 Serve 未退出")
	}

	waitForCondition(t, time.Second, func() bool {
		return !manager.IsClientConnected("client-1")
	})
}

// TestManagerServeRejectsDuplicateClientId 验证同一标识的客户端不允许重复注册。
func TestManagerServeRejectsDuplicateClientId(t *testing.T) {
	manager := NewManager()

	resp, _ := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- manager.Serve(resp, req, "duplicate")
	}()

	waitForCondition(t, time.Second, func() bool {
		return manager.IsClientConnected("duplicate")
	})

	secondResp, _ := createSafeResponse()
	secondReq, secondCancel := createServeRequest()
	defer secondCancel()

	err := manager.Serve(secondResp, secondReq, "duplicate")
	if !errors.Is(err, ErrClientAlreadyExists) {
		t.Fatalf("err = %v, want ErrClientAlreadyExists", err)
	}

	cancel()
	<-done
}

// TestManagerSendToUnknownClient 验证向不存在的客户端发送返回错误。
func TestManagerSendToUnknownClient(t *testing.T) {
	manager := NewManager()

	err := manager.SendTo("missing", &Message{Data: []byte("x")})
	if err == nil {
		t.Fatal("向不存在的客户端发送必须返回错误")
	}
}

// TestManagerUnregisterIsIdempotent 验证注销不存在的客户端返回 nil，且可重复调用。
func TestManagerUnregisterIsIdempotent(t *testing.T) {
	manager := NewManager()

	if err := manager.Unregister("missing"); err != nil {
		t.Fatalf("注销不存在的客户端应返回 nil: %v", err)
	}

	client := NewClient("client-1", 4)
	manager.clients.Store("client-1", client)

	if err := manager.Unregister("client-1"); err != nil {
		t.Fatalf("注销失败: %v", err)
	}
	if !client.IsClosed() {
		t.Fatal("注销后客户端应被关闭")
	}
	if manager.IsClientConnected("client-1") {
		t.Fatal("注销后不应再出现在在线列表")
	}

	if err := manager.Unregister("client-1"); err != nil {
		t.Fatalf("重复注销应返回 nil: %v", err)
	}
}

// TestManagerBroadcastDeliversToAll 验证广播会向所有在线客户端投递。
func TestManagerBroadcastDeliversToAll(t *testing.T) {
	manager := NewManager()

	first := NewClient("a", 4)
	second := NewClient("b", 4)
	manager.clients.Store("a", first)
	manager.clients.Store("b", second)

	if err := manager.Broadcast(&Message{Event: "broadcast", Data: []byte("hello all")}); err != nil {
		t.Fatalf("广播失败: %v", err)
	}

	for name, client := range map[string]*Client{"a": first, "b": second} {
		if count := len(client.messageChan); count != 1 {
			t.Fatalf("客户端 %s 队列长度 = %d, want 1", name, count)
		}

		msg := <-client.messageChan
		if len(msg.Data) == 0 || msg.Event != "broadcast" {
			t.Fatalf("客户端 %s 收到消息 = %+v", name, msg)
		}
	}
}

// TestManagerBroadcastIgnoresEmptyMessage 验证 nil 与空消息广播为无操作。
func TestManagerBroadcastIgnoresEmptyMessage(t *testing.T) {
	manager := NewManager()
	client := NewClient("a", 4)
	manager.clients.Store("a", client)

	if err := manager.Broadcast(nil); err != nil {
		t.Fatalf("nil 消息广播不应报错: %v", err)
	}
	if err := manager.Broadcast(&Message{}); err != nil {
		t.Fatalf("空消息广播不应报错: %v", err)
	}

	if count := len(client.messageChan); count != 0 {
		t.Fatalf("无效消息不应投递，队列长度 = %d", count)
	}

	if err := manager.BroadcastTo([]string{"a"}, nil); err != nil {
		t.Fatalf("nil 消息定向广播不应报错: %v", err)
	}
}

// TestManagerBroadcastToDeliversToSubset 验证定向广播只投递给指定且存在的客户端。
func TestManagerBroadcastToDeliversToSubset(t *testing.T) {
	manager := NewManager()

	target := NewClient("target", 4)
	other := NewClient("other", 4)
	manager.clients.Store("target", target)
	manager.clients.Store("other", other)

	err := manager.BroadcastTo([]string{"target", "missing"}, &Message{Event: "subset", Data: []byte("only target")})
	if err != nil {
		t.Fatalf("定向广播失败: %v", err)
	}

	if count := len(target.messageChan); count != 1 {
		t.Fatalf("目标客户端队列长度 = %d, want 1", count)
	}
	if count := len(other.messageChan); count != 0 {
		t.Fatalf("非目标客户端不应收到消息，队列长度 = %d", count)
	}
}

// TestManagerRemovesClientOnDeliveryFailure 验证投递失败时客户端被注销，
// 且失败原因写入错误通道。
func TestManagerRemovesClientOnDeliveryFailure(t *testing.T) {
	manager := NewManager()

	client := NewClient("full", 1)
	client.SetWriteTimeout(5 * time.Millisecond)
	if err := client.WriteMessage(&Message{Data: []byte("fill")}); err != nil {
		t.Fatalf("填充队列失败: %v", err)
	}
	manager.clients.Store("full", client)

	if err := manager.Broadcast(&Message{Data: []byte("overflow")}); err != nil {
		t.Fatalf("广播失败: %v", err)
	}

	select {
	case err := <-manager.GetErrorChan():
		if !errors.Is(err, ErrMessageChannelIsFull) {
			t.Fatalf("err = %v, want ErrMessageChannelIsFull", err)
		}
	case <-time.After(time.Second):
		t.Fatal("投递失败未写入错误通道")
	}

	if manager.IsClientConnected("full") {
		t.Fatal("投递失败后客户端应被注销")
	}
	if !client.IsClosed() {
		t.Fatal("投递失败后客户端应被关闭")
	}
}

// TestManagerGetAllClientIdsIsSorted 验证客户端标识按字典序升序返回。
func TestManagerGetAllClientIdsIsSorted(t *testing.T) {
	manager := NewManager()
	for _, id := range []string{"charlie", "alpha", "bravo"} {
		manager.clients.Store(id, NewClient(id, 4))
	}

	ids := manager.GetAllClientIds()
	expected := []string{"alpha", "bravo", "charlie"}
	if len(ids) != len(expected) {
		t.Fatalf("客户端列表 = %v, want %v", ids, expected)
	}
	for index, id := range expected {
		if ids[index] != id {
			t.Fatalf("客户端列表 = %v, want %v", ids, expected)
		}
	}
}

// TestManagerRejectsOperationsAfterClose 验证关闭后所有对外操作返回 ErrManagerIsClosed。
func TestManagerRejectsOperationsAfterClose(t *testing.T) {
	manager := NewManager()
	if err := manager.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if !manager.IsClosed() {
		t.Fatal("关闭后 IsClosed 应为 true")
	}

	resp, _ := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	if err := manager.Serve(resp, req, "client-1"); !errors.Is(err, ErrManagerIsClosed) {
		t.Fatalf("Serve err = %v, want ErrManagerIsClosed", err)
	}
	if err := manager.SendTo("client-1", &Message{Data: []byte("x")}); !errors.Is(err, ErrManagerIsClosed) {
		t.Fatalf("SendTo err = %v, want ErrManagerIsClosed", err)
	}
	if err := manager.Broadcast(&Message{Data: []byte("x")}); !errors.Is(err, ErrManagerIsClosed) {
		t.Fatalf("Broadcast err = %v, want ErrManagerIsClosed", err)
	}
	if err := manager.BroadcastTo([]string{"client-1"}, &Message{Data: []byte("x")}); !errors.Is(err, ErrManagerIsClosed) {
		t.Fatalf("BroadcastTo err = %v, want ErrManagerIsClosed", err)
	}
}

// TestManagerCloseReleasesClients 验证关闭管理器会关闭所有客户端并清空列表。
func TestManagerCloseReleasesClients(t *testing.T) {
	manager := NewManager()

	first := NewClient("a", 4)
	second := NewClient("b", 4)
	manager.clients.Store("a", first)
	manager.clients.Store("b", second)

	if err := manager.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if !first.IsClosed() || !second.IsClosed() {
		t.Fatal("关闭管理器后所有客户端都应关闭")
	}
	if count := manager.CountClients(); count != 0 {
		t.Fatalf("关闭后客户端数 = %d, want 0", count)
	}

	select {
	case <-manager.Done():
	default:
		t.Fatal("Done 通道应已关闭")
	}

	if _, isOpen := <-manager.GetErrorChan(); isOpen {
		t.Fatal("错误通道应已关闭")
	}

	if err := manager.Close(); err != nil {
		t.Fatalf("重复关闭失败: %v", err)
	}
}

// TestManagerCloseNotifiesServingClients 验证关闭管理器时，
// 正在服务的客户端会收到关闭消息并正常退出。
func TestManagerCloseNotifiesServingClients(t *testing.T) {
	manager := NewManager()
	resp, recorder := createSafeResponse()
	req, cancel := createServeRequest()
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- manager.Serve(resp, req, "client-1")
	}()

	waitForCondition(t, time.Second, func() bool {
		return manager.IsClientConnected("client-1")
	})

	if err := manager.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("管理器关闭时 Serve 应正常返回: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("管理器关闭后 Serve 未退出")
	}

	body := recorder.BodyString()
	if !strings.Contains(body, "shutting down") {
		t.Fatalf("客户端应收到服务器关闭消息: %q", body)
	}
	if !strings.Contains(body, "close") {
		t.Fatalf("客户端应收到连接关闭消息: %q", body)
	}
}

// TestManagerSendErrorDropsWhenClosed 验证错误通道关闭后投递错误不会 panic。
func TestManagerSendErrorDropsWhenClosed(t *testing.T) {
	manager := NewManager()
	if err := manager.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	// 关闭后调用不应 panic。
	manager.sendError(errors.New("late error"))
	manager.sendError(nil)
}

// TestManagerConcurrentCount 验证并发读取客户端数量是安全的。
func TestManagerConcurrentCount(t *testing.T) {
	manager := NewManager()
	for i := 0; i < 10; i++ {
		client := NewClient("client", 4)
		manager.clients.Store("client-"+string(rune('a'+i)), client)
	}

	var waitGroup sync.WaitGroup
	for i := 0; i < 8; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for j := 0; j < 100; j++ {
				if count := manager.CountClients(); count != 10 {
					t.Errorf("客户端数 = %d, want 10", count)
					return
				}
			}
		}()
	}
	waitGroup.Wait()
}

// BenchmarkManagerBroadcast 测量向 100 个客户端广播消息的耗时。
func BenchmarkManagerBroadcast(b *testing.B) {
	manager := NewManager()
	for i := 0; i < 100; i++ {
		client := NewClient("client", b.N+1)
		manager.clients.Store("client-"+string(rune('a'+i%26))+string(rune('a'+i/26)), client)
	}

	msg := &Message{Event: "bench", Data: []byte("payload")}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := manager.Broadcast(msg); err != nil {
			b.Fatalf("广播失败: %v", err)
		}
	}
}

// 编译期断言：syncRecorder 可作为底层 http.ResponseWriter 使用。
var _ http.ResponseWriter = (*syncRecorder)(nil)
