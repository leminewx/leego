package cors

import "strings"

type CORS struct {
	// 表示可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 提供的信息多长时间，单位秒，一般为10分钟
	MaxAge int64

	// 是否需要携带 Origin
	// 设置为 true 时，Access-Control-Allow-Origin 不能设置为 *
	AllowCredentials bool

	// 允许跨域的域
	allowOrigins map[string]struct{}

	// 允许访问的方法
	allowMethods map[string]struct{}

	// 允许携带的字段
	allowHeaders map[string]struct{}

	// 允许暴露的非简单响应头
	// 在使用CORS方式跨域时，浏览器只会返回 默认的头部 Header，认情况下可用的响应头包括：
	// Cache-Control
	// Content-Language
	// Content-Type
	// Expires
	// Last-Modified
	// Pragma
	exposeHeaders map[string]struct{}
}

func NewCORS() *CORS {
	return &CORS{
		MaxAge:        600, // 默认10分钟
		allowOrigins:  make(map[string]struct{}),
		allowMethods:  make(map[string]struct{}),
		allowHeaders:  make(map[string]struct{}),
		exposeHeaders: make(map[string]struct{}),
	}
}

func (own *CORS) WithMaxAge(age int64) *CORS {
	own.MaxAge = age
	return own
}

func (own *CORS) WithAllowCredentials() *CORS {
	own.AllowCredentials = true
	return own
}

func (own *CORS) WithAllowOrigins(origins ...string) *CORS {
	for _, origin := range origins {
		own.allowOrigins[origin] = struct{}{}
	}

	return own
}

func (own *CORS) GetAllowOrigins(origin string) string {
	if own.AllowCredentials {
		if _, ok := own.allowOrigins[origin]; ok {
			return origin
		}

		return ""
	}

	if _, ok := own.allowOrigins[origin]; ok {
		return origin
	}

	return "*"
}

func (own *CORS) WithAllowMethods(methods ...string) *CORS {
	for _, method := range methods {
		own.allowMethods[method] = struct{}{}
	}

	return own
}

func (own *CORS) GetAllowMethods() string {
	return strings.Join(getMapKeys(own.allowMethods), ", ")
}

func (own *CORS) WithAllowHeaders(headers ...string) *CORS {
	for _, header := range headers {
		own.allowHeaders[header] = struct{}{}
	}

	return own
}

func (own *CORS) GetAllowHeaders() string {
	return strings.Join(getMapKeys(own.allowHeaders), ", ")
}

func (own *CORS) WithExposeHeaders(headers ...string) *CORS {
	for _, header := range headers {
		own.exposeHeaders[header] = struct{}{}
	}

	return own
}

func (own *CORS) GetExposeHeaders() string {
	return strings.Join(getMapKeys(own.exposeHeaders), ", ")
}

func getMapKeys(data map[string]struct{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}

	return keys
}
