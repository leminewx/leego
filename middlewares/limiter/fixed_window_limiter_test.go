package limiter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewFixedWindowLimiter 验证构造参数与默认值。
func TestNewFixedWindowLimiter(t *testing.T) {
	standard := NewFixedWindowLimiter(5, time.Second)

	if !standard.IsEnabled() {
		t.Fatal("限流器创建后必须处于启用状态")
	}

	if standard.GetLimit() != 5 {
		t.Fatalf("limit = %d, want 5", standard.GetLimit())
	}

	if standard.GetWindowSize() != time.Second {
		t.Fatalf("windowSize = %v, want 1s", standard.GetWindowSize())
	}

	fallback := NewFixedWindowLimiter(1, 0)
	if fallback.GetWindowSize() != _DEFAULT_FIXED_WINDOW_SIZE {
		t.Fatalf("非法窗口长度应回退为默认值，实际 = %v", fallback.GetWindowSize())
	}
}

// TestFixedWindowAllow 验证窗口内放行与拒绝。
func TestFixedWindowAllow(t *testing.T) {
	fixed := NewFixedWindowLimiter(3, time.Minute)

	for index := 0; index < 3; index++ {
		if !fixed.Allow() {
			t.Fatalf("第 %d 次 Allow() = false, want true", index+1)
		}
	}

	if fixed.Allow() {
		t.Fatal("超出限额后 Allow() 必须为 false")
	}
}

// TestFixedWindowResets 验证窗口到期后计数归零。
func TestFixedWindowResets(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, 30*time.Millisecond)

	if !fixed.Allow() {
		t.Fatal("第一次 Allow() 必须放行")
	}

	if fixed.Allow() {
		t.Fatal("同窗口内第二次 Allow() 必须拒绝")
	}

	time.Sleep(50 * time.Millisecond)

	if !fixed.Allow() {
		t.Fatal("新窗口必须重新放行")
	}
}

// TestFixedWindowUnlimited 验证非正数限额表示不限流。
func TestFixedWindowUnlimited(t *testing.T) {
	fixed := NewFixedWindowLimiter(0, time.Minute)

	for index := 0; index < 100; index++ {
		if !fixed.Allow() {
			t.Fatalf("不限流时第 %d 次 Allow() = false", index+1)
		}
	}
}

// TestFixedWindowSetLimit 验证热更新限额。
func TestFixedWindowSetLimit(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, time.Minute)

	if !fixed.Allow() {
		t.Fatal("第一次 Allow() 必须放行")
	}

	fixed.SetLimit(2)
	if !fixed.Allow() {
		t.Fatal("调大限额后必须放行")
	}

	fixed.SetLimit(0)
	if !fixed.Allow() {
		t.Fatal("限额为 0 时应不限流")
	}
}

// TestFixedWindowSetWindowSize 验证调整窗口会重置计数。
func TestFixedWindowSetWindowSize(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, time.Minute)
	_ = fixed.Allow()

	fixed.SetWindowSize(2 * time.Minute)
	if fixed.GetWindowSize() != 2*time.Minute {
		t.Fatalf("windowSize = %v, want 2m", fixed.GetWindowSize())
	}

	if !fixed.Allow() {
		t.Fatal("重置窗口后必须重新放行")
	}

	fixed.SetWindowSize(-time.Second)
	if fixed.GetWindowSize() != _DEFAULT_FIXED_WINDOW_SIZE {
		t.Fatalf("非法窗口长度应回退为默认值，实际 = %v", fixed.GetWindowSize())
	}
}

// TestFixedWindowGetConfig 验证配置读取。
func TestFixedWindowGetConfig(t *testing.T) {
	fixed := NewFixedWindowLimiter(7, 2*time.Second)

	limit, windowSize := fixed.GetConfig()
	if limit != 7 || windowSize != 2*time.Second {
		t.Fatalf("GetConfig() = (%d, %v), want (7, 2s)", limit, windowSize)
	}
}

// TestFixedWindowWaitWithTimeout 验证等待配额成功。
func TestFixedWindowWaitWithTimeout(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, 40*time.Millisecond)
	_ = fixed.Allow()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !fixed.AllowWithTimeout(ctx, time.Second) {
		t.Fatal("窗口滚动后 AllowWithTimeout 应放行")
	}
}

// TestFixedWindowWait 验证阻塞等待。
func TestFixedWindowWait(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, 30*time.Millisecond)
	_ = fixed.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := fixed.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

// TestFixedWindowEnableDisable 验证启停开关。
func TestFixedWindowEnableDisable(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, time.Minute)

	fixed.Disable()
	if fixed.IsEnabled() {
		t.Fatal("Disable 后必须为禁用状态")
	}

	fixed.Enable()
	if !fixed.IsEnabled() {
		t.Fatal("Enable 后必须为启用状态")
	}
}

// TestFixedWindowConcurrency 验证并发下不会超发。
func TestFixedWindowConcurrency(t *testing.T) {
	const limit = 100
	const workers = 50
	const attemptsPerWorker = 20

	fixed := NewFixedWindowLimiter(limit, time.Minute)

	var allowed atomic.Int64
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for attempt := 0; attempt < attemptsPerWorker; attempt++ {
				if fixed.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}

	waitGroup.Wait()

	if got := allowed.Load(); got != limit {
		t.Fatalf("放行数量 = %d, want %d（并发下不得超发）", got, limit)
	}
}

// BenchmarkFixedWindowAllow 测量固定窗口判断开销。
func BenchmarkFixedWindowAllow(b *testing.B) {
	fixed := NewFixedWindowLimiter(0, time.Minute)

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		fixed.Allow()
	}
}
