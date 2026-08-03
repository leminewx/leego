package jwt

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Parser 定义用于解析和验证 JWT 的解析器的接口类型
type Parser interface {
	// Parse 解析 JWT，不验证
	Parse(signer Signer, tokenString string, claims Claims) error
	// ParseAndValidateStandardClaims 解析 JWT 并验证标准声明中的所有时间字段（签发时间允许1分钟时间偏差）
	ParseAndValidateStandardClaims(signer Signer, tokenString string, claims Claims) error
	// ParseAndValidateStandardClaimsWithIssuerAndAudiences 解析 JWT 并验证标准声明中的所有时间字段（签发时间允许1分钟时间偏差）、签发者和受众
	ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer Signer, tokenString, expectedIssuer, expectedAudience string, claims Claims) error
}

// parser 定义用于解析和验证 JWT 的解析器的结构类型
type parser struct {
	ClockSkew        time.Duration // 时钟误差
	ExpectedIssuer   string        // 期望签发者
	ExpectedAudience string        // 期望受众，通常是当前服务端
}

// NewParser 新建一个 JWT 解析器
func NewParser() Parser { return &parser{} }

// Parse 解析令牌，不验证
func (own *parser) Parse(signer Signer, tokenString string, claims Claims) error {
	// 解析令牌
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return ErrInvalidToken
	}

	// 验证头部
	if signer.GetHeader().GetEncodedString() != parts[0] {
		return ErrInvalidHeader
	}

	// 校验令牌声明的算法与签名器一致，防止算法混淆攻击（如将 alg 替换为 none）
	var head Header
	if err := json.Unmarshal(decodeBase64URL(parts[0]), &head); err != nil {
		return ErrInvalidHeader
	}
	if head.GetAlgorithm() != signer.GetHeader().GetAlgorithm() {
		return ErrInvalidAlgorithm
	}

	// 解析声明（先于签名校验，使结构错误的令牌返回 ErrInvalidPayload 而非 ErrInvalidToken）
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrInvalidPayload
	}

	// 解析签名
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ErrInvalidSignature
	}

	// 验证签名
	if !signer.Verify([]byte(parts[0]+"."+parts[1]), signature) {
		return ErrInvalidSignature
	}

	// 反序列化声明
	return json.Unmarshal(claimsJSON, &claims)
}

// decodeBase64URL 解码 JWT 段（用于头部/载荷的结构解析）
func decodeBase64URL(seg string) []byte {
	data, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return []byte(seg)
	}
	return data
}

// ParseAndValidateStandardClaims 解析 JWT 并验证标准声明中的所有时间字段（签发时间允许1分钟时间偏差）
func (own *parser) ParseAndValidateStandardClaims(signer Signer, tokenString string, claims Claims) error {
	if err := own.Parse(signer, tokenString, claims); err != nil {
		return err
	}

	return claims.Validate()
}

// ParseAndValidateStandardClaimsWithIssuerAndAudiences 解析 JWT 并验证标准声明中的所有时间字段（签发时间允许1分钟时间偏差）、签发者和受众
func (own *parser) ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer Signer, tokenString string, expectedIssuer, expectedAudience string, claims Claims) error {
	if err := own.Parse(signer, tokenString, claims); err != nil {
		return err
	}

	return claims.ValidateWithIssuerAndAudiences(expectedIssuer, expectedAudience)
}
