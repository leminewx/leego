package session

import (
	"strings"
	"testing"
)

// TestEncodeCookieEmptyID 验证空会话 ID 不产生 Cookie 值。
func TestEncodeCookieEmptyID(t *testing.T) {
	if cookie := encodeCookie([]byte("secret"), ""); cookie != "" {
		t.Fatalf("空会话 ID 应返回空串: %q", cookie)
	}
}

// TestCookieSignedRoundTrip 验证编码后的 Cookie 可被正确校验与还原。
func TestCookieSignedRoundTrip(t *testing.T) {
	secret := []byte("leego-test-secret")
	value := encodeCookie(secret, "session-id")

	if !strings.Contains(value, _COOKIE_SEPARATOR) {
		t.Fatalf("Cookie 值缺少签名分隔符: %q", value)
	}

	id, ok := decodeCookie(secret, value)
	if !ok || id != "session-id" {
		t.Fatalf("decodeCookie() = (%q, %v), want (session-id, true)", id, ok)
	}
}

// TestDecodeCookieRejectsTampering 验证签名、密钥与格式异常都会被拒绝。
func TestDecodeCookieRejectsTampering(t *testing.T) {
	secret := []byte("leego-test-secret")
	value := encodeCookie(secret, "session-id")

	if _, ok := decodeCookie([]byte("other-secret"), value); ok {
		t.Fatal("换用其它密钥不应通过校验")
	}

	payload, signature, _ := strings.Cut(value, _COOKIE_SEPARATOR)
	tampered := payload + _COOKIE_SEPARATOR + strings.Repeat("A", len(signature))
	if _, ok := decodeCookie(secret, tampered); ok {
		t.Fatal("被篡改的签名不应通过校验")
	}

	for _, invalid := range []string{"", "noseparator", _COOKIE_SEPARATOR + signature} {
		if _, ok := decodeCookie(secret, invalid); ok {
			t.Fatalf("decodeCookie(%q) 应返回 false", invalid)
		}
	}
}
