package jwt

import (
	"errors"
	"time"
)

var (
	errTokenExpired         = errors.New("leego: jwt: token is expired")
	errTokenNotValidYet     = errors.New("leego: jwt: token is not valid yet")
	errTokenIssuedFuture    = errors.New("leego: jwt: token issued in the future")
	errTokenInvalidIssuer   = errors.New("leego: jwt: invalid issuer")
	errTokenInvalidAudience = errors.New("leego: jwt: invalid audience")
)

// Claims 定义验证 JWT 令牌的接口类型
type Claims interface {
	Validate() error
}

var _ Claims = &StandardClaims{}

// StandardClaims 定义 JWT 标准声明字段的数据结构
type StandardClaims struct {
	Id        string `json:"jti,omitempty"` // JWT ID
	Issuer    string `json:"iss,omitempty"` // 签发者
	Subject   string `json:"sub,omitempty"` // 主题
	Audience  string `json:"aud,omitempty"` // 受众
	IssuedAt  int64  `json:"iat,omitempty"` // 签发时间
	NotBefore int64  `json:"nbf,omitempty"` // 生效时间
	ExpiresAt int64  `json:"exp,omitempty"` // 过期时间
}

// Validate 验证标准声明字段
func (own *StandardClaims) Validate() error {
	now := time.Now().Unix()

	// 检查过期时间
	if own.ExpiresAt > 0 && now >= own.ExpiresAt {
		return errTokenExpired
	}

	// 检查生效时间
	if own.NotBefore > 0 && now < own.NotBefore {
		return errTokenNotValidYet
	}

	// 检查签发时间（如果签发时间在未来，视为无效）
	// 允许60秒时钟偏差
	if own.IssuedAt > 0 && own.IssuedAt > now+60 {
		return errTokenIssuedFuture
	}

	return nil
}

// ValidateIssuer 验证签发者
func (own *StandardClaims) ValidateIssuer(issuer string) error {
	if issuer != "" && own.Issuer != issuer {
		return errTokenInvalidIssuer
	}

	return nil
}

// ValidateAudience 验证受众
func (own *StandardClaims) ValidateAudience(audience string) error {
	if own.Audience != audience {
		return errTokenInvalidAudience
	}

	return nil
}

// UnixTime Helper functions for common claim operations
func UnixTime(t time.Time) int64 {
	return t.Unix()
}

func FromUnixTime(sec int64) time.Time {
	return time.Unix(sec, 0)
}
