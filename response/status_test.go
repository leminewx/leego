package response

import "testing"

// TestGetStatusText 验证标准状态码的文本格式。
func TestGetStatusText(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{code: 200, want: "200 OK"},
		{code: 201, want: "201 Created"},
		{code: 404, want: "404 Not Found"},
		{code: 500, want: "500 Internal Server Error"},
		{code: 999, want: ""},
		{code: 0, want: ""},
	}

	for _, testCase := range cases {
		if got := GetStatusText(testCase.code); got != testCase.want {
			t.Fatalf("GetStatusText(%d) = %q, want %q", testCase.code, got, testCase.want)
		}
	}
}

// TestGetStatusLowerText 验证小写文本格式。
func TestGetStatusLowerText(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{code: 200, want: "200 ok"},
		{code: 404, want: "404 not found"},
		{code: 503, want: "503 service unavailable"},
		{code: 999, want: ""},
	}

	for _, testCase := range cases {
		if got := GetStatusLowerText(testCase.code); got != testCase.want {
			t.Fatalf("GetStatusLowerText(%d) = %q, want %q", testCase.code, got, testCase.want)
		}
	}
}
