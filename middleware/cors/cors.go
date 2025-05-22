package cors

import (
	"net/http"
	"strconv"
	"strings"
)

type CORS struct {
	// 表示可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 提供的信息多长时间，单位秒，一般为24小时（86400）
	maxAge       int64
	maxAgeString string

	// 是否需要携带 Origin，设置为 true 时，Access-Control-Allow-Origin 不能设置为 *
	allowCredentials bool

	// 允许跨域的域
	allowOrigin string

	// 允许访问的方法
	allowMethods      map[string]struct{}
	allowMethodString string

	// 允许携带的字段
	allowHeaders      map[string]struct{}
	allowHeaderString string

	// 允许暴露的非简单响应头
	//
	// 在使用CORS方式跨域时，浏览器只会返回默认的头部 Header，认情况下可用的响应头包括：
	//
	// Cache-Control
	//
	// Content-Language
	//
	// Content-Type
	//
	// Expires
	//
	// Last-Modified
	//
	// Pragma
	exposeHeaders      map[string]struct{}
	exposeHeaderString string
}

// New 新建一个跨域处理器
func New() *CORS {
	return &CORS{
		maxAge:        86400, // 24小时
		maxAgeString:  "86400",
		allowOrigin:   "*",
		allowMethods:  make(map[string]struct{}),
		allowHeaders:  make(map[string]struct{}),
		exposeHeaders: make(map[string]struct{}),
	}
}

// WithMaxAge 设置最大可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 信息的时间
func (own *CORS) WithMaxAge(age int64) *CORS {
	own.maxAge = age
	own.maxAgeString = strconv.Itoa(int(own.maxAge))
	return own
}

// WithAllowOrigin 指定可跨域访问的 Origin
func (own *CORS) WithAllowOrigin(origin string) *CORS {
	if origin != "" {
		own.allowCredentials = true
		own.allowOrigin = origin
	}

	return own
}

// WithAllowMethods 指定可跨域访问的请求方法
func (own *CORS) WithAllowMethods(methods ...string) *CORS {
	for _, method := range methods {
		own.allowMethods[method] = struct{}{}
	}

	own.allowMethodString = strings.Join(getMapKeys(own.allowMethods), ", ")
	return own
}

// WithAllowHeaders 指定可跨域访问的请求头字段
func (own *CORS) WithAllowHeaders(headers ...string) *CORS {
	for _, header := range headers {
		own.allowHeaders[header] = struct{}{}
	}

	own.allowHeaderString = strings.Join(getMapKeys(own.allowHeaders), ", ")
	return own
}

// WithExposeHeaders 指定响应跨域请求的额外响应头字段
func (own *CORS) WithExposeHeaders(headers ...string) *CORS {
	for _, header := range headers {
		own.exposeHeaders[header] = struct{}{}
	}

	own.exposeHeaderString = strings.Join(getMapKeys(own.allowHeaders), ", ")
	return own
}

func getMapKeys(data map[string]struct{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}

	return keys
}

const (
	HEADER_ACCESS_CONTROL_REQUEST_METHOD  = "Access-Control-Request-Method"  // 实际请求方法
	HEADER_ACCESS_CONTROL_REQUEST_HEADERS = "Access-Control-Request-Headers" // 实际请求携带的自定义字段

	HEADER_ACCESS_CONTROL_MAX_AGE           = "Access-Control-Max-Age"
	HEADER_ACCESS_CONTROL_ALLOW_CREDENTIALS = "Access-Control-Allow-Credentials"
	HEADER_ACCESS_CONTROL_ALLOW_ORIGIN      = "Access-Control-Allow-Origin"
	HEADER_ACCESS_CONTROL_ALLOW_METHODS     = "Access-Control-Allow-Methods"
	HEADER_ACCESS_CONTROL_ALLOW_HEADERS     = "Access-Control-Allow-Headers"
	HEADER_ACCESS_CONTROL_EXPOSE_HEADERS    = "Access-Control-Expose-Headers"

	_DEFAULT_ALLOW_METHODS = "GET, PUT, POST, DELETE, OPTIONS"
)

// SetResponseHeader 设置响应头
func (own *CORS) SetResponseHeader(resp http.ResponseWriter) {
	header := resp.Header()

	// 设置 Access-Control-Allow-Origin
	header.Set(HEADER_ACCESS_CONTROL_ALLOW_ORIGIN, own.allowOrigin)

	// 设置 Access-Control-Allow-Credentials
	if own.allowCredentials {
		header.Set(HEADER_ACCESS_CONTROL_ALLOW_CREDENTIALS, "true")
	} else {
		header.Set(HEADER_ACCESS_CONTROL_ALLOW_CREDENTIALS, "false")
	}

	// Access-Control-Allow-Methods
	if len(own.allowMethods) > 0 {
		header.Set(HEADER_ACCESS_CONTROL_ALLOW_METHODS, own.allowMethodString)
	} else {
		header.Set(HEADER_ACCESS_CONTROL_ALLOW_METHODS, _DEFAULT_ALLOW_METHODS)
	}

	// 设置 Access-Control-Allow-Headers
	if len(own.allowHeaders) > 0 {
		header.Set(HEADER_ACCESS_CONTROL_ALLOW_HEADERS, own.allowHeaderString)
	} else {
		header.Set(HEADER_ACCESS_CONTROL_ALLOW_HEADERS, "*")
	}

	// 设置 Access-Control-Max-Age
	if own.maxAge > 0 {
		header.Set(HEADER_ACCESS_CONTROL_MAX_AGE, own.maxAgeString)
	}

	// 设置 Access-Control-Expose-Headers
	if len(own.exposeHeaders) > 0 {
		header.Set(HEADER_ACCESS_CONTROL_EXPOSE_HEADERS, own.exposeHeaderString)
	}

	// if own.allowCredentials {
	// 	origin := req.Header.Get(HEADER_ACCESS_CONTROL_ALLOW_ORIGIN)
	// 	if origin == "*" || origin != own.allowOrigin {
	// 		http.Error(resp, status.STATUS_FORBIDDEN, http.StatusForbidden)
	// 		return false
	// 	}

	// 	requestMethod := req.Header.Get(HEADER_ACCESS_CONTROL_REQUEST_METHOD)
	// 	if requestMethod != "" {
	// 		if _, ok := own.allowMethods[requestMethod]; !ok {
	// 			http.Error(resp, status.STATUS_FORBIDDEN, http.StatusForbidden)
	// 			return false
	// 		}
	// 	}
	// }

}
