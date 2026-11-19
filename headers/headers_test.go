package headers

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// allHeaders 汇总包内全部头部常量，便于统一校验拼写、唯一性与合法性。
var allHeaders = map[string]string{
	// universal.go
	"DATE":                DATE,
	"CACHE_CONTROL":       CACHE_CONTROL,
	"CONNECTION":          CONNECTION,
	"UPGRADE":             UPGRADE,
	"ALLOW":               ALLOW,
	"VIA":                 VIA,
	"CONTENT_TYPE":        CONTENT_TYPE,
	"CONTENT_LENGTH":      CONTENT_LENGTH,
	"CONTENT_RANGE":       CONTENT_RANGE,
	"CONTENT_DISPOSITION": CONTENT_DISPOSITION,
	"CONTENT_ENCODING":    CONTENT_ENCODING,
	"TRANSFER_ENCODING":   TRANSFER_ENCODING,

	// request.go
	"HOST":            HOST,
	"USER_AGENT":      USER_AGENT,
	"ACCEPT":          ACCEPT,
	"ACCEPT_ENCODING": ACCEPT_ENCODING,
	"ACCEPT_LANGUAGE": ACCEPT_LANGUAGE,
	"AUTHORIZATION":   AUTHORIZATION,
	"COOKIE":          COOKIE,
	"REFERER":         REFERER,
	"RANGE":           RANGE,

	// response.go
	"STATUS":        STATUS,
	"SERVER":        SERVER,
	"SET_COOKIE":    SET_COOKIE,
	"LOCATION":      LOCATION,
	"E_TAG":         E_TAG,
	"LAST_MODIFIED": LAST_MODIFIED,

	// security.go
	"REFERRER_POLICY":                  REFERRER_POLICY,
	"PERMISSIONS_POLICY":               PERMISSIONS_POLICY,
	"CONTENT_SECURITY_POLICY":          CONTENT_SECURITY_POLICY,
	"X_XSS_PROTECTION":                 X_XSS_PROTECTION,
	"X_FRAME_OPTIONS":                  X_FRAME_OPTIONS,
	"X_CONTENT_TYPE_OPTIONS":           X_CONTENT_TYPE_OPTIONS,
	"STRICT_TRANSPORT_SECURITY":        STRICT_TRANSPORT_SECURITY,
	"ACCESS_CONTROL_ALLOW_ORIGIN":      ACCESS_CONTROL_ALLOW_ORIGIN,
	"ACCESS_CONTROL_ALLOW_METHODS":     ACCESS_CONTROL_ALLOW_METHODS,
	"ACCESS_CONTROL_ALLOW_HEADERS":     ACCESS_CONTROL_ALLOW_HEADERS,
	"ACCESS_CONTROL_EXPOSE_HEADERS":    ACCESS_CONTROL_EXPOSE_HEADERS,
	"ACCESS_CONTROL_REQUEST_METHOD":    ACCESS_CONTROL_REQUEST_METHOD,
	"ACCESS_CONTROL_REQUEST_HEADERS":   ACCESS_CONTROL_REQUEST_HEADERS,
	"ACCESS_CONTROL_ALLOW_CREDENTIALS": ACCESS_CONTROL_ALLOW_CREDENTIALS,
	"ACCESS_CONTROL_MAX_AGE":           ACCESS_CONTROL_MAX_AGE,

	// expansion.go
	"X_REAL_IP":        X_REAL_IP,
	"X_FORWARDED_FOR":  X_FORWARDED_FOR,
	"X_REQUESTED_WITH": X_REQUESTED_WITH,
	"X_REQUEST_ID":     X_REQUEST_ID,
}

// isTokenCharacter 判断字符是否属于 RFC 7230 定义的 token 字符集。
func isTokenCharacter(char byte) bool {
	if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
		return true
	}

	switch char {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}

// TestHeaderConstantsAreValid 验证所有常量非空、符合 HTTP 头名称规范且互不重复。
func TestHeaderConstantsAreValid(t *testing.T) {
	seen := make(map[string]string, len(allHeaders))

	for name, value := range allHeaders {
		if value == "" {
			t.Fatalf("%s 为空", name)
		}

		for index := 0; index < len(value); index++ {
			if !isTokenCharacter(value[index]) {
				t.Fatalf("%s = %q 含有非法字符 %q", name, value, value[index])
			}
		}

		if previous, exists := seen[value]; exists {
			t.Fatalf("%s 与 %s 的头部名称重复: %q", name, previous, value)
		}
		seen[value] = name
	}
}

// TestHeaderConstantsMatchLiteralValues 验证关键常量的字面值，防止误改导致协议不兼容。
func TestHeaderConstantsMatchLiteralValues(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "DATE", actual: DATE, expected: "Date"},
		{name: "CONTENT_TYPE", actual: CONTENT_TYPE, expected: "Content-Type"},
		{name: "CONTENT_LENGTH", actual: CONTENT_LENGTH, expected: "Content-Length"},
		{name: "SET_COOKIE", actual: SET_COOKIE, expected: "Set-Cookie"},
		{name: "LOCATION", actual: LOCATION, expected: "Location"},
		{name: "USER_AGENT", actual: USER_AGENT, expected: "User-Agent"},
		{name: "AUTHORIZATION", actual: AUTHORIZATION, expected: "Authorization"},
		{name: "COOKIE", actual: COOKIE, expected: "Cookie"},
		{name: "X_FORWARDED_FOR", actual: X_FORWARDED_FOR, expected: "X-Forwarded-For"},
		{name: "X_REAL_IP", actual: X_REAL_IP, expected: "X-Real-IP"},
		{name: "X_REQUEST_ID", actual: X_REQUEST_ID, expected: "X-Request-ID"},
		{name: "STRICT_TRANSPORT_SECURITY", actual: STRICT_TRANSPORT_SECURITY, expected: "Strict-Transport-Security"},
	}

	for _, testCase := range cases {
		if testCase.actual != testCase.expected {
			t.Fatalf("%s = %q, want %q", testCase.name, testCase.actual, testCase.expected)
		}
	}
}

// TestGetContentType 验证媒体类型提取（去除参数与空格）。
func TestGetContentType(t *testing.T) {
	cases := []struct {
		name     string
		header   http.Header
		expected string
	}{
		{name: "nil 头", header: nil, expected: ""},
		{name: "缺失", header: http.Header{}, expected: ""},
		{name: "纯媒体类型", header: http.Header{"Content-Type": {"application/json"}}, expected: "application/json"},
		{name: "带字符集", header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, expected: "application/json"},
		{name: "带空格", header: http.Header{"Content-Type": {"text/html "}}, expected: "text/html"},
		{name: "空值", header: http.Header{"Content-Type": {""}}, expected: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := GetContentType(testCase.header); actual != testCase.expected {
				t.Fatalf("GetContentType() = %q, want %q", actual, testCase.expected)
			}
		})
	}
}

// createTestFile 创建带指定扩展名与内容的临时文件。
func createTestFile(t *testing.T, name, content string) *os.File {
	t.Helper()

	filePath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("打开临时文件失败: %v", err)
	}

	t.Cleanup(func() {
		_ = file.Close()
	})

	return file
}

// TestDetectContentTypeByExtension 验证优先按扩展名识别类型。
func TestDetectContentTypeByExtension(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		content  string
		expected string
	}{
		{name: "JSON 文件", fileName: "data.json", content: `{"ok":true}`, expected: "application/json"},
		{name: "HTML 文件", fileName: "index.html", content: "<html></html>", expected: "text/html; charset=utf-8"},
		{name: "大写扩展名", fileName: "style.CSS", content: "body{}", expected: "text/css; charset=utf-8"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file := createTestFile(t, testCase.fileName, testCase.content)
			if actual := DetectContentType(file); actual != testCase.expected {
				t.Fatalf("DetectContentType() = %q, want %q", actual, testCase.expected)
			}
		})
	}
}

// TestDetectContentTypeByContent 验证无扩展名时回退到内容嗅探。
func TestDetectContentTypeByContent(t *testing.T) {
	file := createTestFile(t, "noext", "<!DOCTYPE html><html><body>hello</body></html>")
	if actual := DetectContentType(file); actual != "text/html; charset=utf-8" {
		t.Fatalf("DetectContentType() = %q, want text/html; charset=utf-8", actual)
	}
}

// TestDetectContentTypeFallsBackToBinary 验证无法识别时回退为二进制流。
func TestDetectContentTypeFallsBackToBinary(t *testing.T) {
	file := createTestFile(t, "noext", string([]byte{0x00, 0x01, 0x02, 0x03}))
	if actual := DetectContentType(file); actual != "application/octet-stream" {
		t.Fatalf("DetectContentType() = %q, want application/octet-stream", actual)
	}
}

// TestIsContinuousResponse 验证持续响应判定覆盖分块、SSE、下载与 WebSocket。
func TestIsContinuousResponse(t *testing.T) {
	cases := []struct {
		name     string
		header   http.Header
		expected bool
	}{
		{name: "普通响应", header: http.Header{"Content-Type": {"application/json"}}, expected: false},
		{name: "分块传输", header: http.Header{TRANSFER_ENCODING: {"chunked"}}, expected: true},
		{name: "SSE", header: http.Header{CONTENT_TYPE: {"text/event-stream; charset=utf-8"}}, expected: true},
		{name: "文件下载", header: http.Header{CONTENT_DISPOSITION: {`attachment; filename="a.zip"`}}, expected: true},
		{name: "WebSocket", header: http.Header{UPGRADE: {"websocket"}, CONNECTION: {"Upgrade"}}, expected: true},
		{name: "WebSocket 小写 Connection", header: http.Header{UPGRADE: {"websocket"}, CONNECTION: {"upgrade"}}, expected: true},
		{name: "仅 Upgrade 无 Connection", header: http.Header{UPGRADE: {"websocket"}}, expected: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := IsContinuousResponse(testCase.header); actual != testCase.expected {
				t.Fatalf("IsContinuousResponse() = %v, want %v", actual, testCase.expected)
			}
		})
	}
}
