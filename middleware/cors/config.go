package cors

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// Enable 启用
func (own *CORS) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用
func (own *CORS) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)

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

// SetAllowOrigins （重新）设置可跨域访问的源，若不为空，则允许所有源访问
func (own *CORS) SetAllowOrigins(origins ...string) {
	own.lock.Lock()
	defer own.lock.Unlock()

	// 若源为空，则允许所有源
	if len(origins) == 0 {
		own.allowAllOrigins = true                   // 允许所有源
		own.allowCredentials = false                 // 通配符源不能使用凭证
		own.allowOrigins = make(map[string]struct{}) // 重置源
		return
	}

	// 重置源，并添加新源
	own.allowOrigins = make(map[string]struct{})
	for _, origin := range origins {
		if origin == "" {
			continue
		}

		// 重置源，且允许所有源
		if origin == "*" {
			own.allowAllOrigins = true
			own.allowCredentials = false
			own.allowOrigins = make(map[string]struct{})
		}

		if _, ok := own.allowOrigins[origin]; !ok {
			own.allowOrigins[origin] = struct{}{}
		}
	}

	// 如果有具体源，默认需要凭证
	if len(own.allowOrigins) > 0 {
		own.allowAllOrigins = false
		own.allowCredentials = true
	}
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
