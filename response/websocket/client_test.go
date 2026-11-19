package websocket

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestNewClientInitializesFields 验证客户端创建时的默认字段。
func TestNewClientInitializesFields(t *testing.T) {
	before := time.Now()
	client := NewClient("client-1", "user-1", nil)

	if client.Id != "client-1" || client.UserId != "user-1" {
		t.Fatalf("Id/UserId = (%q, %q)", client.Id, client.UserId)
	}

	if cap(client.SendChan) != _DEFAULT_SEND_BUFFER_SIZE {
		t.Fatalf("SendChan 容量 = %d, want %d", cap(client.SendChan), _DEFAULT_SEND_BUFFER_SIZE)
	}

	if client.Metadata == nil {
		t.Fatal("Metadata 不应为 nil")
	}

	if client.ConnectedAt.Before(before) || client.ConnectedAt.After(time.Now()) {
		t.Fatalf("ConnectedAt = %v, 不在合理区间", client.ConnectedAt)
	}

	if client.IsClosed() {
		t.Fatal("新建客户端不应处于关闭状态")
	}
}

// TestClientWriteMessageEnqueuesPayload 验证消息被放入发送队列。
func TestClientWriteMessageEnqueuesPayload(t *testing.T) {
	client := NewClient("client-1", "", nil)
	payload := []byte("hello")

	if err := client.WriteMessage(payload); err != nil {
		t.Fatalf("WriteMessage 失败: %v", err)
	}

	select {
	case received := <-client.SendChan:
		if string(received) != "hello" {
			t.Fatalf("队列内容 = %q, want hello", received)
		}

	case <-time.After(time.Second):
		t.Fatal("消息未进入发送队列")
	}
}

// TestClientWriteMessageIgnoresNil 验证 nil 负载被静默忽略。
func TestClientWriteMessageIgnoresNil(t *testing.T) {
	client := NewClient("client-1", "", nil)

	if err := client.WriteMessage(nil); err != nil {
		t.Fatalf("WriteMessage(nil) = %v, want nil", err)
	}

	if len(client.SendChan) != 0 {
		t.Fatalf("队列长度 = %d, want 0", len(client.SendChan))
	}
}

// TestClientWriteMessageRejectsFullQueue 验证队列满时快速失败。
func TestClientWriteMessageRejectsFullQueue(t *testing.T) {
	client := NewClient("client-1", "", nil)
	payload := []byte("x")

	for index := 0; index < _DEFAULT_SEND_BUFFER_SIZE; index++ {
		if err := client.WriteMessage(payload); err != nil {
			t.Fatalf("第 %d 次写入失败: %v", index, err)
		}
	}

	err := client.WriteMessage(payload)
	if !errors.Is(err, ErrSendBufferIsFull) {
		t.Fatalf("err = %v, want ErrSendBufferIsFull", err)
	}
}

// TestClientWriteMessageAfterClose 验证关闭后投递返回 ErrClientIsClosed。
func TestClientWriteMessageAfterClose(t *testing.T) {
	client := NewClient("client-1", "", nil)

	if err := client.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	if err := client.WriteMessage([]byte("x")); !errors.Is(err, ErrClientIsClosed) {
		t.Fatalf("err = %v, want ErrClientIsClosed", err)
	}
}

// TestClientCloseIsIdempotent 验证重复关闭安全且只执行一次清理。
func TestClientCloseIsIdempotent(t *testing.T) {
	client := NewClient("client-1", "", nil)

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
	case <-client.CloseChan:
	default:
		t.Fatal("CloseChan 应已关闭")
	}
}

// TestClientSetAndGetUserId 验证用户标识的读写。
func TestClientSetAndGetUserId(t *testing.T) {
	client := NewClient("client-1", "", nil)

	client.SetUserId("user-2")
	if got := client.GetUserId(); got != "user-2" {
		t.Fatalf("GetUserId() = %q, want user-2", got)
	}
}

// TestClientSetMetadataIgnoresEmptyKey 验证空键被忽略。
func TestClientSetMetadataIgnoresEmptyKey(t *testing.T) {
	client := NewClient("client-1", "", nil)

	client.SetMetadata("", "value")

	if metadata := client.GetMetadata(); len(metadata) != 0 {
		t.Fatalf("Metadata = %v, want 空", metadata)
	}
}

// TestClientGetMetadataReturnsCopy 验证元数据返回的是副本。
func TestClientGetMetadataReturnsCopy(t *testing.T) {
	client := NewClient("client-1", "", nil)
	client.SetMetadata("role", "admin")

	metadata := client.GetMetadata()
	metadata["role"] = "guest"
	metadata["extra"] = true

	copied := client.GetMetadata()
	if copied["role"] != "admin" {
		t.Fatalf("role = %v, want admin", copied["role"])
	}

	if _, isExisted := copied["extra"]; isExisted {
		t.Fatal("修改返回值不应影响客户端内部状态")
	}
}

// TestClientGetSnapshotReturnsCopy 验证快照与内部状态隔离。
func TestClientGetSnapshotReturnsCopy(t *testing.T) {
	client := NewClient("client-1", "user-1", nil)
	client.SetMetadata("role", "admin")

	snapshot := client.GetSnapshot()
	if snapshot.Id != "client-1" || snapshot.UserId != "user-1" {
		t.Fatalf("快照 = %+v", snapshot)
	}

	snapshot.Metadata["role"] = "guest"

	if got := client.GetSnapshot().Metadata["role"]; got != "admin" {
		t.Fatalf("内部 role = %v, want admin", got)
	}
}

// TestClientConcurrentStateAccess 验证状态读写可并发执行。
func TestClientConcurrentStateAccess(t *testing.T) {
	client := NewClient("client-1", "", nil)

	const workers, iterations = 8, 100

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()

			for index := 0; index < iterations; index++ {
				switch worker % 4 {
				case 0:
					client.SetUserId("user")
				case 1:
					client.SetMetadata("index", index)
				case 2:
					_ = client.GetUserId()
				default:
					_ = client.GetSnapshot()
				}
			}
		}(worker)
	}

	waitGroup.Wait()
}

// TestClientExportedErrors 验证导出的哨兵错误已定义且互不相同。
func TestClientExportedErrors(t *testing.T) {
	if ErrClientIsClosed == nil || ErrSendBufferIsFull == nil {
		t.Fatal("导出错误不应为 nil")
	}

	if errors.Is(ErrClientIsClosed, ErrSendBufferIsFull) {
		t.Fatal("两个错误不应相等")
	}
}
