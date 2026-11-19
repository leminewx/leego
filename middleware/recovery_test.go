package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
)

// TestRecoverHandlesPanic 验证 panic 被捕获并返回 500。
func TestRecoverHandlesPanic(t *testing.T) {
	output := &bytes.Buffer{}
	handle := RecoverWithConfig(&RecoveryConfig{Output: output, DumpRequest: true})(func(ctx *leego.Context) {
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
	handle := RecoverWithConfig(&RecoveryConfig{Output: output})(func(ctx *leego.Context) {
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
	handle := RecoverWithConfig(&RecoveryConfig{
		Output: &bytes.Buffer{},
		HandleFunc: func(ctx *leego.Context, err any) {
			captured = err
			_ = ctx.ResponseText(http.StatusBadGateway, "recovered")
		},
	})(func(ctx *leego.Context) {
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
	handle := RecoverWithConfig(&RecoveryConfig{Output: &bytes.Buffer{}})(func(ctx *leego.Context) {
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
	handle := RecoverWithConfig(nil)(func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusNoContent, "")
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil)))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d, want 204", recorder.Code)
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
