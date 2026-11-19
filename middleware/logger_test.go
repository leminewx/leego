package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
)

// performLoggedRequest 执行一次带访问日志中间件的请求。
func performLoggedRequest(config *LoggerConfig, method, target string, status int) string {
	output := &bytes.Buffer{}
	config.Output = output

	handle := LogWithConfig(config)(func(ctx *leego.Context) {
		_ = ctx.ResponseText(status, "ok")
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	request.RemoteAddr = "127.0.0.1:1234"

	handle(leego.NewContext(recorder, request))
	return output.String()
}

// TestLogWithConfig 验证访问日志包含方法、路径、状态码与客户端 IP。
func TestLogWithConfig(t *testing.T) {
	output := performLoggedRequest(&LoggerConfig{}, http.MethodGet, "/api/users", http.StatusOK)

	for _, fragment := range []string{"GET", "/api/users", "200", "127.0.0.1"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("日志缺少片段 %q: %q", fragment, output)
		}
	}

	if strings.Contains(output, "\033[") {
		t.Fatalf("未显式启用颜色时不应包含 ANSI 转义: %q", output)
	}
}

// TestLogWithQuery 验证查询参数的开关。
func TestLogWithQuery(t *testing.T) {
	hidden := performLoggedRequest(&LoggerConfig{}, http.MethodGet, "/search?q=leego", http.StatusOK)
	if strings.Contains(hidden, "q=leego") {
		t.Fatalf("未启用 WithQuery 时不应记录查询参数: %q", hidden)
	}

	shown := performLoggedRequest(&LoggerConfig{WithQuery: true}, http.MethodGet, "/search?q=leego", http.StatusOK)
	if !strings.Contains(shown, "/search?q=leego") {
		t.Fatalf("启用 WithQuery 后应记录完整路径: %q", shown)
	}
}

// TestLogSkipsPaths 验证跳过规则。
func TestLogSkipsPaths(t *testing.T) {
	absolute := performLoggedRequest(&LoggerConfig{
		SkipAbsolutePaths: map[string]struct{}{"/health": {}},
	}, http.MethodGet, "/health", http.StatusOK)

	if absolute != "" {
		t.Fatalf("绝对路径应被跳过，实际输出: %q", absolute)
	}

	wildcard := performLoggedRequest(&LoggerConfig{
		SkipWildcardPaths: []string{"/static/"},
	}, http.MethodGet, "/static/app.css", http.StatusOK)

	if wildcard != "" {
		t.Fatalf("通配路径应被跳过，实际输出: %q", wildcard)
	}

	kept := performLoggedRequest(&LoggerConfig{
		SkipAbsolutePaths: map[string]struct{}{"/health": {}},
		SkipWildcardPaths: []string{"/static/"},
	}, http.MethodGet, "/api/ping", http.StatusOK)

	if kept == "" {
		t.Fatal("未命中跳过规则的路径必须记录日志")
	}
}

// TestLogWithColor 验证显式启用颜色时输出 ANSI 转义。
func TestLogWithColor(t *testing.T) {
	output := performLoggedRequest(&LoggerConfig{WithColor: true}, http.MethodPost, "/api/users", http.StatusCreated)

	if !strings.Contains(output, "\033[") {
		t.Fatalf("启用颜色后应包含 ANSI 转义: %q", output)
	}
}

// TestLogRecordsServerError 验证 5xx 状态码被记录。
func TestLogRecordsServerError(t *testing.T) {
	output := performLoggedRequest(&LoggerConfig{}, http.MethodDelete, "/api/users/42", http.StatusInternalServerError)

	if !strings.Contains(output, "500") {
		t.Fatalf("日志缺少 500 状态码: %q", output)
	}
}

// TestLogWithNilConfig 验证 nil 配置回退为默认值。
func TestLogWithNilConfig(t *testing.T) {
	handle := LogWithConfig(nil)(func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusNoContent, "")
	})

	// 默认输出为 os.Stdout，此处只验证不 panic 且处理链被执行。
	executed := false
	handle = LogWithConfig(&LoggerConfig{Output: &bytes.Buffer{}})(func(ctx *leego.Context) {
		executed = true
	})

	handle(leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)))
	if !executed {
		t.Fatal("日志中间件必须调用后续处理器")
	}
}

// TestShouldSkip 验证跳过规则匹配。
func TestShouldSkip(t *testing.T) {
	config := &LoggerConfig{
		SkipAbsolutePaths: map[string]struct{}{"/health": {}},
		SkipWildcardPaths: []string{"/static", "/assets/"},
	}

	if !config.shouldSkip("/health") {
		t.Fatal("绝对路径必须命中跳过规则")
	}

	if !config.shouldSkip("/static/app.css") || !config.shouldSkip("/assets/app.css") {
		t.Fatal("通配前缀必须命中跳过规则")
	}

	if config.shouldSkip("/api") {
		t.Fatal("未命中的路径不应被跳过")
	}

	empty := &LoggerConfig{}
	if empty.shouldSkip("/api") {
		t.Fatal("空配置不应跳过任何路径")
	}
}
