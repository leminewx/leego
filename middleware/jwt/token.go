package jwt

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

var (
	errTokenNoAnyClaims     = errors.New("leego: jwt: token has no any claims")
	errTokenInvalidIssuer   = errors.New("leego: jwt: token has no invalid issuer")
	errTokenInvalidAudience = errors.New("leego: jwt: token has no invalid audience")
)

// Token 定义一个 JWT 令牌的数据结构
type Token struct {
	claims    map[string]any  // 所有声明信息
	stdClaims *StandardClaims // 标准声明信息
	isValid   bool            // 是否验证通过
}

// NewToken 创建新的 JWT
func NewToken(claims *StandardClaims) *Token {
	return &Token{stdClaims: claims}
}

// GetClaim 获取指定声明
func (own *Token) GetClaim(key string) (any, bool) {
	if len(own.claims) > 0 {
		val, ok := own.claims[key]
		return val, ok
	}

	return nil, false
}

// GetAllClaims 获取所有的声明
func (own *Token) GetAllClaims() map[string]any {
	return own.claims
}

// GetStandardClaims 获取标准声明字段
func (own *Token) GetStandardClaims() *StandardClaims {
	return own.stdClaims
}

// WithClaim 设置声明字段
func (own *Token) WithClaim(key string, value any) *Token {
	if own.claims == nil {
		own.claims = make(map[string]any)
	}

	own.claims[key] = value
	return own
}

// encodeClaims 编码声明
func (own *Token) encodeClaims() (string, error) {
	if len(own.claims) == 0 {
		if own.stdClaims == nil {
			return "", errTokenNoAnyClaims
		}

		return own.stdClaims.Encode()
	}

	bytes, err := json.Marshal(&own.claims)
	if err != nil {
		return "", fmt.Errorf("leego: jwt: failed to marshal token claims: %v", err)
	}

	return EncodeBase64(bytes), nil
}

// syncClaims 同步声明
func (own *Token) syncClaims() {
	if len(own.claims) == 0 || own.stdClaims == nil {
		return
	}

	claims := own.stdClaims
	if claims.Issuer != "" {
		own.claims["iss"] = claims.Issuer
	}

	if claims.Subject != "" {
		own.claims["sub"] = claims.Subject
	}

	if len(claims.Audiences) > 0 {
		own.claims["aud"] = claims.Audiences
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
}

// ValidateStandardClaims 验证令牌的标准声明（签发时间允许1分钟时间偏差）
func (own *Token) ValidateStandardClaims(expectedIssuer, expectedAudience string) error {
	// 验证标准声明中的所有时间字段
	if err := own.stdClaims.Validate(); err != nil {
		return err
	}

	// 验证 JWT 签发者
	if expectedIssuer != "" && own.stdClaims.Issuer != expectedIssuer {
		return errTokenInvalidIssuer
	}

	// 验证 JWT 接收者
	if expectedAudience == "" {
		return nil
	}

	switch len(own.stdClaims.Audiences) {
	case 0:
		return errTokenInvalidAudience
	case 1:
		if own.stdClaims.Audiences[0] != expectedAudience {
			return errTokenInvalidAudience
		}
	default:
		if !slices.Contains(own.stdClaims.Audiences, expectedAudience) {
			return errTokenInvalidAudience
		}
	}

	return nil
}
