package authenticator

import (
	"strings"
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego/middleware/authenticator/jwt"
	"gitee.com/leminewx/leego/utils/ggid"
)

type (
	Config struct {
		// 元数据
		Id        string        `json:"id" xml:"id"`
		Name      string        `json:"name" xml:"name"`
		Scope     string        `json:"scope" xml:"scope"`
		Algorithm jwt.Algorithm `json:"algorithm" xml:"algorithm"`

		// SkipAbsolutePathMap 鉴权时跳过的绝对路径
		SkipAbsolutePathMap map[string]struct{} `json:"skip_absolute_path_map" xml:"skip_absolute_path_map"`
		// SkipWildcardPathSlice 鉴权时跳过的通配路径
		SkipWildcardPathSlice []string `json:"skip_wildcard_path_slice" xml:"skip_wildcard_path_slice"`

		// JWTStandardClaimsConfig JWT 声明相关鉴权配置
		*JWTStandardClaimsConfig `json:"jwt_standard_claims_config" xml:"jwt_standard_claims_config"`

		lock sync.RWMutex
	}

	JWTStandardClaimsConfig struct {
		Validation          int32  `json:"validation" xml:"validation"`
		NotBeforeSeconds    int64  `json:"not_before_seconds" xml:"not_before_seconds"`
		ExpiresAfterSeconds int64  `json:"expires_after_seconds" xml:"expires_after_seconds"`
		ExceptedIssuer      string `json:"excepted_issuer" xml:"excepted_issuer"`
		ExceptedAudience    string `json:"excepted_audience" xml:"excepted_audience"`
	}
)

// NewDefaultConfig 新建一个 JWT 认证管理器的默认配置
func NewDefaultConfig(name string) *Config {
	return &Config{
		// 元数据
		Id:   ggid.NewGID().String(),
		Name: name,

		// 跳过路径
		SkipAbsolutePathMap:   make(map[string]struct{}),
		SkipWildcardPathSlice: make([]string, 0),

		// JWT 配置
		JWTStandardClaimsConfig: &JWTStandardClaimsConfig{
			Validation:          1,         // 默认启用 JWT 认证
			ExpiresAfterSeconds: 24 * 3600, // 默认令牌1天有效
		},
	}
}

// SetScope 设置作用域
func (own *Authenticator) SetScope(scope string) {
	own.lock.Lock()
	own.Scope = scope
	own.lock.Unlock()
}

// WithValidateStandardClaims 设置是否验证标准声明
func (own *Authenticator) WithValidateStandardClaims(validation bool) *Authenticator {
	if validation {
		atomic.StoreInt32(&own.Validation, 1)
	} else {
		atomic.StoreInt32(&own.Validation, 0)
	}

	return own
}

// WithExceptedIssuer 设置期望的签发者
func (own *Authenticator) WithExceptedIssuer(issuer string) *Authenticator {
	own.lock.Lock()
	own.ExceptedIssuer = strings.TrimSpace(issuer)
	own.lock.Unlock()
	return own
}

// WithExceptedAudience 设置期望的接收者
func (own *Authenticator) WithExceptedAudience(audience string) *Authenticator {
	own.lock.Lock()
	own.ExceptedAudience = strings.TrimSpace(audience)
	own.lock.Unlock()
	return own
}

// WithNotBeforeSeconds 设置令牌在多少秒之后开始生效（签发时间之后）
func (own *Authenticator) WithNotBeforeSeconds(seconds int64) *Authenticator {
	if seconds > 0 {
		own.lock.Lock()
		own.NotBeforeSeconds = seconds
		own.lock.Unlock()
	}

	return own
}

// WithExpiresAfterSeconds 设置令牌在生效多少秒之后过期
func (own *Authenticator) WithExpiresAfterSeconds(seconds int64) *Authenticator {
	if seconds > 0 {
		own.lock.Lock()
		own.ExpiresAfterSeconds = seconds
		own.lock.Unlock()
	}

	return own
}

// WithSkipPaths 设置跳过路径（白名单）
func (own *Authenticator) WithSkipPaths(paths ...string) *Authenticator {
	own.lock.Lock()
	defer own.lock.Unlock()

	for _, path := range paths {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}

		if strings.HasSuffix(path, "/*") {
			own.SkipWildcardPathSlice = append(own.SkipWildcardPathSlice, strings.TrimSuffix(path, "/*"))
			continue
		}

		own.SkipAbsolutePathMap[path] = struct{}{}
	}

	return own
}

// Sync 同步配置文件
func (own *Authenticator) Sync(config *Config) {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 同步跳过路径参数
	own.SkipAbsolutePathMap = config.SkipAbsolutePathMap
	own.SkipWildcardPathSlice = config.SkipWildcardPathSlice

	// config 没有 JWT 配置参数
	if config.JWTStandardClaimsConfig == nil {
		own.JWTStandardClaimsConfig = &JWTStandardClaimsConfig{}
		return
	}

	// config 有 JWT 配置参数
	if own.JWTStandardClaimsConfig != nil {
		own.JWTStandardClaimsConfig = &JWTStandardClaimsConfig{}
	}

	// 同步 JWT 配置参数
	own.Validation = config.Validation
	own.NotBeforeSeconds = config.NotBeforeSeconds
	own.ExpiresAfterSeconds = config.ExpiresAfterSeconds
	own.ExceptedIssuer = config.ExceptedIssuer
	own.ExceptedAudience = config.ExceptedAudience
}
