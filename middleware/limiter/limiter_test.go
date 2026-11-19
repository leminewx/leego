package limiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

// TestWaitForQuotaImmediate 验证配额充足时无需等待。
func TestWaitForQuotaImmediate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	calls := 0
	err := waitForQuota(ctx, func() bool {
		calls++
		return true
	})

	if err != nil {
		t.Fatalf("waitForQuota() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("allow 调用次数 = %d, want 1", calls)
	}
}

// TestWaitForQuotaWithoutDeadline 验证缺少截止时间的上下文被拒绝。
func TestWaitForQuotaWithoutDeadline(t *testing.T) {
	err := waitForQuota(context.Background(), func() bool { return false })
	if err != ErrNoDeadline {
		t.Fatalf("error = %v, want ErrNoDeadline", err)
	}

	// nil 上下文同样视为没有截止时间。
	if err := waitForQuota(nil, func() bool { return false }); err != ErrNoDeadline {
		t.Fatalf("nil ctx error = %v, want ErrNoDeadline", err)
	}
}

// TestWaitForQuotaEventuallyAllowed 验证轮询等待后可以拿到配额。
func TestWaitForQuotaEventuallyAllowed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var attempts atomic.Int64
	err := waitForQuota(ctx, func() bool {
		return attempts.Add(1) >= 3
	})

	if err != nil {
		t.Fatalf("waitForQuota() error = %v", err)
	}

	if attempts.Load() < 3 {
		t.Fatalf("尝试次数 = %d, want >= 3", attempts.Load())
	}
}

// TestWaitForQuotaContextCanceled 验证上下文结束时的错误。
func TestWaitForQuotaContextCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := waitForQuota(ctx, func() bool { return false })
	if err == nil {
		t.Fatal("waitForQuota() 必须返回上下文错误")
	}

	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("waitForQuota 应等待到上下文超时")
	}
}

// TestMiddlewareReturnsTooManyRequests 验证限流中间件返回 429 与 Retry-After。
func TestMiddlewareReturnsTooManyRequests(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, time.Minute)

	called := 0
	handle := fixed.GetMiddleware(0)(func(ctx *leego.Context) {
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
}

// TestMiddlewareBypassesWhenDisabled 验证禁用后直接放行。
func TestMiddlewareBypassesWhenDisabled(t *testing.T) {
	fixed := NewFixedWindowLimiter(1, time.Minute)
	fixed.Disable()

	called := 0
	handle := fixed.GetMiddleware(0)(func(ctx *leego.Context) {
		called++
		ctx.ResponseText(http.StatusOK, "ok")
	})

	for index := 0; index < 5; index++ {
		recorder := httptest.NewRecorder()
		handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

		if recorder.Code != http.StatusOK {
			t.Fatalf("禁用限流后第 %d 次请求状态码 = %d, want 200", index+1, recorder.Code)
		}
	}

	if called != 5 {
		t.Fatalf("业务处理函数调用次数 = %d, want 5", called)
	}
}
