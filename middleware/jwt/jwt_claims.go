package jwt

import (
	"time"
)

/*
iss:

	当验证令牌时，服务端可以检查 iss 声明来确认令牌的发行者是否是预期的发行者。这对于防止未经授权的实体签发令牌非常重要。
	如果 iss 不匹配，则应当拒绝访问，因为这意味着令牌可能不是由预期的发行者签发的。
	此外，在多发行者环境中，iss 声明还可以帮助区分不同来源的令牌，允许系统根据不同的发行者配置不同的验证规则或权限策略。
	总之，iss 提供了一种方法来跟踪和验证令牌的来源，确保只有受信任的发行者签发的令牌才能被接受。这增加了系统的安全性和可靠性。

jti:

	1.防止重放攻击：通过记录已经使用过的 jti，接收方可以检测到重复提交的同一个令牌，并拒绝接受这些重复的令牌，这样就有效地防止了重放攻击。
	2.追踪和调试：jti 可以帮助在日志或监控系统中追踪特定的令牌，便于问题排查和审计。
	3.一次性令牌：当令牌作为一次性令牌（例如密码重置链接中的令牌）时，jti 确保了该令牌只能被使用一次。
	4.撤销机制：虽然令牌通常是无状态的，但如果有需要，可以通过 jti 来实现基于令牌的撤销机制，即维护一个黑名单来存储已经被撤销的 jti 列表。

	在验证令牌时，服务端可以检查 jti 是否已经被使用过或者是否存在于黑名单中。如果 jti 已经存在，则应当拒绝访问，因为这可能意味着令牌正在被重放攻击利用，或者是已经被撤销的令牌。
	总之，jti 提供了一种方法来增强令牌的安全性和管理能力，尤其是在需要保证令牌唯一性的情况下。
*/

var _ Claims = NewClaims()

// Claims 定义 JWT 载荷的接口
type Claims interface {
	// WithNotBefore 设置令牌的生效时间（nbf），生效之前不可用
	WithNotBefore(nbf int64) Claims
	// WithExpiration 设置令牌的过期时间（exp），过期之后将失效
	WithExpiration(exp int64) Claims
	// WithIssuedAt 设置令牌的签发时间（idt）
	WithIssuedAt(iat int64) Claims

	/*
		WithIssuer 设置令牌的签发者（iss）
		  - 当验证令牌时，服务端可以检查 iss 声明来确认令牌的发行者是否是预期的发行者。这对于防止未经授权的实体签发令牌非常重要。
		  - 如果 iss 不匹配，则应当拒绝访问，因为这意味着令牌可能不是由预期的发行者签发的。
		  - 此外，在多发行者环境中，iss 声明还可以帮助区分不同来源的令牌，允许系统根据不同的发行者配置不同的验证规则或权限策略。
		  - 总之，iss 提供了一种方法来跟踪和验证令牌的来源，确保只有受信任的发行者签发的令牌才能被接受。这增加了系统的安全性和可靠性。
	*/
	WithIssuer(iss string) Claims

	// WithSubject 设置令牌的主体（sub），用来表示该令牌所代表的主要对象，通常是唯一标识符
	WithSubject(sub string) Claims
	// WithAudience 设置令牌的受众（aud），通常是一个服务或应用程序的标识符
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
