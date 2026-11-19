package headers

import "testing"

// TestRequestHeaders 验证请求头的字面值与可用性。
func TestRequestHeaders(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "HOST", actual: HOST, expected: "Host"},
		{name: "USER_AGENT", actual: USER_AGENT, expected: "User-Agent"},
		{name: "ACCEPT", actual: ACCEPT, expected: "Accept"},
		{name: "ACCEPT_ENCODING", actual: ACCEPT_ENCODING, expected: "Accept-Encoding"},
		{name: "ACCEPT_LANGUAGE", actual: ACCEPT_LANGUAGE, expected: "Accept-Language"},
		{name: "AUTHORIZATION", actual: AUTHORIZATION, expected: "Authorization"},
		{name: "COOKIE", actual: COOKIE, expected: "Cookie"},
		{name: "REFERER", actual: REFERER, expected: "Referer"},
		{name: "RANGE", actual: RANGE, expected: "Range"},
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
