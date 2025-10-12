package jwt

import (
	"time"
)

var _ Claims = NewClaims()

// Claims 定义 JWT 载荷的接口
type Claims interface {
	/*
		WithJwtId 设置令牌的受众（jti）

		使用：
		  - 重放攻击防护的核心
		  - 每个令牌的唯一标识符
		  - 配合服务端存储实现一次性使用检查
	*/
	WithJwtId(aud string) Claims

	/*
		WithIssuer 设置令牌的签发者（iss）

		使用：
		  - 用于在多租户或分布式系统中识别令牌的签发源
		  - 可以是任意字符串，但通常使用 URL 形式以确保全局唯一性

		注意：
		  - 必须验证：服务端必须维护可信的签发者白名单
		  - 严格匹配：验证时应进行精确字符串匹配，包括协议和路径
		  - 防止混淆：避免使用过于简单的标识符，如 "app"、"server"
	*/
	WithIssuer(iss string) Claims

	/*
		WithIssuedAt 设置令牌的签发时间（idt）

		使用：
		  - 用于计算令牌年龄和检测异常
		  - 辅助实现令牌撤销列表
	*/
	WithIssuedAt(iat int64) Claims

	/*
		WithNotBefore 设置令牌的生效时间（nbf）

		使用：
		  - 提供令牌激活的时间控制
		  - 可用于实现定时任务、预定操作

		注意：
		  - 验证时考虑时钟偏差（通常5分钟）

		示例：
			// 预定维护令牌
				"nbf": 1719847275,  // 在指定时间后生效
				"exp": 1719850875,  // 1小时后过期
				"sub": "maintenance_bot",
				"scope": "system.maintenance"

			// 预售活动令牌
				"nbf": 1720432275,  // 活动开始时间生效
				"sub": "premium_user_456",
				"access": "early_bird"
	*/
	WithNotBefore(nbf int64) Claims

	/*
		WithExpiration 设置令牌的过期时间（exp）

		使用：
		  - 使用 Unix 时间戳（秒级，不是毫秒）
		  - 建议设置较短的过期时间

		注意：
		  - 验证时考虑时钟偏差（通常5分钟）

		示例：
			// Access Token (短期)
				"exp": 1719847275,  // 30分钟后过期
				"sub": "user_123",
				"aud": "https://api.example.com"

			// Refresh Token (长期，存储安全位置)
				"exp": 1720432275,  // 7天后过期
				"sub": "user_123",
				"purpose": "refresh"
	*/
	WithExpiration(exp int64) Claims

	/*
		WithSubject 设置令牌的主题（sub）

		使用：
		  - 主题标识符应该是持久、不可变的
		  - 一旦确定，在整个系统生命周期内不应更改

		注意：
		  - 不要使用易变信息：如邮箱、用户名，这些可能改变
		  - 保持唯一性：在整个用户池中必须唯一
		  - 隐私考虑：避免使用有意义的标识符（如身份证号）

		示例：
			// 推荐格式
				"sub": "user_12345"                                // 带前缀的ID
				"sub": "auth0|5e7a8b9c0d1e2f3a4b5c6d7e"            // 提供商格式
				"sub": "uuid:550e8400-e29b-41d4-a716-446655440000" // UUID格式
	*/
	WithSubject(sub string) Claims

	/*
		WithAudience 设置令牌的受众（aud）

		使用：
		  - 指定令牌的合法消费者
		  - 可以是单个值或数组，表示令牌可被多个服务使用

		示例：
			// 单一服务
				"aud": "https://api.example.com"

			// 微服务架构 - 多个受众
				"aud": [
					"https://api.userservice.com",
					"https://api.orderservice.com",
					"https://api.notificationservice.com"
				]
	*/
	WithAudience(aud string) Claims

	// Get 获取指定字段
	Get(key string) (any, bool)
	// With 添加自定义字段
	With(key string, value any) Claims
	// Delete 删除指定字段
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
