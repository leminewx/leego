package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// _COOKIE_SEPARATOR 分隔会话载荷与签名。
const _COOKIE_SEPARATOR = "."

// _ISSUE_SEPARATOR 分隔会话 ID 与签发时间。
//
// 会话 ID 是 base64url 编码的随机字节，不可能包含该字符，
// 因此可以安全地用它作为载荷内部的分隔符。
const _ISSUE_SEPARATOR = "|"

// encodeCookie 把会话 ID 编码为"载荷.签名"形式。
//
// 载荷包含会话 ID 与 Cookie 签发时间，签发时间用于滑动续期：
// 服务端据此判断是否需要同时刷新 Cookie 与存储过期时间。
func encodeCookie(secret []byte, id string) string {
	return encodeCookieAt(secret, id, time.Now())
}

// encodeCookieAt 以指定签发时间编码会话 Cookie。
//
// 载荷使用 base64url 无填充编码，签名覆盖载荷的原始字节，
// 避免对 base64 文本重新编码带来的歧义。
func encodeCookieAt(secret []byte, id string, issued time.Time) string {
	if id == "" {
		return ""
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte(id + _ISSUE_SEPARATOR + strconv.FormatInt(issued.Unix(), 10)))
	return payload + _COOKIE_SEPARATOR + base64.RawURLEncoding.EncodeToString(signValue(secret, payload))
}

// decodeCookie 校验签名并还原会话 ID。
func decodeCookie(secret []byte, value string) (string, bool) {
	id, _, ok := decodeCookieWithIssue(secret, value)
	return id, ok
}

// decodeCookieWithIssue 校验签名并还原会话 ID 与签发时间。
//
// 不含签发时间的旧格式 Cookie 仍然可以通过校验，此时返回零值时间，
// 由调用方按“需要立即续期”处理，从而实现平滑升级。
func decodeCookieWithIssue(secret []byte, value string) (string, time.Time, bool) {
	if value == "" {
		return "", time.Time{}, false
	}

	payload, signature, ok := strings.Cut(value, _COOKIE_SEPARATOR)
	if !ok || payload == "" || signature == "" {
		return "", time.Time{}, false
	}

	expected := signValue(secret, payload)
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(expected, actual) {
		return "", time.Time{}, false
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 {
		return "", time.Time{}, false
	}

	id, issuedText, found := strings.Cut(string(raw), _ISSUE_SEPARATOR)
	if !found {
		return string(raw), time.Time{}, true
	}

	unix, err := strconv.ParseInt(issuedText, 10, 64)
	if err != nil || id == "" {
		return "", time.Time{}, false
	}

	return id, time.Unix(unix, 0), true
}

// signValue 计算载荷的 HMAC-SHA256。
func signValue(secret []byte, payload string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
