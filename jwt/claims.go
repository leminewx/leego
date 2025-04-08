package jwt

import (
	"errors"
	"time"
)

// 定义错误类型
var (
	errInvalidToken     = errors.New("leego: jwt: invalid token")
	errExpiredToken     = errors.New("leego: jwt: token has expired")
	errIneffectiveToken = errors.New("leego: jwt: token not yet valid")
	errInvalidSignature = errors.New("leego: jwt: invalid signature")
)

// Claims 接口定义
type Claims interface {
	// Validate 校验令牌是否有效
	Validate() error
}

// StandardClaims 定义标准 JWT 声明
type StandardClaims struct {
	// IssuedAt(iss) 签发时间
	IssuedAt int64 `json:"iat,omitempty"`
	// 生效时间
	NotBefore int64 `json:"nbf,omitempty"`
	// 过期时间
	ExpiredAt int64 `json:"exp,omitempty"`
	// 签发者
	Issuer string `json:"iss,omitempty"`
	// 主题
	Subject string `json:"sub,omitempty"`
	// 接收方
	Audience string `json:"aud,omitempty"`
	// 唯一标识
	JwtId string `json:"jti,omitempty"`
}

// Validate 校验令牌是否有效
func (own *StandardClaims) Validate() error {
	now := time.Now().Unix()

	// 检查令牌是否过期
	if own.ExpiredAt > 0 && now > own.ExpiredAt {
		return errExpiredToken
	}

	// 检查令牌是否未生效
	if own.NotBefore > 0 && now < own.NotBefore {
		return errIneffectiveToken
	}

	return nil
}
