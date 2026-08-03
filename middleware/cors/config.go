package cors

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// Config 定义 CORS 配置信息的结构类型
type Config struct {
	IsEnabled        bool     `json:"is_enabled"`        // 是否启用
	AllowCredentials bool     `json:"allow_credentials"` // 是否允许使用凭证
	MaxAgeSeconds    int64    `json:"max_age_seconds"`   // 前端缓存信息最大时间
	AllowOrigins     []string `json:"allow_origins"`     // 允许的源
	AllowMethods     []string `json:"allow_methods"`     // 允许的请求方法
	AllowHeaders     []string `json:"allow_headers"`     // 允许的请求头字段
	ExposeHeaders    []string `json:"expose_headers"`    // 响应的额外响应头字段
}

// GetConfig 获取配置信息
func (own *CORS) GetConfig() Config {
	config := Config{
		IsEnabled:    own.IsEnabled(),
		AllowOrigins: make([]string, 0),
		AllowMethods: make([]string, 0),
		AllowHeaders: make([]string, 0),
	}

	own.lock.RLock()
	config.AllowCredentials = own.allowCredentials
	config.MaxAgeSeconds = own.maxAge

	for origin := range own.allowOrigins {
		config.AllowOrigins = append(config.AllowOrigins, origin)
	}

	for method := range own.allowMethods {
		config.AllowMethods = append(config.AllowMethods, method)
	}

	for header := range own.allowHeaders {
		config.AllowHeaders = append(config.AllowHeaders, header)
	}

	for header := range own.exposeHeaders {
		config.ExposeHeaders = append(config.ExposeHeaders, header)
	}
	own.lock.RUnlock()

	return config
}

// Enable 启用
func (own *CORS) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用
func (own *CORS) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)
}

// IsEnabled 检查是否启用
func (own *CORS) IsEnabled() bool {
	return atomic.LoadInt32(&own.isEnabled) == 1
}

// SetMaxAge 设置前端缓存信息的最大时间（单位：秒）
func (own *CORS) SetMaxAge(seconds int64) {
	if seconds < 0 {
		seconds = 86400
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	own.maxAge = seconds
	own.maxAgeString = strconv.Itoa(int(own.maxAge))
}

// SetAllowOrigins （重新）设置可跨域访问的源。
// 若未传任何参数，则允许所有源（Access-Control-Allow-Origin: *，且不能使用凭证）。
// 若传入具体源（如 https://example.com），则仅允许这些源，并默认开启凭证。
// 若参数中包含通配符 "*"，则视为允许所有源（与凭证互斥，忽略其余具体源）。
func (own *CORS) SetAllowOrigins(origins ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 若源为空，则允许所有源
	if len(origins) == 0 {
		own.allowAllOrigins = true   // 允许所有源
		own.allowCredentials = false // 通配符源不能使用凭证
		own.allowOrigins = nil       // 重置源
		return
	}

	allowSet := make(map[string]struct{})
	for _, origin := range origins {
		if origin == "" {
			continue
		}

		// 包含通配符时，直接允许所有源，并忽略其余具体源
		if origin == "*" {
			own.allowAllOrigins = true
			own.allowCredentials = false
			own.allowOrigins = nil
			return
		}

		allowSet[origin] = struct{}{}
	}

	if len(allowSet) == 0 {
		// 仅传入了空白字符串等无效源，回退为允许所有源
		own.allowAllOrigins = true
		own.allowCredentials = false
		own.allowOrigins = nil
		return
	}

	// 传入了具体源，则仅允许这些源，并默认开启凭证
	own.allowAllOrigins = false
	own.allowCredentials = true
	own.allowOrigins = allowSet
}

// SetAllowMethods （重新）设置可跨域访问的请求方法
func (own *CORS) SetAllowMethods(methods ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	own.allowMethods = make(map[string]struct{})
	for _, method := range methods {
		if method != "" {
			own.allowMethods[method] = struct{}{}
		}
	}

	if len(own.allowMethods) > 0 {
		allowMethods := make([]string, 0, len(methods))
		for method := range own.allowMethods {
			allowMethods = append(allowMethods, method)
		}

		own.allowMethodString = strings.Join(allowMethods, ", ")
	}

}

// SetAllowHeaders （重新）设置可跨域访问的请求头字段
func (own *CORS) SetAllowHeaders(headers ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	own.allowHeaders = make(map[string]struct{})
	for _, header := range headers {
		if header != "" {
			own.allowHeaders[header] = struct{}{}
		}
	}

	if len(own.allowHeaders) > 0 {
		allowHeaders := make([]string, 0, len(own.allowHeaders))
		for header := range own.allowHeaders {
			allowHeaders = append(allowHeaders, header)
		}

		own.allowHeaderString = strings.Join(allowHeaders, ", ")
	}
}

// SetExposeHeaders （重新）设置响应跨域请求的额外响应头字段
func (own *CORS) SetExposeHeaders(headers ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	own.exposeHeaders = make(map[string]struct{})
	for _, header := range headers {
		if header != "" {
			own.exposeHeaders[header] = struct{}{}
		}
	}

	if len(own.exposeHeaders) > 0 {
		exposeHeaders := make([]string, 0, len(own.exposeHeaders))
		for header := range own.exposeHeaders {
			exposeHeaders = append(exposeHeaders, header)
		}

		own.exposeHeaderString = strings.Join(exposeHeaders, ", ")
	}
}
