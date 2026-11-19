package jwt

import (
	"encoding/base64"
	"testing"
	"time"
)

// customizedClaims 定义嵌入标准声明的业务声明，用于验证自定义字段的签发与解析。
type customizedClaims struct {
	*StandardClaims

	Username string `json:"username"`
	Team     string `json:"team"`
	Role     string `json:"role"`
}

// decodeBase64URL 解码无填充的 Base64URL 字符串，失败时终止测试。
func decodeBase64URL(t *testing.T, encoded string) []byte {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Base64URL 解码失败: %v", err)
	}

	return decoded
}

// TestErrorSentinelsAreDistinct 验证各哨兵错误互不重叠，
// 便于调用方用 errors.Is 精确区分失败原因。
func TestErrorSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrInvalidToken":        ErrInvalidToken,
		"ErrMalformedToken":      ErrMalformedToken,
		"ErrUnsupportedMethod":   ErrUnsupportedMethod,
		"ErrInvalidSignature":    ErrInvalidSignature,
		"ErrMissingSignature":    ErrMissingSignature,
		"ErrNilClaims":           ErrNilClaims,
		"ErrInvalidIssuer":       ErrInvalidIssuer,
		"ErrInvalidSubject":      ErrInvalidSubject,
		"ErrInvalidAudience":     ErrInvalidAudience,
		"ErrExpiredToken":        ErrExpiredToken,
		"ErrNotValidYet":         ErrNotValidYet,
		"ErrInvalidIssuanceTime": ErrInvalidIssuanceTime,
		"ErrInvalidType":         ErrInvalidType,
		"ErrInvalidHeader":       ErrInvalidHeader,
		"ErrInvalidAlgorithm":    ErrInvalidAlgorithm,
		"ErrAlgorithmMismatch":   ErrAlgorithmMismatch,
		"ErrInvalidPayload":      ErrInvalidPayload,
		"ErrValidationFailed":    ErrValidationFailed,
	}

	for name, sentinel := range sentinels {
		if sentinel == nil {
			t.Fatalf("%s 为 nil", name)
		}
	}

	seen := make(map[string]string, len(sentinels))
	for name, sentinel := range sentinels {
		message := sentinel.Error()
		if previous, exists := seen[message]; exists {
			t.Fatalf("%s 与 %s 的错误消息重复: %q", name, previous, message)
		}
		seen[message] = name
	}
}

// TestSignAndParseRoundTrip 验证完整闭环：签发 → 解析 → 校验 → 声明还原。
func TestSignAndParseRoundTrip(t *testing.T) {
	signer, err := NewHMACSigner(HS384, []byte("0123456789abcdef"))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}
	parser := NewParser()

	now := time.Now().Unix()
	issued := &customizedClaims{
		StandardClaims: &StandardClaims{
			Id:        "jti-round-trip",
			Issuer:    "https://auth.example.com",
			Subject:   "user_12345",
			Audiences: []string{"https://api.example.com"},
			IssuedAt:  now,
			NotBefore: now,
			ExpiresAt: now + 3600,
		},
		Username: "carol",
		Team:     "infra",
		Role:     "operator",
	}

	token, err := signer.Sign(issued)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	parsed := &customizedClaims{}
	err = parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(
		signer, string(token), "https://auth.example.com", "https://api.example.com", parsed,
	)
	if err != nil {
		t.Fatalf("解析并校验失败: %v", err)
	}

	if parsed.Username != issued.Username || parsed.Team != issued.Team || parsed.Role != issued.Role {
		t.Fatalf("业务声明不一致: %+v != %+v", parsed, issued)
	}
	if parsed.Subject != issued.Subject || parsed.Id != issued.Id {
		t.Fatalf("标准声明不一致: %+v != %+v", parsed.StandardClaims, issued.StandardClaims)
	}
}

// TestSignWithDifferentAlgorithmsCanBeParsed 验证三种算法签发的令牌都能被
// 对应算法的签名器解析，而不会被错误拒绝。
func TestSignWithDifferentAlgorithmsCanBeParsed(t *testing.T) {
	parser := NewParser()

	for _, algorithm := range []Algorithm{HS256, HS384, HS512} {
		t.Run(string(algorithm), func(t *testing.T) {
			signer, err := NewHMACSigner(algorithm, []byte("secret-key"))
			if err != nil {
				t.Fatalf("创建签名器失败: %v", err)
			}

			token, err := signer.Sign(createTestClaims())
			if err != nil {
				t.Fatalf("签发失败: %v", err)
			}

			if err = parser.ParseAndValidateStandardClaims(signer, string(token), &StandardClaims{}); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
		})
	}
}

// TestSigningIsDeterministic 验证同一密钥、同一声明（时间字段固定）签发的令牌完全一致，
// 便于跨进程比对与缓存。
func TestSigningIsDeterministic(t *testing.T) {
	signer, err := NewHMACSigner(HS256, []byte("secret-key"))
	if err != nil {
		t.Fatalf("创建签名器失败: %v", err)
	}

	claims := createTestClaims()
	first, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	second, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("相同输入签发结果不一致: %s != %s", first, second)
	}
}
