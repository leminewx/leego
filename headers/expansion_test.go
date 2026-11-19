package headers

import (
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

// assertHeaderRoundTrip 验证头部常量可被 net/http 规范化并往返读写。
//
// 常量值必须已经是规范形式（除首字母缩写的大小写外），否则写入时会被
// 规范化成另一个键，读取方按常量取值时就会出现"写进去读不到"。
func assertHeaderRoundTrip(t *testing.T, name, value string) {
	t.Helper()

	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		t.Fatalf("%s = %q 不是合法的头部名称", name, value)
	}

	if canonical := textproto.CanonicalMIMEHeaderKey(value); !strings.EqualFold(canonical, value) {
		t.Fatalf("%s = %q 不是规范形式，规范化后为 %q", name, value, canonical)
	}

	header := make(http.Header)
	header.Set(value, "demo")
	if got := header.Get(value); got != "demo" {
		t.Fatalf("%s = %q 往返读写失败: %q", name, value, got)
	}

	if len(header) != 1 {
		t.Fatalf("%s = %q 写入后产生了 %d 个键", name, value, len(header))
	}
}

// TestExpansionHeaders 验证拓展头的字面值与可用性。
func TestExpansionHeaders(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "X_REAL_IP", actual: X_REAL_IP, expected: "X-Real-IP"},
		{name: "X_FORWARDED_FOR", actual: X_FORWARDED_FOR, expected: "X-Forwarded-For"},
		{name: "X_REQUESTED_WITH", actual: X_REQUESTED_WITH, expected: "X-Requested-With"},
		{name: "X_REQUEST_ID", actual: X_REQUEST_ID, expected: "X-Request-ID"},
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
