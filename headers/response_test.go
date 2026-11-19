package headers

import (
	"net/http"
	"testing"
)

// TestResponseHeaders 验证响应头的字面值与可用性。
func TestResponseHeaders(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "STATUS", actual: STATUS, expected: "Status"},
		{name: "SERVER", actual: SERVER, expected: "Server"},
		{name: "SET_COOKIE", actual: SET_COOKIE, expected: "Set-Cookie"},
		{name: "LOCATION", actual: LOCATION, expected: "Location"},
		{name: "E_TAG", actual: E_TAG, expected: "ETag"},
		{name: "LAST_MODIFIED", actual: LAST_MODIFIED, expected: "Last-Modified"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.actual != testCase.expected {
				t.Fatalf("%s = %q, want %q", testCase.name, testCase.actual, testCase.expected)
			}

			assertHeaderRoundTrip(t, testCase.name, testCase.actual)
		})
	}
}

// TestSetCookieHeaderParsable 验证 SET_COOKIE 能被标准库正确解析。
//
// 只有常量与 net/http 的解析约定完全一致时，写入的 Cookie 才能被客户端识别。
func TestSetCookieHeaderParsable(t *testing.T) {
	header := make(http.Header)
	header.Add(SET_COOKIE, "sid=abc; Path=/; HttpOnly")

	response := http.Response{Header: header}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie 解析数量 = %d, want 1", len(cookies))
	}

	if cookies[0].Name != "sid" || cookies[0].Value != "abc" || !cookies[0].HttpOnly {
		t.Fatalf("Set-Cookie 解析结果异常: %+v", cookies[0])
	}
}
