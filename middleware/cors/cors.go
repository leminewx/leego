package cors

import (
	"net/http"
	"strconv"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
)

const (
	_DEFAULT_MAX_AGE       = 86400                                    // 默认预检请求缓存时间（24小时）
	_DEFAULT_ALLOW_METHODS = "GET, PUT, POST, DELETE, OPTIONS, PATCH" // 默认允许的 HTTP 方法
)

// CORS 定义跨域处理器的数据结构
type CORS struct {
	*Config

	// 表示可以缓存 Access-Control-Allow-Methods 和 Access-Control-Allow-Headers 提供的信息多长时间，单位秒，一般为24小时（86400）
	maxAgeString string

	// 允许访问的源
	allowCredentials bool                // 是否需要指定源，设置为 true 时，Access-Control-Allow-Origin 不能设置为 *
	allowAllOrigins  bool                // 是否允许所有源
	allowOrigins     map[string]struct{} // 具体的允许源列表

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

// NewDefaultCORS 创建一个带默认配置的 CORS 中间件（允许所有源、所有请求头和常用方法）
func NewDefaultCORS(name string) *CORS {
	return NewCORS(NewDefaultConfig(name)).
		WithAllowOrigins().
		WithAllowMethods("GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH")
}

// NewCORS 新建一个 CORS 中间件
func NewCORS(config *Config) *CORS {
	return &CORS{
		Config:        config,
		maxAgeString:  strconv.Itoa(int(config.MaxAge)),
		allowOrigins:  make(map[string]struct{}),
		allowMethods:  make(map[string]struct{}),
		allowHeaders:  make(map[string]struct{}),
		exposeHeaders: make(map[string]struct{}),
	}
}

// Handle 创建一个处理函数
func (own *CORS) Handle(handle leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		// 处理预检请求
		if own.handlePreflight(ctx) {
			return
		}

		// 处理普通请求
		own.writeResponseHeader(ctx)

		// 调用下一个处理器
		handle(ctx)
	}
}

// handlePreflight 处理预检请求
func (own *CORS) handlePreflight(ctx *leego.Context) bool {
	// 检查请求方法
	if ctx.GetMethod() != http.MethodOptions {
		return false
	}

	// 检查是否有预检请求相关的头
	if method := ctx.GetRequestHeader(headers.ACCESS_CONTROL_REQUEST_METHOD); method == "" {
		return false
	}

	// 设置预检响应头
	own.writeResponseHeader(ctx)

	// 设置状态码
	ctx.WithStatusCode(http.StatusNoContent)
	return true
}

// writeResponseHeader 写入响应头
func (own *CORS) writeResponseHeader(ctx *leego.Context) {
	own.lock.RLock()
	defer own.lock.RUnlock()

	header := ctx.Response.Header()

	// 设置 Access-Control-Allow-Origin
	allowedOrigin := own.getAllowedOrigin(ctx.Request)
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
		header.Set(headers.ACCESS_CONTROL_ALLOW_HEADERS, "*")
	}

	// 设置 Access-Control-Max-Age
	if own.MaxAge > 0 {
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
