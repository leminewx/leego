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

// TestNewTokenBucketLimiter 验证构造参数与默认值。
func TestNewTokenBucketLimiter(t *testing.T) {
	standard := NewTokenBucketLimiter(5, 10)

	if !standard.IsEnabled() {
		t.Fatal("限流器创建后必须处于启用状态")
	}

	if standard.GetLimit() != 5 {
		t.Fatalf("limit = %d, want 5", standard.GetLimit())
	}

	if standard.GetBurst() != 10 {
		t.Fatalf("burst = %d, want 10", standard.GetBurst())
	}

	// 容量小于 1 时按 1 处理：容量为 0 的令牌桶永远无法放行，
	// 若直接采信调用方传入的 0，会静默地把服务打成全量拒绝。
	fallback := NewTokenBucketLimiter(5, 0)
	if fallback.GetBurst() != 1 {
		t.Fatalf("非法容量应回退为 1，实际 = %d", fallback.GetBurst())
	}
}

// TestTokenBucketAllow 验证令牌耗尽后的拒绝。
func TestTokenBucketAllow(t *testing.T) {
	token := NewTokenBucketLimiter(1, 2)

	if !token.Allow() {
		t.Fatal("第 1 个令牌必须放行")
	}

	if !token.Allow() {
		t.Fatal("第 2 个令牌必须放行")
	}

	if token.Allow() {
		t.Fatal("令牌耗尽后 Allow() 必须为 false")
	}
}

// TestTokenBucketUnlimited 验证非正数速率表示不限流。
func TestTokenBucketUnlimited(t *testing.T) {
	// 速率小于等于 0 映射为 rate.Inf（不限流），而不是“一个令牌也不发”。
	token := NewTokenBucketLimiter(0, 1)

	for index := 0; index < 1000; index++ {
		if !token.Allow() {
			t.Fatalf("不限流时第 %d 次 Allow() = false", index+1)
		}
	}
}

// TestTokenBucketSetLimit 验证热更新速率。
func TestTokenBucketSetLimit(t *testing.T) {
	token := NewTokenBucketLimiter(1, 1)
	_ = token.Allow()

	// 提升速率后，等待一个令牌的时间从 1 秒缩短到 10 毫秒。
	token.SetLimit(100)
	if token.GetLimit() != 100 {
		t.Fatalf("limit = %d, want 100", token.GetLimit())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if !token.AllowWithTimeout(ctx, 500*time.Millisecond) {
		t.Fatal("提升速率后应尽快补充令牌并放行")
	}

	token.SetLimit(0)
	if !token.Allow() {
		t.Fatal("速率归零后应视为不限流")
	}
}

// TestTokenBucketSetBurst 验证热更新容量。
//
// 注意：扩大容量只抬高“能存多少令牌”的上限，并不会凭空补入令牌，
// 因此扩容后仍需等待令牌按速率补充。
func TestTokenBucketSetBurst(t *testing.T) {
	token := NewTokenBucketLimiter(1, 1)
	_ = token.Allow()

	if token.Allow() {
		t.Fatal("容量为 1 且已消耗令牌时必须拒绝")
	}

	token.SetBurst(3)
	if token.GetBurst() != 3 {
		t.Fatalf("burst = %d, want 3", token.GetBurst())
	}

	token.SetLimit(100)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if !token.AllowWithTimeout(ctx, 500*time.Millisecond) {
		t.Fatal("令牌补充后应放行")
	}

	token.SetBurst(0)
	if token.GetBurst() != 1 {
		t.Fatalf("非法容量应回退为 1，实际 = %d", token.GetBurst())
	}
}

// TestTokenBucketGetConfig 验证配置读取。
func TestTokenBucketGetConfig(t *testing.T) {
	token := NewTokenBucketLimiter(7, 3)

	limit, burst := token.GetConfig()
	if limit != 7 || burst != 3 {
		t.Fatalf("GetConfig() = (%d, %d), want (7, 3)", limit, burst)
	}
}

// TestTokenBucketAllowWithTimeout 验证等待期间令牌补充后放行。
func TestTokenBucketAllowWithTimeout(t *testing.T) {
	token := NewTokenBucketLimiter(100, 1)
	_ = token.Allow()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !token.AllowWithTimeout(ctx, time.Second) {
		t.Fatal("令牌补充后 AllowWithTimeout 应放行")
	}
}

// TestTokenBucketWait 验证阻塞等待成功。
func TestTokenBucketWait(t *testing.T) {
	token := NewTokenBucketLimiter(100, 1)
	_ = token.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

// TestTokenBucketWaitCanceled 验证上下文结束时等待失败。
func TestTokenBucketWaitCanceled(t *testing.T) {
	token := NewTokenBucketLimiter(1, 1)
	_ = token.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := token.Wait(ctx); err == nil {
		t.Fatal("上下文超时后 Wait() 必须返回错误")
	}
}

// TestTokenBucketEnableDisable 验证启停开关。
func TestTokenBucketEnableDisable(t *testing.T) {
	token := NewTokenBucketLimiter(1, 1)

	token.Disable()
	if token.IsEnabled() {
		t.Fatal("Disable 后必须为禁用状态")
	}

	token.Enable()
	if !token.IsEnabled() {
		t.Fatal("Enable 后必须为启用状态")
	}
}

// TestTokenBucketMiddleware 验证中间件在令牌耗尽时返回 429，
// 并在禁用后直接放行。
func TestTokenBucketMiddleware(t *testing.T) {
	token := NewTokenBucketLimiter(1, 1)

	called := 0
	handle := token.GetMiddleware(0)(func(ctx *leego.Context) {
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

	token.Disable()

	thirdRecorder := httptest.NewRecorder()
	handle(leego.NewContext(thirdRecorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if thirdRecorder.Code != http.StatusOK {
		t.Fatalf("禁用后请求状态码 = %d, want 200", thirdRecorder.Code)
	}
}

// TestTokenBucketConcurrency 验证并发下放行量不超过桶容量。
//
// 令牌桶的容量是并发下的硬上界；测试执行时间远小于 1 秒，
// 因此最多只会额外补充 1 个令牌，允许该 1 个令牌的误差。
func TestTokenBucketConcurrency(t *testing.T) {
	const burst = 200
	const workers = 20
	const attemptsPerWorker = 20

	token := NewTokenBucketLimiter(1, burst)

	var allowed atomic.Int64
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for attempt := 0; attempt < attemptsPerWorker; attempt++ {
				if token.Allow() {
					allowed.Add(1)
				}
			}
		}()
	}

	waitGroup.Wait()

	got := allowed.Load()
	if got < burst || got > burst+1 {
		t.Fatalf("放行数量 = %d, want %d（允许补充 1 个令牌的误差）", got, burst)
	}
}

// BenchmarkTokenBucketAllow 测量令牌桶判断开销。
func BenchmarkTokenBucketAllow(b *testing.B) {
	token := NewTokenBucketLimiter(0, 1)

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		token.Allow()
	}
}
