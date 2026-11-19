package headers

import "testing"

// TestUniversalHeaders 验证通用头的字面值与可用性。
func TestUniversalHeaders(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "DATE", actual: DATE, expected: "Date"},
		{name: "CACHE_CONTROL", actual: CACHE_CONTROL, expected: "Cache-Control"},
		{name: "CONNECTION", actual: CONNECTION, expected: "Connection"},
		{name: "UPGRADE", actual: UPGRADE, expected: "Upgrade"},
		{name: "ALLOW", actual: ALLOW, expected: "Allow"},
		{name: "VIA", actual: VIA, expected: "Via"},
		{name: "CONTENT_TYPE", actual: CONTENT_TYPE, expected: "Content-Type"},
		{name: "CONTENT_LENGTH", actual: CONTENT_LENGTH, expected: "Content-Length"},
		{name: "CONTENT_RANGE", actual: CONTENT_RANGE, expected: "Content-Range"},
		{name: "CONTENT_DISPOSITION", actual: CONTENT_DISPOSITION, expected: "Content-Disposition"},
		{name: "CONTENT_ENCODING", actual: CONTENT_ENCODING, expected: "Content-Encoding"},
		{name: "TRANSFER_ENCODING", actual: TRANSFER_ENCODING, expected: "Transfer-Encoding"},
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
