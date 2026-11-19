// Package cors 提供符合 Fetch 规范的跨域资源共享（CORS）中间件。
//
// 设计要点：
//   - 预检请求（OPTIONS + Access-Control-Request-Method）必须逐项校验
//     请求来源与方法是否在白名单内，任一项不通过即拒绝，不做“无脑 204”；
//   - Vary 头采用“追加去重”而非覆盖，避免覆盖业务或其它中间件已写入的
//     缓存维度，导致 CDN / 反向代理缓存出错误的跨域响应；
//   - 凭证（credentials）与通配符源（*）互斥：浏览器禁止二者同时使用，
//     因此只要允许所有源，就一定不会输出 Allow-Credentials。
package cors

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
)

// defaultAllowMethods 为默认允许的跨域方法。
//
// 覆盖常用方法（含 HEAD 与 PATCH）：HEAD 常用于探活与缓存校验，
// PATCH 则是 REST 局部更新的标准方法，缺一都会在真实业务中造成跨域失败。
var defaultAllowMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

// CORS 定义跨域中间件。
//
// 所有可变配置都由 lock 保护，可以在运行期安全地热更新；
// 启用状态使用 atomic，读取时无需加锁，避免热路径上的锁竞争。
type CORS struct {
	// isEnabled 表示是否启用（atomic）。
	isEnabled atomic.Bool

	// lock 保护以下全部字段。
	lock sync.RWMutex
	// maxAge 为预检结果的最大缓存秒数，小于等于 0 表示不下发该头。
	maxAge int64
	// maxAgeString 为 maxAge 的字符串缓存，避免每次请求都做整数格式化。
	maxAgeString string

	// allowCredentials 表示是否允许浏览器携带凭证。
	allowCredentials bool
	// allowAllOrigins 表示是否允许任意来源。
	allowAllOrigins bool
	// allowOrigins 为来源白名单。
	allowOrigins map[string]struct{}

	// allowMethods 为方法白名单（查找用）。
	allowMethods map[string]struct{}
	// allowMethodList 为方法白名单（保持调用方传入顺序，用于生成响应头）。
	allowMethodList []string
	// allowMethodString 为方法响应头内容。
	allowMethodString string

	// allowHeaders 为请求头白名单（查找用）。
	allowHeaders map[string]struct{}
	// allowHeaderList 为请求头白名单（保持传入顺序）。
	allowHeaderList []string
	// allowHeaderString 为请求头响应头内容。
	allowHeaderString string

	// exposeHeaders 为允许暴露给脚本的响应头（查找用）。
	exposeHeaders map[string]struct{}
	// exposeHeaderList 为允许暴露的响应头（保持传入顺序）。
	exposeHeaderList []string
	// exposeHeaderString 为暴露响应头内容。
	exposeHeaderString string
}

/*
NewDefaultCORS 创建带默认配置的跨域中间件。

默认行为：
  - 允许任意来源（Access-Control-Allow-Origin: *）；
  - 不允许携带凭证（与通配符源互斥）；
  - 允许 GET / HEAD / POST / PUT / PATCH / DELETE / OPTIONS；
  - 预检结果的缓存时间为 24 小时。

创建后即处于启用状态，无需再调用 Enable。
*/
func NewDefaultCORS() *CORS {
	instance := &CORS{}
	instance.SetAllowOrigins()
	instance.SetAllowMethods(defaultAllowMethods...)
	instance.SetMaxAge(86400)
	instance.Enable()

	return instance
}

// Enable 启用跨域处理。
func (own *CORS) Enable() {
	own.isEnabled.Store(true)
}

// Disable 禁用跨域处理，中间件将直接放行且不写入任何 CORS 头。
func (own *CORS) Disable() {
	own.isEnabled.Store(false)
}

// IsEnabled 返回跨域处理是否启用。
func (own *CORS) IsEnabled() bool {
	return own.isEnabled.Load()
}

// GetMiddleware 返回跨域中间件。
//
// 处理顺序：
//  1. 未启用则直接放行；
//  2. 命中预检请求则完成校验并直接响应，不再进入业务链；
//  3. 普通请求写入必要的 CORS 响应头后放行。
func (own *CORS) GetMiddleware() leego.MiddlewareFunc {
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			if !own.IsEnabled() {
				next(ctx)
				return
			}

			if own.handlePreflight(ctx) {
				return
			}

			own.writeSimpleHeader(ctx)
			next(ctx)
		}
	}
}

// handlePreflight 处理预检请求，返回 true 表示已响应、不应再进入业务链。
func (own *CORS) handlePreflight(ctx *leego.Context) bool {
	if ctx.Request == nil || ctx.Request.Method != http.MethodOptions {
		return false
	}

	// 没有 Access-Control-Request-Method 说明是普通 OPTIONS，而非 CORS 预检。
	requestMethod := strings.ToUpper(strings.TrimSpace(ctx.GetRequestHeader(headers.ACCESS_CONTROL_REQUEST_METHOD)))
	if requestMethod == "" {
		return false
	}

	origin := own.getAllowedOrigin(ctx.Request)
	if origin == "" {
		// 来源不被允许：不输出任何 CORS 头，直接拒绝，避免浏览器误判为成功。
		ctx.ResponseError(http.StatusForbidden, http.StatusText(http.StatusForbidden))
		return true
	}

	if !own.isMethodAllowed(requestMethod) {
		ctx.ResponseError(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return true
	}

	own.writePreflightHeader(ctx, origin)
	ctx.SetStatusCode(http.StatusNoContent)

	return true
}

// isMethodAllowed 判断请求方法是否在白名单内。
func (own *CORS) isMethodAllowed(method string) bool {
	own.lock.RLock()
	_, isAllowed := own.allowMethods[method]
	own.lock.RUnlock()

	return isAllowed
}

// writeSimpleHeader 为普通（非预检）请求写入 CORS 响应头。
func (own *CORS) writeSimpleHeader(ctx *leego.Context) {
	own.lock.RLock()
	allowAllOrigins := own.allowAllOrigins
	allowCredentials := own.allowCredentials
	exposeHeaderString := own.exposeHeaderString
	own.lock.RUnlock()

	header := ctx.Response.Header()

	origin := own.getAllowedOrigin(ctx.Request)
	if origin != "" {
		header.Set(headers.ACCESS_CONTROL_ALLOW_ORIGIN, origin)

		// 响应随 Origin 变化，必须让缓存按 Origin 区分；通配符源则无需 Vary。
		if !allowAllOrigins {
			appendVary(header, "Origin")
		}
	}

	// 浏览器禁止 “通配符源 + 凭证” 组合，因此允许所有源时强制不下发该头。
	if allowCredentials && !allowAllOrigins {
		header.Set(headers.ACCESS_CONTROL_ALLOW_CREDENTIALS, "true")
	} else {
		header.Del(headers.ACCESS_CONTROL_ALLOW_CREDENTIALS)
	}

	if exposeHeaderString != "" {
		header.Set(headers.ACCESS_CONTROL_EXPOSE_HEADERS, exposeHeaderString)
	}
}

// writePreflightHeader 为预检请求写入完整响应头。
func (own *CORS) writePreflightHeader(ctx *leego.Context, origin string) {
	own.lock.RLock()
	allowAllOrigins := own.allowAllOrigins
	allowCredentials := own.allowCredentials
	allowMethodString := own.allowMethodString
	allowHeaderString := own.allowHeaderString
	maxAge := own.maxAge
	maxAgeString := own.maxAgeString
	own.lock.RUnlock()

	header := ctx.Response.Header()

	header.Set(headers.ACCESS_CONTROL_ALLOW_ORIGIN, origin)
	if !allowAllOrigins {
		appendVary(header, "Origin")
	}

	// 预检响应取决于这三个请求头，必须全部加入 Vary，否则缓存会串味。
	appendVary(header, "Access-Control-Request-Method", "Access-Control-Request-Headers")

	if allowCredentials && !allowAllOrigins {
		header.Set(headers.ACCESS_CONTROL_ALLOW_CREDENTIALS, "true")
	} else {
		header.Del(headers.ACCESS_CONTROL_ALLOW_CREDENTIALS)
	}

	if allowMethodString != "" {
		header.Set(headers.ACCESS_CONTROL_ALLOW_METHODS, allowMethodString)
	}

	if allowHeaderString != "" {
		header.Set(headers.ACCESS_CONTROL_ALLOW_HEADERS, allowHeaderString)
	} else if requestedHeaders := ctx.GetRequestHeader(headers.ACCESS_CONTROL_REQUEST_HEADERS); requestedHeaders != "" {
		// 未配置请求头白名单时回显浏览器实际请求的头，兼容自定义头场景。
		header.Set(headers.ACCESS_CONTROL_ALLOW_HEADERS, requestedHeaders)
	}

	if maxAge > 0 {
		header.Set(headers.ACCESS_CONTROL_MAX_AGE, maxAgeString)
	}
}

// getAllowedOrigin 依据请求的 Origin 返回应写入的 Allow-Origin 值。
//
// 返回空字符串表示来源不被允许。
// 注意：允许所有源时返回 "*"，此时调用方不得再下发 Allow-Credentials。
func (own *CORS) getAllowedOrigin(req *http.Request) string {
	if req == nil {
		return ""
	}

	own.lock.RLock()
	allowAllOrigins := own.allowAllOrigins
	_, isAllowed := own.allowOrigins[req.Header.Get("Origin")]
	own.lock.RUnlock()

	if allowAllOrigins {
		return "*"
	}

	if isAllowed {
		return req.Header.Get("Origin")
	}

	return ""
}

// appendVary 向 Vary 头追加取值，保持既有顺序并去重。
//
// 直接 Set 会覆盖其它组件写入的缓存维度（例如 Accept-Encoding），
// 造成缓存键缺失；这里采用“合并 + 去重”的方式保证语义正确。
func appendVary(header http.Header, values ...string) {
	existing := header.Values("Vary")

	normalized := make(map[string]string, len(existing)+len(values))
	order := make([]string, 0, len(existing)+len(values))

	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}

		key := strings.ToLower(value)
		if _, isExisted := normalized[key]; isExisted {
			return
		}

		normalized[key] = value
		order = append(order, key)
	}

	for _, value := range existing {
		for _, part := range strings.Split(value, ",") {
			add(part)
		}
	}

	for _, value := range values {
		add(value)
	}

	parts := make([]string, 0, len(order))
	for _, key := range order {
		parts = append(parts, normalized[key])
	}

	header.Set("Vary", strings.Join(parts, ", "))
}
