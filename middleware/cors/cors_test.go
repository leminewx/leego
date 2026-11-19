package cors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
)

// performCORS 执行一次经过 CORS 中间件的请求。
//
// 返回值中的 called 表示业务处理函数是否被执行。
func performCORS(cors *CORS, req *http.Request) (*httptest.ResponseRecorder, bool) {
	called := false
	handle := cors.GetMiddleware()(func(ctx *leego.Context) {
		called = true
		_ = ctx.ResponseText(http.StatusOK, "ok")
	})

	recorder := httptest.NewRecorder()
	handle(leego.NewContext(recorder, req))
	return recorder, called
}

// createPreflightRequest 构造一个 CORS 预检请求。
func createPreflightRequest(origin, method, requestHeaders string) *http.Request {
	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", method)
	if requestHeaders != "" {
		req.Header.Set("Access-Control-Request-Headers", requestHeaders)
	}

	return req
}

// TestNewDefaultCORS 验证默认配置。
func TestNewDefaultCORS(t *testing.T) {
	cors := NewDefaultCORS()
	if !cors.IsEnabled() {
		t.Fatal("默认跨域中间件必须处于启用状态")
	}

	config := cors.GetConfig()
	if len(config.AllowOrigins) != 1 || config.AllowOrigins[0] != "*" {
		t.Fatalf("默认来源白名单 = %v, want [*]", config.AllowOrigins)
	}

	if config.AllowCredentials {
		t.Fatal("允许任意来源时不得默认开启凭证")
	}

	if config.MaxAgeSeconds != 86400 {
		t.Fatalf("默认 MaxAge = %d, want 86400", config.MaxAgeSeconds)
	}

	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		found := false
		for _, allowed := range config.AllowMethods {
			if allowed == method {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("默认方法白名单缺少 %s: %v", method, config.AllowMethods)
		}
	}
}

// TestSimpleRequestWithWildcardOrigin 验证通配符来源的普通请求。
func TestSimpleRequestWithWildcardOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Origin", "https://any.example")

	recorder, called := performCORS(NewDefaultCORS(), req)

	if !called {
		t.Fatal("普通跨域请求必须放行到业务处理函数")
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}

	// 通配符源的响应不随 Origin 变化，不需要 Vary。
	if vary := recorder.Header().Values("Vary"); len(vary) != 0 {
		t.Fatalf("通配符源不应写入 Vary: %v", vary)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("通配符源不得下发凭证头，实际 = %q", got)
	}
}

// TestSimpleRequestWithSpecificOrigin 验证白名单来源与凭证。
func TestSimpleRequestWithSpecificOrigin(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins("https://a.example")
	cors.SetAllowCredentials(true)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Origin", "https://a.example")

	recorder, called := performCORS(cors, req)
	if !called {
		t.Fatal("白名单来源必须放行")
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example" {
		t.Fatalf("Allow-Origin = %q, want https://a.example", got)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials = %q, want true", got)
	}

	if !strings.Contains(strings.Join(recorder.Header().Values("Vary"), ","), "Origin") {
		t.Fatalf("具体来源必须写入 Vary: Origin，实际 = %v", recorder.Header().Values("Vary"))
	}
}

// TestSimpleRequestWithDisallowedOrigin 验证不在白名单的来源。
func TestSimpleRequestWithDisallowedOrigin(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins("https://a.example")

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Origin", "https://b.example")

	recorder, called := performCORS(cors, req)

	// 普通请求由浏览器拦截响应，中间件不主动拒绝，避免误伤同源请求。
	if !called {
		t.Fatal("普通请求仍应进入业务处理函数")
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("不在白名单的来源不得下发 Allow-Origin，实际 = %q", got)
	}
}

// TestPreflightAllowed 验证合法预检请求。
func TestPreflightAllowed(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins("https://a.example")
	cors.SetAllowMethods("POST")
	cors.SetAllowHeaders("X-Token")

	recorder, called := performCORS(cors, createPreflightRequest("https://a.example", "post", "X-Token"))

	if called {
		t.Fatal("预检请求不得进入业务处理函数")
	}

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("状态码 = %d, want 204", recorder.Code)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example" {
		t.Fatalf("Allow-Origin = %q", got)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Methods"); got != "POST" {
		t.Fatalf("Allow-Methods = %q, want POST", got)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Headers"); got != "X-Token" {
		t.Fatalf("Allow-Headers = %q, want X-Token", got)
	}

	if got := recorder.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Fatalf("Max-Age = %q, want 86400", got)
	}

	vary := strings.Join(recorder.Header().Values("Vary"), ",")
	for _, dimension := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !strings.Contains(vary, dimension) {
			t.Fatalf("Vary 缺少 %s: %v", dimension, recorder.Header().Values("Vary"))
		}
	}
}

// TestPreflightEchoesRequestHeaders 验证未配置请求头白名单时回显请求头。
func TestPreflightEchoesRequestHeaders(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowMethods(http.MethodPost)

	recorder, _ := performCORS(cors, createPreflightRequest("https://a.example", http.MethodPost, "X-Custom, X-Trace"))

	if got := recorder.Header().Get("Access-Control-Allow-Headers"); got != "X-Custom, X-Trace" {
		t.Fatalf("Allow-Headers = %q, want 回显请求头", got)
	}
}

// TestPreflightRejectedOrigin 验证来源被拒的预检请求。
func TestPreflightRejectedOrigin(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins("https://a.example")

	recorder, called := performCORS(cors, createPreflightRequest("https://b.example", http.MethodGet, ""))

	if called {
		t.Fatal("被拒的预检请求不得进入业务处理函数")
	}

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d, want 403", recorder.Code)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("被拒来源不得下发 Allow-Origin，实际 = %q", got)
	}
}

// TestPreflightRejectedMethod 验证方法被拒的预检请求。
func TestPreflightRejectedMethod(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowMethods(http.MethodPost)

	recorder, called := performCORS(cors, createPreflightRequest("https://a.example", http.MethodDelete, ""))

	if called {
		t.Fatal("被拒的预检请求不得进入业务处理函数")
	}

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, want 405", recorder.Code)
	}
}

// TestPlainOptionsPassThrough 验证非预检 OPTIONS 请求正常放行。
func TestPlainOptionsPassThrough(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req.Header.Set("Origin", "https://a.example")

	recorder, called := performCORS(NewDefaultCORS(), req)
	if !called {
		t.Fatal("普通 OPTIONS 请求必须进入业务处理函数")
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("普通 OPTIONS 仍应写入简单响应头，实际 = %q", got)
	}
}

// TestDisabledCORS 验证禁用后不写入任何 CORS 头。
func TestDisabledCORS(t *testing.T) {
	cors := NewDefaultCORS()
	cors.Disable()

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Origin", "https://a.example")

	recorder, called := performCORS(cors, req)
	if !called {
		t.Fatal("禁用后必须直接放行")
	}

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("禁用后不得写入 CORS 头，实际 = %q", got)
	}
}

// TestExposeHeaders 验证暴露响应头。
func TestExposeHeaders(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetExposeHeaders("X-Total", "x-total")

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Origin", "https://a.example")

	recorder, _ := performCORS(cors, req)
	if got := recorder.Header().Get("Access-Control-Expose-Headers"); got != "X-Total" {
		t.Fatalf("Expose-Headers = %q, want X-Total", got)
	}
}

// TestWildcardOriginForcesNoCredentials 验证通配符源与凭证互斥。
func TestWildcardOriginForcesNoCredentials(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins("*")
	cors.SetAllowCredentials(true)

	if cors.GetConfig().AllowCredentials {
		t.Fatal("允许任意来源时凭证设置必须被忽略")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://a.example")

	recorder, _ := performCORS(cors, req)
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("通配符源不得下发凭证头，实际 = %q", got)
	}
}

// TestAppendVary 验证 Vary 追加去重。
func TestAppendVary(t *testing.T) {
	header := http.Header{}
	header.Set("Vary", "Accept-Encoding")

	appendVary(header, "Origin")
	appendVary(header, "Origin")
	appendVary(header, "Access-Control-Request-Method")

	values := strings.Join(header.Values("Vary"), ", ")
	for _, dimension := range []string{"Accept-Encoding", "Origin", "Access-Control-Request-Method"} {
		if !strings.Contains(values, dimension) {
			t.Fatalf("Vary 缺少 %s: %q", dimension, values)
		}
	}

	// Origin 只能出现一次。
	if count := strings.Count(values, "Origin"); count != 1 {
		t.Fatalf("Vary 中 Origin 出现 %d 次, want 1: %q", count, values)
	}
}
