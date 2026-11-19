package limiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

// TestNewSlidingWindowLimiter 验证构造参数与默认值。
func TestNewSlidingWindowLimiter(t *testing.T) {
	standard := NewSlidingWindowLimiter(5, 4, 2*time.Second)

	if !standard.IsEnabled() {
		t.Fatal("限流器创建后必须处于启用状态")
	}

	if standard.GetLimit() != 5 {
		t.Fatalf("limit = %d, want 5", standard.GetLimit())
	}

	if standard.GetBucketCount() != 4 {
		t.Fatalf("bucketCount = %d, want 4", standard.GetBucketCount())
	}

	if standard.GetWindowSize() != 2*time.Second {
		t.Fatalf("windowSize = %v, want 2s", standard.GetWindowSize())
	}

	fallback := NewSlidingWindowLimiter(5, 0, 0)
	if fallback.GetBucketCount() != _DEFAULT_SLIDING_BUCKET_COUNT {
		t.Fatalf("非法桶数量应回退为默认值，实际 = %d", fallback.GetBucketCount())
	}

	if fallback.GetWindowSize() != _DEFAULT_SLIDING_WINDOW_SIZE {
		t.Fatalf("非法窗口长度应回退为默认值，实际 = %v", fallback.GetWindowSize())
	}
}

// TestSlidingWindowAllow 验证窗口内放行与拒绝。
func TestSlidingWindowAllow(t *testing.T) {
	sliding := NewSlidingWindowLimiter(3, 10, time.Minute)

	for index := 0; index < 3; index++ {
		if !sliding.Allow() {
			t.Fatalf("第 %d 次 Allow() = false, want true", index+1)
		}
	}

	if sliding.Allow() {
		t.Fatal("超出限额后 Allow() 必须为 false")
	}

	if count := sliding.GetCurrentCount(); count != 3 {
		t.Fatalf("GetCurrentCount() = %d, want 3", count)
	}
}

// TestSlidingWindowUnlimited 验证非正数限额表示不限流。
func TestSlidingWindowUnlimited(t *testing.T) {
	sliding := NewSlidingWindowLimiter(0, 10, time.Minute)

	for index := 0; index < 100; index++ {
		if !sliding.Allow() {
			t.Fatalf("不限流时第 %d 次 Allow() = false", index+1)
		}
	}

	// 不限流分支不计数，避免长期运行后计数无意义增长。
	if count := sliding.GetCurrentCount(); count != 0 {
		t.Fatalf("不限流时 GetCurrentCount() = %d, want 0", count)
	}
}

// TestSlidingWindowSetLimit 验证热更新限额。
func TestSlidingWindowSetLimit(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Minute)

	if !sliding.Allow() {
		t.Fatal("第一次 Allow() 必须放行")
	}

	if sliding.Allow() {
		t.Fatal("限额为 1 时第二次 Allow() 必须拒绝")
	}

	sliding.SetLimit(2)
	if !sliding.Allow() {
		t.Fatal("调大限额后必须放行")
	}

	sliding.SetLimit(0)
	if !sliding.Allow() {
		t.Fatal("限额为 0 时应不限流")
	}
}

// TestSlidingWindowSetBucketCount 验证调整桶数量会重置窗口。
func TestSlidingWindowSetBucketCount(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Minute)
	_ = sliding.Allow()

	sliding.SetBucketCount(4)
	if sliding.GetBucketCount() != 4 {
		t.Fatalf("bucketCount = %d, want 4", sliding.GetBucketCount())
	}

	if count := sliding.GetCurrentCount(); count != 0 {
		t.Fatalf("重建桶后计数必须归零，实际 = %d", count)
	}

	if !sliding.Allow() {
		t.Fatal("重置窗口后必须重新放行")
	}

	sliding.SetBucketCount(0)
	if sliding.GetBucketCount() != _DEFAULT_SLIDING_BUCKET_COUNT {
		t.Fatalf("非法桶数量应回退为默认值，实际 = %d", sliding.GetBucketCount())
	}
}

// TestSlidingWindowSetWindowSize 验证调整窗口长度会重置计数。
func TestSlidingWindowSetWindowSize(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Minute)
	_ = sliding.Allow()

	sliding.SetWindowSize(2 * time.Minute)
	if sliding.GetWindowSize() != 2*time.Minute {
		t.Fatalf("windowSize = %v, want 2m", sliding.GetWindowSize())
	}

	if count := sliding.GetCurrentCount(); count != 0 {
		t.Fatalf("重置窗口后计数必须归零，实际 = %d", count)
	}

	if !sliding.Allow() {
		t.Fatal("重置窗口后必须重新放行")
	}

	sliding.SetWindowSize(time.Millisecond)
	if sliding.GetWindowSize() != _DEFAULT_SLIDING_WINDOW_SIZE {
		t.Fatalf("非法窗口长度应回退为默认值，实际 = %v", sliding.GetWindowSize())
	}
}

// TestSlidingWindowGetConfig 验证配置读取。
func TestSlidingWindowGetConfig(t *testing.T) {
	sliding := NewSlidingWindowLimiter(7, 5, 2*time.Second)

	limit, bucketCount, windowSize := sliding.GetConfig()
	if limit != 7 || bucketCount != 5 || windowSize != 2*time.Second {
		t.Fatalf("GetConfig() = (%d, %d, %v), want (7, 5, 2s)", limit, bucketCount, windowSize)
	}
}

// TestSlidingWindowAllowsAfterWindowSlides 验证窗口整体滚动后重新放行。
//
// 滑动窗口的最小长度被限制为 1 秒，因此这里必须真实等待一个完整窗口，
// 才能观察到“过期桶被清零、计数归零”的效果。
func TestSlidingWindowAllowsAfterWindowSlides(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Second)

	if !sliding.Allow() {
		t.Fatal("第一次 Allow() 必须放行")
	}

	if sliding.Allow() {
		t.Fatal("同窗口内第二次 Allow() 必须拒绝")
	}

	time.Sleep(1100 * time.Millisecond)

	if !sliding.Allow() {
		t.Fatal("窗口滚动后必须重新放行")
	}

	if count := sliding.GetCurrentCount(); count != 1 {
		t.Fatalf("GetCurrentCount() = %d, want 1", count)
	}
}

// TestSlidingWindowWaitWithTimeout 验证等待配额成功。
func TestSlidingWindowWaitWithTimeout(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Second)
	_ = sliding.Allow()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !sliding.AllowWithTimeout(ctx, 3*time.Second) {
		t.Fatal("窗口滚动后 AllowWithTimeout 应放行")
	}
}

// TestSlidingWindowWait 验证阻塞等待。
func TestSlidingWindowWait(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Second)
	_ = sliding.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sliding.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

// TestSlidingWindowWaitCanceled 验证上下文取消时等待失败。
func TestSlidingWindowWaitCanceled(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Minute)
	_ = sliding.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := sliding.Wait(ctx); err == nil {
		t.Fatal("上下文超时后 Wait() 必须返回错误")
	}
}

// TestSlidingWindowEnableDisable 验证启停开关。
func TestSlidingWindowEnableDisable(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Minute)

	sliding.Disable()
	if sliding.IsEnabled() {
		t.Fatal("Disable 后必须为禁用状态")
	}

	sliding.Enable()
	if !sliding.IsEnabled() {
		t.Fatal("Enable 后必须为启用状态")
	}
}

// TestSlidingWindowConcurrency 验证并发下不会超发。
func TestSlidingWindowConcurrency(t *testing.T) {
	const limit = 100
	const workers = 50
	const attemptsPerWorker = 20

	sliding := NewSlidingWindowLimiter(limit, 10, time.Minute)

	var allowed atomic.Int64
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for attempt := 0; attempt < attemptsPerWorker; attempt++ {
				if sliding.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}

	waitGroup.Wait()

	if got := allowed.Load(); got != limit {
		t.Fatalf("放行数量 = %d, want %d（并发下不得超发）", got, limit)
	}

	if count := sliding.GetCurrentCount(); count != limit {
		t.Fatalf("GetCurrentCount() = %d, want %d", count, limit)
	}
}

// TestSlidingWindowMiddleware 验证中间件在配额耗尽时返回 429，
// 并在禁用后直接放行。
func TestSlidingWindowMiddleware(t *testing.T) {
	sliding := NewSlidingWindowLimiter(1, 10, time.Minute)

	called := 0
	handle := sliding.GetMiddleware(0)(func(ctx *leego.Context) {
		called++
		ctx.ResponseText(http.StatusOK, "ok")
	})

	firstRecorder := httptest.NewRecorder()
	handle(leego.NewContext(firstRecorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("第一次请求状态码 = %d, want 200", firstRecorder.Code)
	}

	secondRecorder := httptest.NewRecorder()
	handle(leego.NewContext(secondRecorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("第二次请求状态码 = %d, want 429", secondRecorder.Code)
	}

	if got := secondRecorder.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}

	if called != 1 {
		t.Fatalf("业务处理函数调用次数 = %d, want 1", called)
	}

	sliding.Disable()

	thirdRecorder := httptest.NewRecorder()
	handle(leego.NewContext(thirdRecorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if thirdRecorder.Code != http.StatusOK {
		t.Fatalf("禁用后请求状态码 = %d, want 200", thirdRecorder.Code)
	}
}

// BenchmarkSlidingWindowAllow 测量滑动窗口判断开销。
func BenchmarkSlidingWindowAllow(b *testing.B) {
	sliding := NewSlidingWindowLimiter(0, 10, time.Minute)

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		sliding.Allow()
	}
}
