package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Parser 定义用于解析和验证 JWT 的解析器的接口类型
type Parser interface {
	// Parse 解析 JWT
	Parse(signer Signer, tokenString string, claims Claims) error
	// ParseAndValidateStandardClaims 解析 JWT 并验证标准声明
	ParseAndValidateStandardClaims(signer Signer, tokenString, expectedIssuer, expectedAudience string, claims Claims) error
}

// parser 定义用于解析和验证 JWT 的解析器的数据结构
type parser struct{}

// NewParser 新建一个 JWT 解析器
func NewParser() Parser {
	return &parser{}
}

// Parse 解析 JWT 字符串
func (own *parser) Parse(signer Signer, tokenString string, claims Claims) error {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return fmt.Errorf("leego: jwt: token contains an invalid number of segments")
	}

	// 解析头部
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("leego: jwt: failed to decode header: %v", err)
	}

	header := &Header{}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return fmt.Errorf("leego: jwt: failed to unmarshal header: %v", err)
	}

	// 验证算法
	if signer.GetAlgorithm() != header.Algorithm {
		return fmt.Errorf("leego: jwt: invalid signing algorithm: %s", header.Algorithm)
	}

	if header.Algorithm != signer.GetAlgorithm() {
		return fmt.Errorf("leego: jwt: signing algorithm mismatch")
	}

	// 验证签名
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("leego: jwt: failed to decode signature: %v", err)
	}

	signingInput := []byte(parts[0] + "." + parts[1])
	if !signer.Verify(signingInput, signature) {
		return fmt.Errorf("leego: jwt: failed to verify signature")
	}

	// 解析声明
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("leego: jwt: failed to decode claims: %v", err)
	}

	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return fmt.Errorf("leego: jwt: failed to unmarshal claims: %v", err)
	}

	return nil
}

// ParseAndValidateStandardClaims 解析 JWT 并验证标准声明
func (own *parser) ParseAndValidateStandardClaims(signer Signer, tokenString string, expectedIssuer, expectedAudience string, claims Claims) error {
	if err := own.Parse(signer, tokenString, claims); err != nil {
		return err
	}

	return claims.ValidateWithIssuerAndAudiences(expectedIssuer, expectedAudience)
}
