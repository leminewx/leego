package jwt

import (
	"time"
)

// Claims 定义 JWT 声明必须实现的能力。
type Claims interface {
	// Validate 验证标准声明中的所有时间字段。
	Validate() error
	// ValidateWithIssuerAndAudiences 校验时间字段、颁发者与受众。
	ValidateWithIssuerAndAudiences(expectedIssuer, expectedAudience string) error
}

var _ Claims = &StandardClaims{}

// StandardClaims 定义 JWT 标准声明的数据结构
type StandardClaims struct {
	/*
		Id 令牌的唯一标识（jti）

		使用：
		  - 重放攻击防护的核心
		  - 每个令牌的唯一标识符
		  - 配合服务端存储实现一次性使用检查
		  - 若你在服务端维护了一个已使用的 JWT ID 列表（或者使用黑名单/白名单），那你需要验证这个 jti 是否已经被使用过。如果使用过，则拒绝该令牌；如果没有使用过，则将其记录，并在有效期内不再接受相同的 jti

		注意：
		  - 使用jti防重放会增加服务端的存储和检查开销，因此需要根据安全需求权衡
	*/
	Id string `json:"jti,omitempty"`

	/*
		Issuer 令牌的签发者（iss）

		使用：
		  - 用于在多租户或分布式系统中识别令牌的签发源
		  - 可以是任意字符串，但通常使用 URL 形式以确保全局唯一性

		注意：
		  - 必须验证：服务端必须维护可信的签发者白名单
		  - 严格匹配：验证时应进行精确字符串匹配，包括协议和路径
		  - 防止混淆：避免使用过于简单的标识符，如 "app"、"server"
	*/
	Issuer string `json:"iss,omitempty"`

	/*
		Subject 令牌的主题（sub）

		使用：
		  - 主题标识符应该是持久、不可变的
		  - 一旦确定，在整个系统生命周期内不应更改
		  - sub 通常是可选验证的，因为通常我们通过其他方式（如数据库）来验证用户的存在和状态

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
	Subject string `json:"sub,omitempty"`

	/*
		Audiences 令牌的受众（aud）

		使用：
		  - 指定令牌的合法消费者
		  - 可以是单个值或数组，表示令牌可被多个服务使用
		  - 每个服务在验证 JWT 时，都必须检查 aud 是否包含自己（即服务的标识）。如果令牌的受众不包含当前服务，就必须拒绝该令牌

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
	Audiences []string `json:"aud,omitempty"`

	/*
		IssuedAt 令牌的签发时间（idt）

		使用：
		  - 用于计算令牌年龄和检测异常
		  - 辅助实现令牌撤销列表
	*/
	IssuedAt int64 `json:"iat,omitempty"`

	/*
		NotBefore 令牌的生效时间（nbf）

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
	NotBefore int64 `json:"nbf,omitempty"`

	/*
		ExpiresAt 令牌的过期时间（exp）

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
	ExpiresAt int64 `json:"exp,omitempty"`
}

// Validate 验证标准声明中的所有时间字段。
//
// 该方法等价于 ValidateWithLeeway(0)，即不允许任何时钟偏差。
func (own StandardClaims) Validate() error {
	return own.ValidateWithLeeway(0)
}

// ValidateWithLeeway 验证标准声明中的所有时间字段，并允许指定的时钟偏差。
//
// 参数 leeway 表示前后各容忍的时长：
//   - exp 在 now+leeway 之后才视为过期；
//   - nbf 在 now-leeway 之前就视为生效；
//   - iat 在 now+leeway 之后才视为签发时间异常。
//
// 负值会被修正为 0，避免调用方误传负偏差导致校验变严。
func (own StandardClaims) ValidateWithLeeway(leeway time.Duration) error {
	if leeway < 0 {
		leeway = 0
	}

	seconds := int64(leeway / time.Second)
	now := time.Now().Unix()

	// 检查过期时间。
	if own.ExpiresAt > 0 && own.ExpiresAt+seconds < now {
		return ErrExpiredToken
	}

	// 检查生效时间。
	if own.NotBefore > 0 && own.NotBefore-seconds > now {
		return ErrNotValidYet
	}

	// 检查签发时间（未来签发视为无效）。
	if own.IssuedAt > 0 && own.IssuedAt-seconds > now {
		return ErrInvalidIssuanceTime
	}

	return nil
}

// ValidateWithIssuerAndAudiences 验证标准声明中的所有时间字段、签发者和受众。
func (own StandardClaims) ValidateWithIssuerAndAudiences(expectedIssuer, expectedAudience string) error {
	return own.ValidateWithIssuerAndAudiencesAndLeeway(expectedIssuer, expectedAudience, 0)
}

// ValidateWithIssuerAndAudiencesAndLeeway 验证时间字段、签发者和受众，并允许时钟偏差。
//
// 校验规则：
//   - expectedIssuer 非空时必须与 iss 完全一致；
//   - expectedAudience 非空时必须命中 aud 列表中的某一项；
//   - 任一为空表示 “不限制该维度”。
func (own StandardClaims) ValidateWithIssuerAndAudiencesAndLeeway(expectedIssuer, expectedAudience string, leeway time.Duration) error {
	if err := own.ValidateWithLeeway(leeway); err != nil {
		return err
	}

	if expectedIssuer != "" && own.Issuer != expectedIssuer {
		return ErrInvalidIssuer
	}

	if expectedAudience == "" {
		return nil
	}

	for _, audience := range own.Audiences {
		if audience == expectedAudience {
			return nil
		}
	}

	return ErrInvalidAudience
}
