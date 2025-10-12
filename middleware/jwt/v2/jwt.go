package jwt

import (
	"errors"
	"time"
)

var (
	errTokenExpired      = errors.New("leego: jwt: token is expired")
	errTokenNotValidYet  = errors.New("leego: jwt: token is not valid yet")
	errTokenIssuedFuture = errors.New("leego: jwt: token issued in the future")
)

// StandardClaims 定义 JWT 标准声明字段的数据结构
type StandardClaims struct {
	Id        string `json:"jti,omitempty"` // JWT ID
	Issuer    string `json:"iss,omitempty"` // 签发者
	Subject   string `json:"sub,omitempty"` // 主题
	Audience  string `json:"aud,omitempty"` // 受众
	NotBefore int64  `json:"nbf,omitempty"` // 生效时间
	ExpiresAt int64  `json:"exp,omitempty"` // 过期时间
	IssuedAt  int64  `json:"iat,omitempty"` // 签发时间

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
