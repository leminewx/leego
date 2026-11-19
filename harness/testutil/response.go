package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// RequestOption 修改单次请求的构造参数。
type RequestOption func(*requestConfig)

// requestConfig 保存一次请求的临时配置。
type requestConfig struct {
	// headers 为本次请求的请求头。
	headers http.Header
	// cookies 为本次请求的 Cookie。
	cookies []*http.Cookie
	// query 为追加到 URL 上的查询参数。
	query url.Values
}

// newRequestConfig 基于客户端的默认值创建请求配置。
//
// 默认请求头与 Cookie 会被深拷贝，避免单次请求的修改影响后续请求。
func newRequestConfig(client *Client) *requestConfig {
	config := &requestConfig{
		headers: make(http.Header, len(client.headers)),
		query:   make(url.Values),
	}

	for key, values := range client.headers {
		config.headers[key] = append([]string(nil), values...)
	}

	config.cookies = append(config.cookies, client.cookies...)
	return config
}

// WithHeader 为本次请求设置请求头。
func WithHeader(key, value string) RequestOption {
	return func(config *requestConfig) {
		if key == "" {
			return
		}

		config.headers.Set(key, value)
	}
}

// WithCookie 为本次请求追加 Cookie。
func WithCookie(cookie *http.Cookie) RequestOption {
	return func(config *requestConfig) {
		if cookie != nil {
			config.cookies = append(config.cookies, cookie)
		}
	}
}

// WithQuery 为本次请求追加查询参数；value 会按 URL 规范编码。
func WithQuery(key, value string) RequestOption {
	return func(config *requestConfig) {
		if key == "" {
			return
		}

		config.query.Add(key, value)
	}
}

// WithRawQuery 直接替换查询串，适合需要精确控制编码的场景。
//
// 与 WithQuery 同时使用时，两者会合并：RawQuery 先写入，再追加 WithQuery 的参数。
func WithRawQuery(raw string) RequestOption {
	return func(config *requestConfig) {
		values, err := url.ParseQuery(raw)
		if err != nil {
			return
		}

		for key, items := range values {
			for _, item := range items {
				config.query.Add(key, item)
			}
		}
	}
}

// build 根据配置构造请求对象。
func (own *requestConfig) build(method, path string, body []byte) (*http.Request, error) {
	target, err := buildURL(path, own.query)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, target.String(), reader)
	if err != nil {
		return nil, err
	}

	req.Host = target.Host
	for key, values := range own.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	for _, cookie := range own.cookies {
		req.AddCookie(cookie)
	}

	return req, nil
}

// Response 是一次进程内请求的结果快照。
type Response struct {
	// Status 为响应状态码。
	Status int
	// Header 为响应头。
	Header http.Header
	// Body 为完整响应体。
	Body []byte
	// Cookies 为响应中的 Set-Cookie 解析结果。
	Cookies []*http.Cookie
	// Err 为请求构造阶段发生的错误；非 nil 时其他字段无意义。
	Err error
}

// newResponse 从 httptest.ResponseRecorder 构建响应快照。
func newResponse(recorder *httptest.ResponseRecorder) *Response {
	if recorder == nil {
		return &Response{Status: http.StatusInternalServerError}
	}

	result := recorder.Result()
	defer func() { _ = result.Body.Close() }()

	body, _ := io.ReadAll(result.Body)
	return &Response{
		Status:  result.StatusCode,
		Header:  result.Header,
		Body:    body,
		Cookies: result.Cookies(),
	}
}

// newFailureResponse 构造一个只携带错误的伪响应。
func newFailureResponse(err error) *Response {
	return &Response{Status: 0, Header: make(http.Header), Err: err}
}

// String 返回响应体文本，便于日志输出。
func (own *Response) String() string {
	if own == nil {
		return ""
	}

	return string(own.Body)
}

// AssertStatus 断言状态码并返回自身，便于链式断言。
func (own *Response) AssertStatus(t *testing.T, want int) *Response {
	t.Helper()

	if own.Status != want {
		t.Fatalf("状态码 = %d, 期望 %d, 响应体 = %s", own.Status, want, own.String())
	}

	return own
}

// AssertHeader 断言响应头包含期望值。
func (own *Response) AssertHeader(t *testing.T, key, want string) *Response {
	t.Helper()

	if got := own.Header.Get(key); got != want {
		t.Fatalf("响应头 %s = %q, 期望 %q", key, got, want)
	}

	return own
}

// AssertContains 断言响应体包含指定子串。
func (own *Response) AssertContains(t *testing.T, want string) *Response {
	t.Helper()

	if !bytes.Contains(own.Body, []byte(want)) {
		t.Fatalf("响应体不包含 %q, 实际 = %s", want, own.String())
	}

	return own
}

// BindJSON 将响应体解码到 target；解码失败直接终止测试。
func (own *Response) BindJSON(t *testing.T, target any) {
	t.Helper()

	if err := json.Unmarshal(own.Body, target); err != nil {
		t.Fatalf("解析 JSON 失败: %v, 响应体 = %s", err, own.String())
	}
}

// GetCookie 返回指定名称的响应 Cookie。
func (own *Response) GetCookie(name string) (*http.Cookie, bool) {
	for _, cookie := range own.Cookies {
		if cookie.Name == name {
			return cookie, true
		}
	}

	return nil, false
}

// GetJSONMap 将响应体解码为 map，便于快速断言字段。
func (own *Response) GetJSONMap(t *testing.T) map[string]any {
	t.Helper()

	target := make(map[string]any)
	own.BindJSON(t, &target)
	return target
}

// marshalJSON 序列化请求体；nil 返回 nil 表示不写请求体。
func marshalJSON(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}

	return json.Marshal(body)
}
