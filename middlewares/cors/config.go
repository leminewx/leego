package cors

import (
	"sort"
	"strconv"
	"strings"
)

// Config 定义跨域配置，用于导出当前设置或从配置中心批量装配。
type Config struct {
	// IsEnabled 表示是否启用跨域处理。
	IsEnabled bool `json:"is_enabled"`
	// AllowCredentials 表示是否允许浏览器携带凭证。
	AllowCredentials bool `json:"allow_credentials"`
	// MaxAgeSeconds 为预检结果的最大缓存秒数。
	MaxAgeSeconds int64 `json:"max_age_seconds"`
	// AllowOrigins 为允许的来源列表，允许任意来源时为 ["*"]。
	AllowOrigins []string `json:"allow_origins"`
	// AllowMethods 为允许的请求方法列表（保持配置顺序）。
	AllowMethods []string `json:"allow_methods"`
	// AllowHeaders 为允许的请求头列表（保持配置顺序）。
	AllowHeaders []string `json:"allow_headers"`
	// ExposeHeaders 为允许暴露给脚本的响应头列表（保持配置顺序）。
	ExposeHeaders []string `json:"expose_headers"`
}

// GetConfig 返回当前配置的副本。
//
// 返回值是深拷贝：调用方修改切片不会影响中间件内部状态。
// 由于配置项分属不同同步域，各字段不保证是严格同一瞬间的快照。
func (own *CORS) GetConfig() Config {
	config := Config{
		IsEnabled: own.IsEnabled(),
	}

	own.lock.RLock()
	config.AllowCredentials = own.allowCredentials
	config.MaxAgeSeconds = own.maxAge

	config.AllowMethods = append([]string(nil), own.allowMethodList...)
	config.AllowHeaders = append([]string(nil), own.allowHeaderList...)
	config.ExposeHeaders = append([]string(nil), own.exposeHeaderList...)

	if own.allowAllOrigins {
		config.AllowOrigins = []string{"*"}
	} else {
		config.AllowOrigins = make([]string, 0, len(own.allowOrigins))
		for origin := range own.allowOrigins {
			config.AllowOrigins = append(config.AllowOrigins, origin)
		}
		sort.Strings(config.AllowOrigins)
	}
	own.lock.RUnlock()

	return config
}

// SetMaxAge 设置预检结果的最大缓存秒数。
//
// 传负数表示恢复默认值 86400 秒（24 小时）；传 0 表示不下发 Max-Age 头。
func (own *CORS) SetMaxAge(seconds int64) {
	if seconds < 0 {
		seconds = 86400
	}

	own.lock.Lock()
	own.maxAge = seconds
	own.maxAgeString = strconv.FormatInt(seconds, 10)
	own.lock.Unlock()
}

/*
SetAllowOrigins 重新设置来源白名单。

规则：
  - 不传参数、或参数中包含 "*"、或全部为空字符串时，视为允许任意来源，
    同时强制关闭凭证（浏览器不允许 “*” 与凭证同时使用）；
  - 传入具体源时仅允许这些源，且不会自动开启凭证，
    需要携带凭证请显式调用 SetAllowCredentials(true)。

与旧实现相比，这里取消了“传入具体源即自动开启凭证”的隐式行为：
凭证属于安全敏感项，应当由调用方明确开启。
*/
func (own *CORS) SetAllowOrigins(origins ...string) {
	allowSet := make(map[string]struct{}, len(origins))
	allowAll := len(origins) == 0

	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}

		if origin == "*" {
			allowAll = true
			break
		}

		allowSet[origin] = struct{}{}
	}

	if len(allowSet) == 0 {
		allowAll = true
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	if allowAll {
		own.allowAllOrigins = true
		own.allowCredentials = false
		own.allowOrigins = nil
		return
	}

	own.allowAllOrigins = false
	own.allowOrigins = allowSet
}

// SetAllowCredentials 设置是否允许浏览器携带凭证。
//
// 若同时允许任意来源，则凭证设置会被忽略（响应中不会下发 Allow-Credentials），
// 这是浏览器强制的安全约束，而非本实现的选择。
func (own *CORS) SetAllowCredentials(allow bool) {
	own.lock.Lock()
	own.allowCredentials = allow && !own.allowAllOrigins
	own.lock.Unlock()
}

// SetAllowMethods 重新设置允许的请求方法。
//
// 方法名会被统一转为大写并去重，同时保持调用方传入的顺序，
// 使 Access-Control-Allow-Methods 的输出稳定、可测试。
func (own *CORS) SetAllowMethods(methods ...string) {
	allowMap := make(map[string]struct{}, len(methods))
	allowList := make([]string, 0, len(methods))

	for _, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			continue
		}

		if _, isExisted := allowMap[method]; isExisted {
			continue
		}

		allowMap[method] = struct{}{}
		allowList = append(allowList, method)
	}

	own.lock.Lock()
	own.allowMethods = allowMap
	own.allowMethodList = allowList
	own.allowMethodString = strings.Join(allowList, ", ")
	own.lock.Unlock()
}

// SetAllowHeaders 重新设置允许的请求头，保持传入顺序并去重（大小写不敏感）。
func (own *CORS) SetAllowHeaders(headerNames ...string) {
	allowMap := make(map[string]struct{}, len(headerNames))
	allowList := make([]string, 0, len(headerNames))

	for _, headerName := range headerNames {
		headerName = strings.TrimSpace(headerName)
		if headerName == "" {
			continue
		}

		// HTTP 头名称大小写不敏感，统一按小写去重，但保留调用方写法用于输出。
		key := strings.ToLower(headerName)
		if _, isExisted := allowMap[key]; isExisted {
			continue
		}

		allowMap[key] = struct{}{}
		allowList = append(allowList, headerName)
	}

	own.lock.Lock()
	own.allowHeaders = allowMap
	own.allowHeaderList = allowList
	own.allowHeaderString = strings.Join(allowList, ", ")
	own.lock.Unlock()
}

// SetExposeHeaders 重新设置允许暴露给脚本的响应头，保持传入顺序并去重。
func (own *CORS) SetExposeHeaders(headerNames ...string) {
	exposeMap := make(map[string]struct{}, len(headerNames))
	exposeList := make([]string, 0, len(headerNames))

	for _, headerName := range headerNames {
		headerName = strings.TrimSpace(headerName)
		if headerName == "" {
			continue
		}

		key := strings.ToLower(headerName)
		if _, isExisted := exposeMap[key]; isExisted {
			continue
		}

		exposeMap[key] = struct{}{}
		exposeList = append(exposeList, headerName)
	}

	own.lock.Lock()
	own.exposeHeaders = exposeMap
	own.exposeHeaderList = exposeList
	own.exposeHeaderString = strings.Join(exposeList, ", ")
	own.lock.Unlock()
}

// GetAllowedOrigins 返回来源白名单副本；允许任意来源时返回 ["*"]。
func (own *CORS) GetAllowedOrigins() []string {
	return own.GetConfig().AllowOrigins
}

// GetAllowedMethods 返回允许的方法列表副本。
func (own *CORS) GetAllowedMethods() []string {
	own.lock.RLock()
	methods := append([]string(nil), own.allowMethodList...)
	own.lock.RUnlock()

	return methods
}

// GetAllowedHeaders 返回允许的请求头列表副本。
func (own *CORS) GetAllowedHeaders() []string {
	own.lock.RLock()
	headerNames := append([]string(nil), own.allowHeaderList...)
	own.lock.RUnlock()

	return headerNames
}

// GetExposedHeaders 返回允许暴露的响应头列表副本。
func (own *CORS) GetExposedHeaders() []string {
	own.lock.RLock()
	headerNames := append([]string(nil), own.exposeHeaderList...)
	own.lock.RUnlock()

	return headerNames
}
