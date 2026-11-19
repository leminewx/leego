// Package testutil 提供不依赖网络端口的 HTTP 测试工具。
//
// 设计目标：
//   - 直接驱动 http.Handler，省去 httptest.Server 的监听与端口分配；
//   - 以链式断言描述期望，失败信息包含状态码与响应体，便于定位；
//   - 不引入任何第三方库，也不依赖 leego 根包，可被任意 net/http 项目复用。
package testutil

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Client 包装一个 http.Handler，用于在进程内发起请求。
//
// Client 保存的默认请求头与 Cookie 会被复制到每次请求，因此可以安全地
// 在多个 goroutine 中共用一个 Client 发起只读请求。
type Client struct {
	// handler 为被测处理器。
	handler http.Handler
	// headers 为每次请求都会携带的默认请求头。
	headers http.Header
	// cookies 为每次请求都会携带的默认 Cookie。
	cookies []*http.Cookie
}

// NewClient 基于处理器创建测试客户端。
//
// handler 为 nil 时使用返回 500 的占位处理器，避免调用方在初始化阶段 panic。
func NewClient(handler http.Handler) *Client {
	if handler == nil {
		handler = http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			http.Error(resp, "testutil: nil handler", http.StatusInternalServerError)
		})
	}

	return &Client{handler: handler, headers: make(http.Header)}
}

// UseHeader 设置默认请求头并返回自身，便于链式调用。
//
// 同名请求头会被覆盖；传入空 key 时忽略。
func (own *Client) UseHeader(key, value string) *Client {
	if key == "" {
		return own
	}

	own.headers.Set(key, value)
	return own
}

// UseCookie 追加一个默认 Cookie 并返回自身。
func (own *Client) UseCookie(cookie *http.Cookie) *Client {
	if cookie == nil {
		return own
	}

	own.cookies = append(own.cookies, cookie)
	return own
}

// Get 发起 GET 请求。
func (own *Client) Get(path string, options ...RequestOption) *Response {
	return own.send(http.MethodGet, path, nil, options...)
}

// Head 发起 HEAD 请求。
func (own *Client) Head(path string, options ...RequestOption) *Response {
	return own.send(http.MethodHead, path, nil, options...)
}

// Post 发起 POST 请求；body 按原样写入，不推断 Content-Type。
func (own *Client) Post(path string, body []byte, options ...RequestOption) *Response {
	return own.send(http.MethodPost, path, body, options...)
}

// Put 发起 PUT 请求。
func (own *Client) Put(path string, body []byte, options ...RequestOption) *Response {
	return own.send(http.MethodPut, path, body, options...)
}

// Patch 发起 PATCH 请求。
func (own *Client) Patch(path string, body []byte, options ...RequestOption) *Response {
	return own.send(http.MethodPatch, path, body, options...)
}

// Delete 发起 DELETE 请求。
func (own *Client) Delete(path string, options ...RequestOption) *Response {
	return own.send(http.MethodDelete, path, nil, options...)
}

// SendJSON 以 application/json 发送请求。
//
// body 为 nil 时不写请求体；否则使用 json.Marshal 序列化，失败时返回
// 一个包含错误说明的伪响应，避免让测试代码额外处理错误分支。
func (own *Client) SendJSON(method, path string, body any, options ...RequestOption) *Response {
	payload, err := marshalJSON(body)
	if err != nil {
		return newFailureResponse(err)
	}

	options = append([]RequestOption{WithHeader("Content-Type", "application/json; charset=utf-8")}, options...)
	return own.send(method, path, payload, options...)
}

// Do 执行一个已经构造好的请求。
func (own *Client) Do(req *http.Request) *Response {
	recorder := httptest.NewRecorder()
	own.handler.ServeHTTP(recorder, req)
	return newResponse(recorder)
}

// send 构造请求并交给处理器执行。
func (own *Client) send(method, path string, body []byte, options ...RequestOption) *Response {
	config := newRequestConfig(own)
	for _, option := range options {
		if option != nil {
			option(config)
		}
	}

	req, err := config.build(method, path, body)
	if err != nil {
		return newFailureResponse(err)
	}

	return own.Do(req)
}

// buildURL 将相对路径与查询参数合并为绝对 URL。
//
// httptest.NewRequest 只接受绝对路径或完整 URL，这里统一补上占位主机，
// 保证 path 中带不带前导斜杠都能工作。
func buildURL(path string, query url.Values) (*url.URL, error) {
	target := strings.TrimSpace(path)
	if target == "" {
		target = "/"
	}

	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}

		target = "http://testutil.local" + target
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	merged := parsed.Query()
	for key, values := range query {
		for _, value := range values {
			merged.Add(key, value)
		}
	}

	parsed.RawQuery = merged.Encode()
	return parsed, nil
}
