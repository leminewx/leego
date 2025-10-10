package cors

import (
	"net/http"
	"strconv"
	"strings"

	"gitee.com/leminewx/leego/headers"
)

const (
	_DEFAULT_MAX_AGE       = 86400                             // 默认预检请求缓存时间（24小时）
	_DEFAULT_ALLOW_HEADERS = "Content-Type, Authorization"     // 默认允许的 HTTP 头字段
	_DEFAULT_ALLOW_METHODS = "GET, PUT, POST, DELETE, OPTIONS" // 默认允许的 HTTP 方法
)

// CORS 定义跨域处理器的数据结构
type CORS struct {
	// 表示可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 提供的信息多长时间，单位秒，一般为24小时（86400）
	maxAge       int
	maxAgeString string

	// 是否需要携带 Origin，设置为 true 时，Access-Control-Allow-Origin 不能设置为 *
	allowCredentials bool

	// 允许跨域的域
	allowAllOrigins bool                // 是否允许所有源
	allowOriginList []string            // 缓存允许源列表，用于快速检查
	allowOrigins    map[string]struct{} // 具体的允许源列表

	// 允许访问的方法
	allowMethods      map[string]struct{}
	allowMethodString string

	// 允许携带的字段
	allowHeaders      map[string]struct{}
	allowHeaderString string

	/*
		允许暴露的非简单响应头，在使用CORS方式跨域时，浏览器只会返回默认的头部 Header，认情况下可用的响应头包括：
		  - Cache-Control
		  - Content-Language
		  - Content-Type
		  - Expires
		  - Last-Modified
		  - Pragma
	*/
	exposeHeaders      map[string]struct{}
	exposeHeaderString string
}

// New 新建一个跨域处理器
func New() *CORS {
	c := &CORS{
		maxAge:          _DEFAULT_MAX_AGE, // 24小时
		maxAgeString:    strconv.Itoa(_DEFAULT_MAX_AGE),
		allowOriginList: make([]string, 0),
		allowOrigins:    make(map[string]struct{}),
		allowMethods:    make(map[string]struct{}),
		allowHeaders:    make(map[string]struct{}),
		exposeHeaders:   make(map[string]struct{}),
	}

	// 设置默认允许的方法
	return c.WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS")
}

// NewDefault 创建默认配置的CORS中间件（允许所有源，常用方法）
func NewDefault() *CORS {
	return New().
		WithAllowOrigins().
		WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH").
		WithAllowHeaders("Content-Type", "Authorization", "X-Requested-With").
		WithExposeHeaders("Content-Length", "Content-Type")
}

// WithMaxAge 设置最大可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 信息的时间
func (own *CORS) WithMaxAge(age int) *CORS {
	if age < 0 {
		age = 0
	}

	own.maxAge = age
	own.maxAgeString = strconv.Itoa(own.maxAge)
	return own
}

// WithAllowOrigins 指定可跨域访问的源，若不指定，则允许所有源访问
func (own *CORS) WithAllowOrigins(origins ...string) *CORS {
	if len(origins) == 0 {
		own.allowAllOrigins = true   // 允许所有源
		own.allowCredentials = false // 通配符源不能使用凭证
		own.allowOrigins = make(map[string]struct{})
		own.allowOriginList = make([]string, 0)
		return own
	}

	for _, origin := range origins {
		if origin == "" {
			continue
		}

		if origin == "*" {
			own.allowAllOrigins = true
			own.allowCredentials = false
			own.allowOrigins = make(map[string]struct{})
			own.allowOriginList = make([]string, 0)
			return own
		}

		if _, ok := own.allowOrigins[origin]; !ok {
			own.allowOrigins[origin] = struct{}{}
			own.allowOriginList = append(own.allowOriginList, origin)

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
	for _, method := range methods {
		if method != "" {
			own.allowMethods[method] = struct{}{}
		}
	}

	if len(own.allowMethods) > 0 {
		own.allowMethodString = strings.Join(getMapKeys(own.allowMethods), ", ")
	}

	return own
}

// WithAllowHeaders 指定可跨域访问的请求头字段
func (own *CORS) WithAllowHeaders(headers ...string) *CORS {
	for _, header := range headers {
		if header != "" {
			own.allowHeaders[header] = struct{}{}
		}
	}

	if len(own.allowHeaders) > 0 {
		own.allowHeaderString = strings.Join(getMapKeys(own.allowHeaders), ", ")
	}

	return own
}

// WithExposeHeaders 指定响应跨域请求的额外响应头字段
func (own *CORS) WithExposeHeaders(headers ...string) *CORS {
	for _, header := range headers {
		if header != "" {
			own.exposeHeaders[header] = struct{}{}
		}
	}

	if len(own.exposeHeaders) > 0 {
		own.exposeHeaderString = strings.Join(getMapKeys(own.exposeHeaders), ", ")
	}

	return own
}

// getMapKeys 获取 map 的 key
func getMapKeys(data map[string]struct{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}

	return keys
}

// HandlePreflight 处理预检请求
func (own *CORS) HandlePreflight(resp http.ResponseWriter, req *http.Request) bool {
	// 检查请求方法
	if req.Method != http.MethodOptions {
		return false
	}

	// 检查是否有预检请求相关的头
	if method := req.Header.Get(headers.ACCESS_CONTROL_REQUEST_METHOD); method == "" {
		return false
	}

	// 设置预检响应头
	own.SetResponseHeader(resp, req)

	// 设置状态码
	resp.WriteHeader(http.StatusNoContent)
	return true
}

// SetResponseHeader 设置响应头
func (own *CORS) SetResponseHeader(resp http.ResponseWriter, req *http.Request) {
	header := resp.Header()

	// 设置 Access-Control-Allow-Origin
	allowedOrigin := own.getAllowedOrigin(req)
	if allowedOrigin != "" {
		header.Set(headers.ACCESS_CONTROL_ALLOW_ORIGIN, allowedOrigin)
	}

	// 设置 Access-Control-Allow-Credentials
	if own.allowCredentials {
		header.Set(headers.ACCESS_CONTROL_ALLOW_CREDENTIALS, "true")
	} else {
		// 删除可能存在的错误设置
		header.Del(headers.ACCESS_CONTROL_ALLOW_CREDENTIALS)
	}

	// 设置 Access-Control-Allow-Methods
	if own.allowMethodString != "" {
		header.Set(headers.ACCESS_CONTROL_ALLOW_METHODS, own.allowMethodString)
	} else {
		header.Set(headers.ACCESS_CONTROL_ALLOW_METHODS, _DEFAULT_ALLOW_METHODS)
	}

	// 设置 Access-Control-Allow-Headers
	if own.allowHeaderString != "" {
		header.Set(headers.ACCESS_CONTROL_ALLOW_HEADERS, own.allowHeaderString)
	} else {
		header.Set(headers.ACCESS_CONTROL_ALLOW_HEADERS, _DEFAULT_ALLOW_HEADERS)
	}

	// 设置 Access-Control-Max-Age
	if own.maxAge > 0 {
		header.Set(headers.ACCESS_CONTROL_MAX_AGE, own.maxAgeString)
	}

	// 设置 Access-Control-Expose-Headers
	if own.exposeHeaderString != "" {
		header.Set(headers.ACCESS_CONTROL_EXPOSE_HEADERS, own.exposeHeaderString)
	}
}

// getAllowedOrigin 根据请求获取允许的源
func (own *CORS) getAllowedOrigin(req *http.Request) string {
	// 如果允许所有源，则返回 "*"
	if own.allowAllOrigins {
		return "*"
	}

	// 获取请求的源
	origin := req.Header.Get("Origin")
	if origin == "" {
		return ""
	}

	// 检查请求的源是否在允许列表中
	if _, ok := own.allowOrigins[origin]; ok {
		return origin
	}

	// 如果没有匹配的源，返回空字符串
	return ""
}
