package cors

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"gitee.com/leminewx/gokit/ggid"
)

type Config struct {
	// 元数据
	Id    string `json:"id" xml:"id"`
	Type  string `json:"type" xml:"type"`
	Scope string `json:"scope" xml:"scope"`

	// IsEnabled 是否启用
	IsEnabled int32 `json:"is_enabled" xml:"is_enabled"`

	// MaxAge 表示资源缓存时间（一般为24小时：86400，单位：秒）
	MaxAge int64 `json:"max_age" xml:"max_age"`
	// AllowOrigins 允许源列表
	AllowOrigins []string `json:"allow_origins" xml:"allow_origins"`
	// AllowMethods 允许的方法列表
	AllowMethods []string `json:"allow_methods" xml:"allow_methods"`
	// AllowHeaders 允许的请求头列表
	AllowHeaders []string `json:"allow_headers" xml:"allow_headers"`
	// ExposeHeaders 允许的响应头列表
	ExposeHeaders []string `json:"expose_headers" xml:"expose_headers"`
}

// NewDefaultConfig 创建默认跨域中间件配置
func NewDefaultConfig() *Config {
	return &Config{
		Id:            ggid.New().GetShort(),
		Type:          "cors",
		MaxAge:        86400, // 默认预检请求缓存时间（24小时）\
		IsEnabled:     1,
		AllowOrigins:  make([]string, 0),
		AllowMethods:  make([]string, 0),
		AllowHeaders:  make([]string, 0),
		ExposeHeaders: make([]string, 0),
	}
}

// Enable 启用中间件
func (own *CORS) Enable() {
	atomic.StoreInt32(&own.config.IsEnabled, 1)
}

// Disable 禁用中间件
func (own *CORS) Disable() {
	atomic.StoreInt32(&own.config.IsEnabled, 0)
}

// SetScope 设置作用域
func (own *CORS) SetScope(scope string) {
	if scope == "" {
		scope = "/"
	}

	own.lock.Lock()
	own.config.Scope = scope
	own.lock.Unlock()
}

// SetMaxAge 设置前端缓存信息的最大时间（单位：秒）
func (own *CORS) SetMaxAge(seconds int64) {
	if seconds < 0 {
		seconds = 0
	}

	own.lock.Lock()
	own.config.MaxAge = seconds
	own.maxAgeString = strconv.Itoa(int(own.config.MaxAge))
	own.lock.Unlock()
}

// SetAllowOrigins 设置可跨域访问的源，若不设置，则允许所有源访问
func (own *CORS) SetAllowOrigins(origins ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 如果不设置源，则允许所有源，重置配置
	if len(origins) == 0 {
		own.allowAllOrigins = true   // 允许所有源
		own.allowCredentials = false // 通配符源不能使用凭证
		own.config.AllowOrigins = make([]string, 0)
		own.allowOrigins = make(map[string]struct{})
	}

	// 添加源，则在原有基础上添加
	for _, origin := range origins {
		if origin == "" {
			continue
		}

		if origin == "*" {
			own.allowAllOrigins = true
			own.allowCredentials = false
			own.config.AllowOrigins = make([]string, 0)
			own.allowOrigins = make(map[string]struct{})
		}

		if _, ok := own.allowOrigins[origin]; !ok {
			own.config.AllowOrigins = append(own.config.AllowOrigins, origin)
			own.allowOrigins[origin] = struct{}{}
		}
	}

	// 如果有具体源，默认需要凭证
	if len(own.allowOrigins) > 0 {
		own.allowCredentials = true
	}
}

// SetAllowMethods 设置可跨域访问的请求方法
func (own *CORS) SetAllowMethods(methods ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	for _, method := range methods {
		if method != "" {
			own.config.AllowMethods = append(own.config.AllowMethods, method)
			own.allowMethods[method] = struct{}{}
		}
	}

	if len(own.config.AllowMethods) > 0 {
		own.allowMethodString = strings.Join(own.config.AllowMethods, ", ")
	}
}

// SetAllowHeaders 设置可跨域访问的请求头字段
func (own *CORS) SetAllowHeaders(headers ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	for _, header := range headers {
		if header != "" {
			own.config.AllowHeaders = append(own.config.AllowHeaders, header)
			own.allowHeaders[header] = struct{}{}
		}
	}

	if len(own.config.AllowHeaders) > 0 {
		own.allowHeaderString = strings.Join(own.config.AllowHeaders, ", ")
	}
}

// SetExposeHeaders 设置响应跨域请求的额外响应头字段
func (own *CORS) SetExposeHeaders(headers ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	for _, header := range headers {
		if header != "" {
			own.config.ExposeHeaders = append(own.config.ExposeHeaders, header)
			own.exposeHeaders[header] = struct{}{}
		}
	}

	if len(own.config.ExposeHeaders) > 0 {
		own.exposeHeaderString = strings.Join(own.config.ExposeHeaders, ", ")
	}
}

// SyncConfig 同步配置
func (own *CORS) SyncConfig(config any) error {
	conf, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("leego: cors: failed to sync config, config is not *cors.Config")
	}
	if config == nil {
		return fmt.Errorf("leego: cors: failed to sync config, config is nil")
	}

	// 重置配置
	own.lock.Lock()
	own.resetConfig()
	own.lock.Unlock()

	// 同步配置参数
	if conf.IsEnabled == 0 {
		own.Disable()
	} else {
		own.Enable()
	}

	own.SetMaxAge(conf.MaxAge)
	own.SetAllowOrigins(conf.AllowOrigins...)
	own.SetAllowHeaders(conf.AllowHeaders...)
	own.SetAllowMethods(conf.AllowMethods...)
	own.SetExposeHeaders(conf.ExposeHeaders...)
	return nil
}

// resetConfig 重置配置
func (own *CORS) resetConfig() {
	own.config.AllowOrigins = make([]string, 0)
	own.config.AllowMethods = make([]string, 0)
	own.config.AllowHeaders = make([]string, 0)
	own.config.ExposeHeaders = make([]string, 0)

	own.allowOrigins = make(map[string]struct{})
	own.allowMethods = make(map[string]struct{})
	own.allowHeaders = make(map[string]struct{})
	own.exposeHeaders = make(map[string]struct{})

	own.maxAgeString = ""
	own.allowMethodString = ""
	own.allowHeaderString = ""
	own.exposeHeaderString = ""
}

// GetID 获取ID
func (own *CORS) GetID() string {
	return own.config.Id
}

// GetConfig 获取跨域配置
func (own *CORS) GetConfig() any {
	config := &Config{
		Id:            own.config.Id,
		Type:          "cors",
		Scope:         own.config.Scope,
		MaxAge:        atomic.LoadInt64(&own.config.MaxAge),
		IsEnabled:     atomic.LoadInt32(&own.config.IsEnabled),
		AllowOrigins:  make([]string, 0),
		AllowMethods:  make([]string, 0),
		AllowHeaders:  make([]string, 0),
		ExposeHeaders: make([]string, 0),
	}

	own.lock.RLock()
	for _, origin := range own.config.AllowOrigins {
		config.AllowOrigins = append(config.AllowOrigins, origin)
	}

	for _, method := range own.config.AllowMethods {
		config.AllowMethods = append(config.AllowMethods, method)
	}

	for _, header := range own.config.AllowHeaders {
		config.AllowHeaders = append(config.AllowHeaders, header)
	}

	for _, header := range own.config.ExposeHeaders {
		config.ExposeHeaders = append(config.ExposeHeaders, header)
	}
	own.lock.RUnlock()

	return config
}
