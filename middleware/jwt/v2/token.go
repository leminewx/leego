package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Token 定义一个 JWT 令牌的数据结构
type Token struct {
	source    string          // 原始令牌字符串
	header    *Header         // 头部信息
	claims    map[string]any  // 声明信息
	stdClaims *StandardClaims // 标准声明信息
	signature []byte          // 签名
	isValid   bool            // 是否验证通过
}

// NewToken 创建新的 JWT
func NewToken(claims map[string]any) *Token {
	return &Token{
		header: &Header{
			Type:      "JWT",
			Algorithm: "HS256", // 默认算法
		},
		claims: claims,
	}
}

// WithStandardClaims 设置标准声明字段
func (own *Token) WithStandardClaims(claims *StandardClaims) *Token {
	if own.claims == nil {
		own.claims = make(map[string]any)
		own.stdClaims = claims
	}

	if claims.Issuer != "" {
		own.claims["iss"] = claims.Issuer
	}

	if claims.Subject != "" {
		own.claims["sub"] = claims.Subject
	}

	if claims.Audience != "" {
		own.claims["aud"] = claims.Audience
	}

	if claims.ExpiresAt > 0 {
		own.claims["exp"] = claims.ExpiresAt
	}

	if claims.NotBefore > 0 {
		own.claims["nbf"] = claims.NotBefore
	}

	if claims.IssuedAt > 0 {
		own.claims["iat"] = claims.IssuedAt
	}

	if claims.Id != "" {
		own.claims["jti"] = claims.Id
	}

	return own
}

// GetStandardClaims 获取标准声明字段
func (own *Token) GetStandardClaims() *StandardClaims {
	return &StandardClaims{
		Id:        own.stdClaims.Id,
		Issuer:    own.stdClaims.Issuer,
		Subject:   own.stdClaims.Subject,
		Audience:  own.stdClaims.Audience,
		IssuedAt:  own.stdClaims.IssuedAt,
		ExpiresAt: own.stdClaims.ExpiresAt,
		NotBefore: own.stdClaims.NotBefore,
	}
}

// ValidateStandardClaims 验证令牌的标准声明
func (own *Token) ValidateStandardClaims(issuer, audience string) error {
	if err := own.stdClaims.Validate(); err != nil {
		return err
	}

	if issuer != "" && own.stdClaims.Issuer != issuer {
		return errTokenInvalidIssuer
	}

	if audience != "" && own.stdClaims.Audience != audience {
		return errTokenInvalidAudience
	}

	return nil
}

// Sign 对令牌进行签名
func (own *Token) Sign(signer Signer) (string, error) {
	// 设置算法
	own.header.Algorithm = signer.GetAlgorithm()

	// 编码头部
	headerJSON, err := json.Marshal(&own.header)
	if err != nil {
		return "", fmt.Errorf("leego: jwt: failed to marshal header: %v", err)
	}
	headerEncoded := EncodeBase64(headerJSON)

	// 编码声明
	claimsJSON, err := json.Marshal(own.claims)
	if err != nil {
		return "", fmt.Errorf("leego: jwt: failed to marshal claims: %v", err)
	}
	claimsEncoded := EncodeBase64(claimsJSON)

	// 生成签名
	signingInput := headerEncoded + "." + claimsEncoded
	signature, err := signer.Sign([]byte(signingInput))
	if err != nil {
		return "", fmt.Errorf("leego: jwt: failed to sign token: %v", err)
	}
	signatureEncoded := EncodeBase64(signature)

	// 构建完整的 JWT
	own.source = signingInput + "." + signatureEncoded
	own.signature = signature

	return own.source, nil
}

// EncodeBase64 对数据进行 Base64 URL 编码
func EncodeBase64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeBase64 解码 Base64 URL 编码的数据
func DecodeBase64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
