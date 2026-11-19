package jwt

import (
	"errors"
	"testing"
	"time"
)

// TestStandardClaimsValidateIgnoresEmptyTimeFields 验证零值声明不做过期判断。
func TestStandardClaimsValidateIgnoresEmptyTimeFields(t *testing.T) {
	claims := StandardClaims{}
	if err := claims.Validate(); err != nil {
		t.Fatalf("空声明不应报错: %v", err)
	}
}

// TestStandardClaimsValidateAcceptsValidToken 验证时间字段均合法的声明通过校验。
func TestStandardClaimsValidateAcceptsValidToken(t *testing.T) {
	now := time.Now().Unix()
	claims := StandardClaims{
		IssuedAt:  now,
		NotBefore: now - 10,
		ExpiresAt: now + 3600,
	}

	if err := claims.Validate(); err != nil {
		t.Fatalf("有效声明校验失败: %v", err)
	}
}

// TestStandardClaimsValidateRejectsExpiredToken 验证过期声明返回 ErrExpiredToken。
func TestStandardClaimsValidateRejectsExpiredToken(t *testing.T) {
	claims := StandardClaims{ExpiresAt: time.Now().Add(-time.Hour).Unix()}
	if err := claims.Validate(); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

// TestStandardClaimsValidateRejectsNotValidYet 验证未生效声明返回 ErrNotValidYet。
func TestStandardClaimsValidateRejectsNotValidYet(t *testing.T) {
	claims := StandardClaims{NotBefore: time.Now().Add(time.Hour).Unix()}
	if err := claims.Validate(); !errors.Is(err, ErrNotValidYet) {
		t.Fatalf("err = %v, want ErrNotValidYet", err)
	}
}

// TestStandardClaimsValidateRejectsFutureIssuedAt 验证签发时间在未来的声明被拒绝。
func TestStandardClaimsValidateRejectsFutureIssuedAt(t *testing.T) {
	claims := StandardClaims{IssuedAt: time.Now().Add(time.Hour).Unix()}
	if err := claims.Validate(); !errors.Is(err, ErrInvalidIssuanceTime) {
		t.Fatalf("err = %v, want ErrInvalidIssuanceTime", err)
	}
}

// TestStandardClaimsValidateWithLeewayToleratesSkew 验证时钟偏差对三个时间字段同时生效。
func TestStandardClaimsValidateWithLeewayToleratesSkew(t *testing.T) {
	now := time.Now().Unix()
	claims := StandardClaims{
		ExpiresAt: now - 30, // 过期 30 秒，但在 1 分钟偏差容忍范围内
		NotBefore: now + 30, // 尚未生效 30 秒，同样在容忍范围内
		IssuedAt:  now + 30, // 签发时间在未来 30 秒，同样在容忍范围内
	}

	if err := claims.ValidateWithLeeway(time.Minute); err != nil {
		t.Fatalf("1 分钟偏差下不应报错: %v", err)
	}

	if err := claims.ValidateWithLeeway(0); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("无偏差时 err = %v, want ErrExpiredToken", err)
	}
}

// TestStandardClaimsValidateWithNegativeLeeway 验证负偏差被修正为 0，
// 不会因为传入负值把校验变得更宽松或更严格。
func TestStandardClaimsValidateWithNegativeLeeway(t *testing.T) {
	claims := StandardClaims{ExpiresAt: time.Now().Add(-time.Minute).Unix()}
	if err := claims.ValidateWithLeeway(-time.Hour); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

// TestStandardClaimsValidateWithIssuerAndAudiences 覆盖签发者与受众的组合校验。
func TestStandardClaimsValidateWithIssuerAndAudiences(t *testing.T) {
	claims := StandardClaims{
		Issuer:    "https://auth.example.com",
		Audiences: []string{"https://api.example.com", "https://admin.example.com"},
	}

	cases := []struct {
		name      string
		issuer    string
		audience  string
		expectErr error
	}{
		{name: "全部匹配", issuer: "https://auth.example.com", audience: "https://api.example.com"},
		{name: "匹配第二个受众", issuer: "https://auth.example.com", audience: "https://admin.example.com"},
		{name: "空期望值表示不限制", issuer: "", audience: ""},
		{name: "仅校验签发者", issuer: "https://auth.example.com", audience: ""},
		{name: "签发者不匹配", issuer: "https://evil.example.com", audience: "https://api.example.com", expectErr: ErrInvalidIssuer},
		{name: "受众不匹配", issuer: "https://auth.example.com", audience: "https://other.example.com", expectErr: ErrInvalidAudience},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := claims.ValidateWithIssuerAndAudiences(testCase.issuer, testCase.audience)
			if !errors.Is(err, testCase.expectErr) {
				t.Fatalf("err = %v, want %v", err, testCase.expectErr)
			}
		})
	}
}

// TestStandardClaimsValidateWithIssuerAndAudiencesChecksTimeFirst 验证时间校验先于
// 签发者校验执行：过期令牌即使签发者匹配，也应返回过期错误。
func TestStandardClaimsValidateWithIssuerAndAudiencesChecksTimeFirst(t *testing.T) {
	claims := StandardClaims{
		Issuer:    "issuer",
		Audiences: []string{"audience"},
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}

	err := claims.ValidateWithIssuerAndAudiences("issuer", "audience")
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

// TestStandardClaimsValidateWithIssuerAndAudiencesAndLeeway 验证带偏差的完整校验。
func TestStandardClaimsValidateWithIssuerAndAudiencesAndLeeway(t *testing.T) {
	now := time.Now().Unix()
	claims := StandardClaims{
		Issuer:    "issuer",
		Audiences: []string{"audience"},
		ExpiresAt: now - 10,
	}

	if err := claims.ValidateWithIssuerAndAudiencesAndLeeway("issuer", "audience", time.Minute); err != nil {
		t.Fatalf("偏差容忍范围内不应报错: %v", err)
	}

	if err := claims.ValidateWithIssuerAndAudiencesAndLeeway("other", "audience", time.Minute); !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("err = %v, want ErrInvalidIssuer", err)
	}
}

// TestStandardClaimsEmptyAudienceMatchesNothing 验证期望受众非空但声明无受众时被拒绝。
func TestStandardClaimsEmptyAudienceMatchesNothing(t *testing.T) {
	claims := StandardClaims{Issuer: "issuer"}
	if err := claims.ValidateWithIssuerAndAudiences("issuer", "audience"); !errors.Is(err, ErrInvalidAudience) {
		t.Fatalf("err = %v, want ErrInvalidAudience", err)
	}
}
