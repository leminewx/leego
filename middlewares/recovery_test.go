package middlewares

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
)

// TestRecoverHandlesPanic 验证 panic 被捕获并返回 500。
func TestRecoverHandlesPanic(t *testing.T) {
	output := &bytes.Buffer{}
	handle := NewRecoveryWithConfig(&RecoveryConfig{Output: output, DumpRequest: true}).AdaptToMiddleware()(func(ctx *leego.Context) {
		panic("boom")
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil)))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want 500", recorder.Code)
	}

	// 默认响应体由 response.GetStatusLowerText 生成，形如 "500 internal server error"。
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "internal server error") {
		t.Fatalf("响应体 = %q", recorder.Body.String())
	}

	logged := output.String()
	if !strings.Contains(logged, "panic recovered") || !strings.Contains(logged, "boom") {
		t.Fatalf("日志缺少 panic 信息: %q", logged)
	}

	if !strings.Contains(logged, "GET /panic") {
		t.Fatalf("日志缺少请求行: %q", logged)
	}
}

// TestRecoverWithoutDumpRequest 验证关闭请求转储后的日志。
func TestRecoverWithoutDumpRequest(t *testing.T) {
	output := &bytes.Buffer{}
	handle := NewRecoveryWithConfig(&RecoveryConfig{Output: output}).AdaptToMiddleware()(func(ctx *leego.Context) {
		panic(http.ErrAbortHandler)
	})

	handle(leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/boom", nil)))

	logged := output.String()
	if !strings.Contains(logged, "panic recovered") {
		t.Fatalf("日志缺少 panic 信息: %q", logged)
	}

	if strings.Contains(logged, "POST /boom") {
		t.Fatalf("关闭 DumpRequest 后不应输出请求行: %q", logged)
	}
}

// TestRecoverCallsHandleFunc 验证自定义善后函数。
func TestRecoverCallsHandleFunc(t *testing.T) {
	var captured any
	handle := NewRecoveryWithConfig(&RecoveryConfig{
		Output: &bytes.Buffer{},
		HandleFunc: func(ctx *leego.Context, err any) {
			captured = err
			_ = ctx.ResponseText(http.StatusBadGateway, "recovered")
		},
	}).AdaptToMiddleware()(func(ctx *leego.Context) {
		panic("custom")
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if captured != "custom" {
		t.Fatalf("HandleFunc 捕获值 = %v, want custom", captured)
	}

	// 善后函数已写响应，恢复中间件不得再写 500。
	if recorder.Code != http.StatusBadGateway || recorder.Body.String() != "recovered" {
		t.Fatalf("响应 = (%d, %q)", recorder.Code, recorder.Body.String())
	}
}

// TestRecoverKeepsWrittenResponse 验证已写入的响应不会被覆盖。
func TestRecoverKeepsWrittenResponse(t *testing.T) {
	handle := NewRecoveryWithConfig(&RecoveryConfig{Output: &bytes.Buffer{}}).AdaptToMiddleware()(func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusOK, "partial")
		panic("after write")
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "partial" {
		t.Fatalf("响应 = (%d, %q), want (200, partial)", recorder.Code, recorder.Body.String())
	}
}

// TestRecoverPassesThrough 验证未发生 panic 时正常放行。
func TestRecoverPassesThrough(t *testing.T) {
	handle := NewRecoveryWithConfig(nil).AdaptToMiddleware()(func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusNoContent, "")
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d, want 204", recorder.Code)
	}
}

// TestRecoverRedactsSensitiveHeaders 验证请求转储不会泄露凭据。
//
// httputil.DumpRequest 会把 Authorization/Cookie 等头原文写进日志；
// 恢复中间件必须基于副本脱敏后再转储，且不修改原始请求。
func TestRecoverRedactsSensitiveHeaders(t *testing.T) {
	output := &bytes.Buffer{}
	handle := NewRecoveryWithConfig(&RecoveryConfig{Output: output, DumpRequest: true}).AdaptToMiddleware()(func(*leego.Context) {
		// 使用非字符串 panic 值，同时验证 %v 能正确格式化任意值。
		panic(errors.New("boom"))
	})

	request := httptest.NewRequest(http.MethodGet, "/secure", nil)
	request.Header.Set("Authorization", "Bearer super-secret-token")
	request.Header.Set("Cookie", "session=top-secret-cookie")
	request.Header.Set("X-Api-Key", "api-key-value")

	handle(leego.NewContext(httptest.NewRecorder(), request))

	logged := output.String()
	for _, secret := range []string{"super-secret-token", "top-secret-cookie", "api-key-value"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("日志泄露敏感值 %q: %s", secret, logged)
		}
	}

	if !strings.Contains(logged, "[REDACTED]") {
		t.Fatalf("日志缺少脱敏占位符: %s", logged)
	}

	if !strings.Contains(logged, "boom") {
		t.Fatalf("日志缺少 panic 值: %s", logged)
	}

	// 脱敏只能基于副本，不能改写调用方请求。
	if got := request.Header.Get("Authorization"); got != "Bearer super-secret-token" {
		t.Fatalf("原始请求头被改写 = %q", got)
	}
}

// TestGetSourceAndFunction 验证堆栈辅助函数。
func TestGetSourceAndFunction(t *testing.T) {
	lines := [][]byte{[]byte("first"), []byte("  second  ")}

	if got := string(getSource(lines, 2)); got != "second" {
		t.Fatalf("getSource(lines, 2) = %q, want second", got)
	}

	if got := string(getSource(lines, 0)); got != "???" {
		t.Fatalf("越界行号 = %q, want ???", got)
	}

	if got := string(getSource(lines, 10)); got != "???" {
		t.Fatalf("越界行号 = %q, want ???", got)
	}

	if got := getFunction(0); got != "???" {
		t.Fatalf("getFunction(0) = %q, want ???", got)
	}
}

// TestCaptureTraceContainsCaller 验证调用栈包含当前测试函数。
func TestCaptureTraceContainsCaller(t *testing.T) {
	output := string(captureTrace(0))
	if !strings.Contains(output, "TestCaptureTraceContainsCaller") {
		t.Fatalf("调用栈未包含测试函数: %q", output)
	}
}
