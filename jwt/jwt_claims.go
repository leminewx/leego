package jwt

import (
	"time"
)

// Claims 接口定义
type Claims interface {
	// WithNotBefore 设置令牌的生效时间，生效之前不可用
	WithNotBefore(nbf int64) Claims
	// WithExpiration 设置令牌的过期时间，过期之后将失效
	WithExpiration(exp int64) Claims
	// WithIssuedAt 设置令牌的签发时间
	WithIssuedAt(iat int64) Claims
	// WithIssuer 设置令牌的签发者
	WithIssuer(iss string) Claims
	// WithSubject 设置令牌的主体，用来表示该令牌所代表的主要对象，通常是唯一标识符
	WithSubject(sub string) Claims
	// WithAudience 设置令牌的受众，通常是一个服务或应用程序的标识符
	WithAudience(aud string) Claims

	// With 添加自定义字段
	With(key string, value any) Claims
	// Get 获取某个字段
	Get(key string) (any, bool)
	// Delete 删除某个字段
	Delete(key string)
	// Validate 校验令牌是否有效
	Validate() error
}

var _ Claims = NewClaims()

// claims 定义 JWT Claims 的数据结构
type claims map[string]any

// NewClaims 新建一个 JWT Claims
func NewClaims() claims { return make(claims) }

// WithNotBefore 设置令牌的生效时间，生效之前不可用
func (own claims) WithNotBefore(nbf int64) Claims {
	own["nbf"] = nbf
	return own
}

// WithExpiration 设置令牌的过期时间，过期之后将失效
func (own claims) WithExpiration(exp int64) Claims {
	own["exp"] = exp
	return own
}

// WithIssuedAt 设置令牌的签发时间
func (own claims) WithIssuedAt(iat int64) Claims {
	own["iat"] = iat
	return own
}

// WithIssuer 设置令牌的签发者
func (own claims) WithIssuer(iss string) Claims {
	own["iss"] = iss
	return own
}

// WithSubject 设置令牌的主体，用来表示该令牌所代表的主要对象，通常是唯一标识符
func (own claims) WithSubject(sub string) Claims {
	own["sub"] = sub
	return own
}

// WithAudience 设置令牌的受众，通常是一个服务或应用程序的标识符
func (own claims) WithAudience(aud string) Claims {
	own["aud"] = aud
	return own
}

// With 添加自定义字段
func (own claims) With(key string, value any) Claims {
	if key != "iat" {
		own[key] = value
	}

	return own
}

// Get 获取指定字段
func (own claims) Get(key string) (any, bool) {
	val, ok := own[key]
	return val, ok
}

// Delete 删除指定字段
func (own claims) Delete(key string) {
	delete(own, key)
}

// Validate 验证令牌是否有效
func (own claims) Validate() error {
	now := time.Now().Unix()

	// 检查是否过期
	if exp, ok := own["exp"]; ok {
		if e, ok := exp.(float64); ok {
			if int64(e) < now {
				return errExpiredToken
			}
		} else {
			return errInvalidToken
		}
	} else {
		return errInvalidToken
	}

	// 检查是否生效
	if nbf, ok := own["nbf"]; ok {
		if n, ok := nbf.(float64); ok {
			if int64(n) > now {
				return errIneffectiveToken
			}
		} else {
			return errInvalidToken
		}
	}

	return nil
}
