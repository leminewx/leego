package jwt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Parser 定义用于解析和验证 JWT 的解析器的数据结构
type Parser struct {
	// ValidAlgorithms 可接受的签名算法
	ValidAlgorithms []Algorithm
	// SkipClaimsValidation 是否跳过声明验证
	SkipClaimsValidation bool
}

// NewParser 创建新的解析器
func NewParser() *Parser {
	return &Parser{
		ValidAlgorithms: []Algorithm{HS256, HS384, HS512},
	}
}

// Parse 解析 JWT 字符串
func (own *Parser) Parse(tokenString string, signer Signer) (*Token, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("leego: jwt: token contains an invalid number of segments")
	}

	token := &Token{source: tokenString}

	// 解析头部
	headerJSON, err := DecodeBase64(parts[0])
	if err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to decode header: %v", err)
	}

	if err := json.Unmarshal(headerJSON, &token.header); err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to unmarshal header: %v", err)
	}

	// 验证算法
	if !own.isValidAlgorithm(token.header.Algorithm) {
		return nil, fmt.Errorf("leego: jwt: invalid signing algorithm: %s", token.header.Algorithm)
	}

	if token.header.Algorithm != signer.GetAlgorithm() {
		return nil, fmt.Errorf("leego: jwt: signing algorithm mismatch")
	}

	// 解析声明
	claimsJSON, err := DecodeBase64(parts[1])
	if err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to decode claims: %v", err)
	}

	if err := json.Unmarshal(claimsJSON, &token.claims); err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to unmarshal claims: %v", err)
	}

	// 验证签名
	signature, err := DecodeBase64(parts[2])
	if err != nil {
		return nil, fmt.Errorf("leego: jwt: failed to decode signature: %v", err)
	}

	signingInput := []byte(parts[0] + "." + parts[1])
	if !signer.Verify(signingInput, signature) {
		return nil, fmt.Errorf("leego: jwt: failed to verify signature")
	}

	token.signature = signature
	token.isValid = true

	// 验证声明（除非明确跳过）
	if !own.SkipClaimsValidation {
		if err := own.validateStandardClaims(token); err != nil {
			return token, err // 返回 token 和错误，token 本身已解析但声明无效
		}
	}

	return token, nil
}

// isValidAlgorithm 检查算法是否在可接受列表中
func (own *Parser) isValidAlgorithm(alg Algorithm) bool {
	for _, a := range own.ValidAlgorithms {
		if a == alg {
			return true
		}
	}

	return false
}

// validateClaims 验证声明
func (own *Parser) validateStandardClaims(token *Token) error {
	stdClaims := token.GetStandardClaims()
	if stdClaims == nil {
		return fmt.Errorf("leego: jwt: failed to get standard claims: %v", err)
	}

	return stdClaims.Validate()
}

// ParseWithClaims 解析 JWT 并验证特定声明
func (own *Parser) ParseWithClaims(signer Signer, tokenString string, expectedIssuer, expectedAudience string) (*Token, error) {
	token, err := own.Parse(tokenString, signer)
	if err != nil {
		return nil, err
	}

	return token, token.ValidateStandardClaims(expectedIssuer, expectedAudience)
}
