package session

import (
	"context"
	"testing"
	"time"
)

// TestMemoryStoreRejectsEmptyID 验证空会话 ID 被拒绝。
func TestMemoryStoreRejectsEmptyID(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	if _, err := store.Get(ctx, ""); err != ErrNotFound {
		t.Fatalf("Get 空 ID 错误 = %v, want ErrNotFound", err)
	}

	if err := store.Save(ctx, "", Values{"user": "alice"}, time.Minute); err != ErrNotFound {
		t.Fatalf("Save 空 ID 错误 = %v, want ErrNotFound", err)
	}
}

// TestMemoryStoreCopiesValues 验证写入与读取都会复制会话数据。
func TestMemoryStoreCopiesValues(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	values := Values{"user": "alice"}
	if err := store.Save(ctx, "id", values, time.Minute); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	values["user"] = "bob"

	loaded, err := store.Get(ctx, "id")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}

	if loaded["user"] != "alice" {
		t.Fatalf("Save 未复制数据: %v", loaded)
	}

	loaded["user"] = "carol"
	again, err := store.Get(ctx, "id")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}

	if again["user"] != "alice" {
		t.Fatalf("Get 未返回副本: %v", again)
	}
}

// TestMemoryStoreDestroyAndSweep 验证销毁幂等与过期清理。
func TestMemoryStoreDestroyAndSweep(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	if err := store.Save(ctx, "a", Values{"k": 1}, time.Minute); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	if err := store.Save(ctx, "b", Values{"k": 2}, time.Minute); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	if err := store.Destroy(ctx, "a"); err != nil {
		t.Fatalf("Destroy 失败: %v", err)
	}

	if err := store.Destroy(ctx, "a"); err != nil {
		t.Fatalf("重复 Destroy 应成功: %v", err)
	}

	if length := store.Len(); length != 1 {
		t.Fatalf("会话数量 = %d, want 1", length)
	}

	store.sweep(time.Now().Add(2 * time.Minute))
	if length := store.Len(); length != 0 {
		t.Fatalf("过期条目未被清理: %d", length)
	}
}

// TestMemoryStoreCloseIsIdempotent 验证关闭可重复调用。
func TestMemoryStoreCloseIsIdempotent(t *testing.T) {
	store := NewMemoryStore()

	if err := store.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("重复 Close 失败: %v", err)
	}
}
