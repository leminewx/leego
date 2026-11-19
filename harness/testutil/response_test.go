package testutil

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

// TestNewRequestConfigCopiesDefaults 验证默认请求头与 Cookie 被深拷贝。
func TestNewRequestConfigCopiesDefaults(t *testing.T) {
	client := &Client{
		headers: http.Header{"X-Base": {"1"}},
		cookies: []*http.Cookie{{Name: "sid", Value: "a"}},
	}

	config := newRequestConfig(client)
	config.headers.Set("X-Base", "changed")
	config.headers.Set("X-Extra", "2")

	if got := client.headers.Get("X-Base"); got != "1" {
		t.Fatalf("默认请求头被修改: %q", got)
	}

	if got := client.headers.Get("X-Extra"); got != "" {
		t.Fatalf("单次请求头泄漏到客户端默认值: %q", got)
	}

	if len(config.cookies) != 1 || config.cookies[0].Name != "sid" {
		t.Fatalf("默认 Cookie 未复制: %+v", config.cookies)
	}
}

// TestRequestConfigBuildMergesOptions 验证请求头、Cookie、查询串与请求体合并。
func TestRequestConfigBuildMergesOptions(t *testing.T) {
	client := NewClient(nil)
	client.headers.Set("X-Base", "1")

	config := newRequestConfig(client)
	WithHeader("X-Request", "2")(config)
	WithHeader("", "ignored")(config)
	WithCookie(&http.Cookie{Name: "sid", Value: "a"})(config)
	WithCookie(nil)(config)
	WithQuery("q", "hello world")(config)
	WithQuery("", "ignored")(config)
	WithRawQuery("page=2&page=3")(config)
	WithRawQuery("://bad")(config)

	request, err := config.build(http.MethodGet, "/items", []byte("body"))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}

	if request.Header.Get("X-Base") != "1" || request.Header.Get("X-Request") != "2" {
		t.Fatalf("请求头异常: %v", request.Header)
	}

	cookie, err := request.Cookie("sid")
	if err != nil || cookie.Value != "a" {
		t.Fatalf("Cookie 异常: %v %v", cookie, err)
	}

	query := request.URL.Query()
	if query.Get("q") != "hello world" {
		t.Fatalf("查询参数 q = %q", query.Get("q"))
	}

	if pages := query["page"]; len(pages) != 2 {
		t.Fatalf("查询参数 page = %v", pages)
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("读取请求体失败: %v", err)
	}

	if string(body) != "body" {
		t.Fatalf("请求体 = %q", body)
	}
}

// TestNewFailureResponseCarriesError 验证失败响应只携带错误信息。
func TestNewFailureResponseCarriesError(t *testing.T) {
	cause := errors.New("boom")
	response := newFailureResponse(cause)

	if response.Err != cause || response.Status != 0 {
		t.Fatalf("失败响应异常: %+v", response)
	}

	if response.Header == nil {
		t.Fatal("失败响应仍应提供非 nil 响应头")
	}

	if response.String() != "" {
		t.Fatalf("失败响应体应为空: %q", response.String())
	}
}

// TestResponseStringHandlesNil 验证 nil 响应不会 panic。
func TestResponseStringHandlesNil(t *testing.T) {
	var response *Response
	if response.String() != "" {
		t.Fatalf("nil 响应体 = %q", response.String())
	}
}
