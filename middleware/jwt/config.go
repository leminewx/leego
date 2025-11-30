package jwt

import (
	"fmt"
	"strings"
	"sync/atomic"

	"gitee.com/leminewx/gokit/ggid"
)

type (
	Config struct {
		// 元数据
		Id        string    `json:"id" xml:"id"`
		Type      string    `json:"type" xml:"type"`
		Scope     string    `json:"scope" xml:"scope"`
		Algorithm Algorithm `json:"algorithm" xml:"algorithm"`

		// IsEnabled 是否启用
		IsEnabled int32 `json:"is_enabled" xml:"is_enabled"`

		// SkipAbsolutePaths 鉴权时跳过的绝对路径
		SkipAbsolutePaths map[string]struct{} `json:"skip_absolute_paths" xml:"skip_absolute_paths"`
		// SkipWildcardPaths 鉴权时跳过的通配路径
		SkipWildcardPaths []string `json:"skip_wildcard_paths" xml:"skip_wildcard_paths"`

		// StandardClaimsConfig JWT 声明相关鉴权配置
		*StandardClaimsConfig `json:"jwt_standard_claims_config" xml:"jwt_standard_claims_config"`
	}

	StandardClaimsConfig struct {
		Validation          int32  `json:"validation" xml:"validation"`
		NotBeforeSeconds    int64  `json:"not_before_seconds" xml:"not_before_seconds"`
		ExpiresAfterSeconds int64  `json:"expires_after_seconds" xml:"expires_after_seconds"`
		ExceptedIssuer      string `json:"excepted_issuer" xml:"excepted_issuer"`
		ExceptedAudience    string `json:"excepted_audience" xml:"excepted_audience"`
	}
)

// NewDefaultConfig 新建一个 JWT 认证管理器的默认配置
func NewDefaultConfig() *Config {
	return &Config{
		// 元数据
		Id:   ggid.New().GetShort(),
		Type: "jwt",

		// 默认启用
		IsEnabled: 1,

		// 跳过路径
		SkipAbsolutePaths: make(map[string]struct{}),
		SkipWildcardPaths: make([]string, 0),

		// JWT 配置
		StandardClaimsConfig: &StandardClaimsConfig{
			Validation:          1,         // 默认启用 JWT 认证
			ExpiresAfterSeconds: 24 * 3600, // 默认令牌1天有效
		},
	}
}

// Enable 启用中间件
func (own *Manager) Enable() {
	atomic.StoreInt32(&own.config.IsEnabled, 1)
}

// Disable 禁用中间件
func (own *Manager) Disable() {
	atomic.StoreInt32(&own.config.IsEnabled, 0)
}

// SetScope 设置作用域
func (own *Manager) SetScope(scope string) {
	if scope == "" {
		scope = "/"
	}

	own.lock.Lock()
	own.config.Scope = scope
	own.lock.Unlock()
}

// SetValidateStandardClaims 设置是否验证标准声明
func (own *Manager) SetValidateStandardClaims(validation bool) {
	if validation {
		atomic.StoreInt32(&own.config.Validation, 1)
	} else {
		atomic.StoreInt32(&own.config.Validation, 0)
	}
}

// SetExceptedIssuer 设置期望的签发者
func (own *Manager) SetExceptedIssuer(issuer string) {
	own.lock.Lock()
	own.config.ExceptedIssuer = strings.TrimSpace(issuer)
	own.lock.Unlock()
}

// SetExceptedAudience 设置期望的接收者
func (own *Manager) SetExceptedAudience(audience string) {
	own.lock.Lock()
	own.config.ExceptedAudience = strings.TrimSpace(audience)
	own.lock.Unlock()
}

// SetNotBeforeSeconds 设置令牌在多少秒之后开始生效（签发时间之后）
func (own *Manager) SetNotBeforeSeconds(seconds int64) {
	if seconds > 0 {
		own.lock.Lock()
		own.config.NotBeforeSeconds = seconds
		own.lock.Unlock()
	}
}

// SetExpiresAfterSeconds 设置令牌在生效多少秒之后过期
func (own *Manager) SetExpiresAfterSeconds(seconds int64) {
	if seconds > 0 {
		own.lock.Lock()
		own.config.ExpiresAfterSeconds = seconds
		own.lock.Unlock()
	}
}

// SetSkipPaths 设置跳过路径（白名单）
func (own *Manager) SetSkipPaths(paths ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	for _, path := range paths {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}

		if strings.HasSuffix(path, "*") {
			own.config.SkipWildcardPaths = append(own.config.SkipWildcardPaths, strings.TrimSuffix(path, "*"))
			continue
		}

		own.config.SkipAbsolutePaths[path] = struct{}{}
	}
}

// GetID 获取ID
func (own *Manager) GetID() string {
	return own.config.Id
}

// GetConfig 获取认证器配置
func (own *Manager) GetConfig() any {
	config := &Config{
		// 基本信息
		Id:        own.config.Id,
		Type:      "jwt",
		Scope:     own.config.Scope,
		Algorithm: own.config.Algorithm,
		IsEnabled: atomic.LoadInt32(&own.config.IsEnabled),

		// 跳过路径
		SkipAbsolutePaths: make(map[string]struct{}),
		SkipWildcardPaths: make([]string, 0),
	}

	own.lock.RLock()
	for _, path := range own.config.SkipWildcardPaths {
		config.SkipWildcardPaths = append(config.SkipWildcardPaths, path)
	}

	for path := range own.config.SkipAbsolutePaths {
		config.SkipAbsolutePaths[path] = struct{}{}
	}

	own.config.StandardClaimsConfig = &StandardClaimsConfig{
		Validation:          own.config.Validation,
		NotBeforeSeconds:    own.config.NotBeforeSeconds,
		ExpiresAfterSeconds: own.config.ExpiresAfterSeconds,
		ExceptedIssuer:      own.config.ExceptedIssuer,
		ExceptedAudience:    own.config.ExceptedAudience,
	}
	own.lock.RUnlock()

	return config
}

// SyncConfig 同步配置文件
func (own *Manager) SyncConfig(config any) error {
	conf, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("leego: authenticator: failed to sync config, config is not *authenticator.Config")
	}
	if config == nil {
		return fmt.Errorf("leego: authenticator: failed to sync config, config is nil")
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	// 同步跳过路径参数
	own.config.SkipAbsolutePaths = conf.SkipAbsolutePaths
	own.config.SkipWildcardPaths = conf.SkipWildcardPaths

	// config 没有 JWT 配置参数
	if conf.StandardClaimsConfig == nil {
		own.config.StandardClaimsConfig = &StandardClaimsConfig{}
		return nil
	}

	// config 有 JWT 配置参数
	if own.config.StandardClaimsConfig != nil {
		own.config.StandardClaimsConfig = &StandardClaimsConfig{}
	}

	// 同步 JWT 配置参数
	own.config.Validation = conf.Validation
	own.config.NotBeforeSeconds = conf.NotBeforeSeconds
	own.config.ExpiresAfterSeconds = conf.ExpiresAfterSeconds
	own.config.ExceptedIssuer = conf.ExceptedIssuer
	own.config.ExceptedAudience = conf.ExceptedAudience
	return nil
}
