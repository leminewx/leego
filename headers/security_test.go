package headers

import "testing"

// TestSecurityHeaders 验证安全头与 CORS 头的字面值与可用性。
func TestSecurityHeaders(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{name: "REFERRER_POLICY", actual: REFERRER_POLICY, expected: "Referrer-Policy"},
		{name: "PERMISSIONS_POLICY", actual: PERMISSIONS_POLICY, expected: "Permissions-Policy"},
		{name: "CONTENT_SECURITY_POLICY", actual: CONTENT_SECURITY_POLICY, expected: "Content-Security-Policy"},
		{name: "X_XSS_PROTECTION", actual: X_XSS_PROTECTION, expected: "X-XSS-Protection"},
		{name: "X_FRAME_OPTIONS", actual: X_FRAME_OPTIONS, expected: "X-Frame-Options"},
		{name: "X_CONTENT_TYPE_OPTIONS", actual: X_CONTENT_TYPE_OPTIONS, expected: "X-Content-Type-Options"},
		{name: "STRICT_TRANSPORT_SECURITY", actual: STRICT_TRANSPORT_SECURITY, expected: "Strict-Transport-Security"},
		{name: "ACCESS_CONTROL_ALLOW_ORIGIN", actual: ACCESS_CONTROL_ALLOW_ORIGIN, expected: "Access-Control-Allow-Origin"},
		{name: "ACCESS_CONTROL_ALLOW_METHODS", actual: ACCESS_CONTROL_ALLOW_METHODS, expected: "Access-Control-Allow-Methods"},
		{name: "ACCESS_CONTROL_ALLOW_HEADERS", actual: ACCESS_CONTROL_ALLOW_HEADERS, expected: "Access-Control-Allow-Headers"},
		{name: "ACCESS_CONTROL_EXPOSE_HEADERS", actual: ACCESS_CONTROL_EXPOSE_HEADERS, expected: "Access-Control-Expose-Headers"},
		{name: "ACCESS_CONTROL_REQUEST_METHOD", actual: ACCESS_CONTROL_REQUEST_METHOD, expected: "Access-Control-Request-Method"},
		{name: "ACCESS_CONTROL_REQUEST_HEADERS", actual: ACCESS_CONTROL_REQUEST_HEADERS, expected: "Access-Control-Request-Headers"},
		{name: "ACCESS_CONTROL_ALLOW_CREDENTIALS", actual: ACCESS_CONTROL_ALLOW_CREDENTIALS, expected: "Access-Control-Allow-Credentials"},
		{name: "ACCESS_CONTROL_MAX_AGE", actual: ACCESS_CONTROL_MAX_AGE, expected: "Access-Control-Max-Age"},
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
