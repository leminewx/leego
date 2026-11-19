package leego

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestAddShutdownHookRejectsNil 验证空钩子被拒绝。
func TestAddShutdownHookRejectsNil(t *testing.T) {
	engine := createTestEngine()

	if err := engine.AddShutdownHook(nil); !errors.Is(err, ErrNilShutdownHook) {
		t.Fatalf("AddShutdownHook(nil) error = %v, want %v", err, ErrNilShutdownHook)
	}

	if count := engine.countShutdownHooks(); count != 0 {
		t.Fatalf("钩子数量 = %d, want 0", count)
	}
}

// TestRunShutdownHooksRunsInReverseOrder 验证钩子逆序执行且错误被汇总。
func TestRunShutdownHooksRunsInReverseOrder(t *testing.T) {
	engine := createTestEngine()

	var order []string
	for _, name := range []string{"first", "second", "third"} {
		hookName := name
		err := engine.AddShutdownHook(func(context.Context) error {
			order = append(order, hookName)
			if hookName == "second" {
				return errors.New("second failed")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("AddShutdownHook(%q) error = %v", hookName, err)
		}
	}

	if count := engine.countShutdownHooks(); count != 3 {
		t.Fatalf("钩子数量 = %d, want 3", count)
	}

	err := engine.RunShutdownHooks(nil)
	if err == nil || !strings.Contains(err.Error(), "second failed") {
		t.Fatalf("RunShutdownHooks() error = %v, want 包含 second failed", err)
	}

	want := []string{"third", "second", "first"}
	if len(order) != len(want) {
		t.Fatalf("执行顺序 = %v, want %v", order, want)
	}

	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("执行顺序 = %v, want %v", order, want)
		}
	}
}

// TestRunShutdownHooksWithoutHooks 验证无钩子时返回 nil。
func TestRunShutdownHooksWithoutHooks(t *testing.T) {
	engine := createTestEngine()
	if err := engine.RunShutdownHooks(context.Background()); err != nil {
		t.Fatalf("RunShutdownHooks() error = %v", err)
	}
}
