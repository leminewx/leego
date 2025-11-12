package cors

import (
	"strconv"
	"strings"

	"gitee.com/leminewx/gokit/ggid"
)

type Config struct {
	// 元数据
	Id    string `json:"id" xml:"id"`
	Type  string `json:"type" xml:"type"`
	Scope string `json:"scope" xml:"scope"`

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
		MaxAge:        _DEFAULT_MAX_AGE,
		AllowOrigins:  make([]string, 0),
		AllowMethods:  make([]string, 0),
		AllowHeaders:  make([]string, 0),
		ExposeHeaders: make([]string, 0),
	}
}

// SetScope 设置作用域
func (own *CORS) SetScope(scope string) {
	own.lock.Lock()
	own.config.Scope = scope
	own.lock.Unlock()
}

// WithMaxAge 设置最大可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 信息的时间
func (own *CORS) WithMaxAge(seconds int64) *CORS {
	if seconds < 0 {
		seconds = 0
	}

	own.lock.Lock()
	own.config.MaxAge = seconds
	own.maxAgeString = strconv.Itoa(int(own.config.MaxAge))
	own.lock.Unlock()

	return own
}

// WithAllowOrigins 指定可跨域访问的源，若不指定，则允许所有源访问
func (own *CORS) WithAllowOrigins(origins ...string) *CORS {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 如果不指定源，则允许所有源，重置配置
	if len(origins) == 0 {
		own.allowAllOrigins = true   // 允许所有源
		own.allowCredentials = false // 通配符源不能使用凭证
		own.config.AllowOrigins = make([]string, 0)
		own.allowOrigins = make(map[string]struct{})
		return own
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
			return own
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

	return own
}

// WithAllowMethods 指定可跨域访问的请求方法
func (own *CORS) WithAllowMethods(methods ...string) *CORS {
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

	return own
}

// WithAllowHeaders 指定可跨域访问的请求头字段
func (own *CORS) WithAllowHeaders(headers ...string) *CORS {
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

	return own
}

// WithExposeHeaders 指定响应跨域请求的额外响应头字段
func (own *CORS) WithExposeHeaders(headers ...string) *CORS {
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

	return own
}

// Sync 同步配置
func (own *CORS) Sync(config *Config) {
	// 重置配置
	own.lock.Lock()
	own.resetConfig()
	own.lock.Unlock()

	// 同步配置参数
	own.WithMaxAge(config.MaxAge).
		WithAllowOrigins(config.AllowOrigins...).
		WithAllowHeaders(config.AllowHeaders...).
		WithAllowMethods(config.AllowMethods...).
		WithExposeHeaders(config.ExposeHeaders...)
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
func (own *CORS) GetConfig() *Config {
	config := &Config{
		Id:            own.config.Id,
		Type:          "cors",
		Scope:         own.config.Scope,
		AllowOrigins:  make([]string, 0),
		AllowMethods:  make([]string, 0),
		AllowHeaders:  make([]string, 0),
		ExposeHeaders: make([]string, 0),
	}

	own.lock.RLock()
	config.MaxAge = own.config.MaxAge

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
