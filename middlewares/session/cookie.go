package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// _COOKIE_SEPARATOR 分隔会话 ID 与签名。
const _COOKIE_SEPARATOR = "."

// encodeCookie 把会话 ID 编码为"载荷.签名"形式。
//
// 载荷使用 base64url 无填充编码，签名覆盖载荷的原始字节，
// 避免对 base64 文本重新编码带来的歧义。
func encodeCookie(secret []byte, id string) string {
	if id == "" {
		return ""
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte(id))
	return payload + _COOKIE_SEPARATOR + base64.RawURLEncoding.EncodeToString(signValue(secret, payload))
}

// decodeCookie 校验签名并还原会话 ID。
func decodeCookie(secret []byte, value string) (string, bool) {
	if value == "" {
		return "", false
	}

	payload, signature, ok := strings.Cut(value, _COOKIE_SEPARATOR)
	if !ok || payload == "" || signature == "" {
		return "", false
	}

	expected := signValue(secret, payload)
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(expected, actual) {
		return "", false
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 {
		return "", false
	}

	return string(raw), true
}

// signValue 计算载荷的 HMAC-SHA256。
func signValue(secret []byte, payload string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
