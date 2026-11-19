// Package middleware 提供与框架无关的通用 HTTP 中间件。
//
// 所有中间件都实现 leego.MiddlewareFunc，通过 leego.Group.UseMiddlewares
// 挂载到引擎（全局）或路由组（局部）上。
package middleware

import (
	"net/http"

	"gitee.com/leminewx/leego"
)

// WrapFromHTTPHandler 将标准库 http.Handler 适配为 leego.MiddlewareFunc。
//
// 适配后的中间件会先执行标准库处理器，再调用 next；因此它适合挂载
// 那些“自行写响应”的处理器（如图片服务、pprof、第三方管理台）。
// 若标准库处理器没有写响应，后续 next 仍然可以继续处理。
func WrapFromHTTPHandler(handler http.Handler) leego.MiddlewareFunc {
	if handler == nil {
		return func(next leego.HandleFunc) leego.HandleFunc { return next }
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			handler.ServeHTTP(ctx.Response, ctx.Request)
			next(ctx)
		}
	}
}

// WrapFromHTTPHandlerFunc 将标准库 http.HandlerFunc 适配为 leego.MiddlewareFunc。
func WrapFromHTTPHandlerFunc(handle http.HandlerFunc) leego.MiddlewareFunc {
	if handle == nil {
		return func(next leego.HandleFunc) leego.HandleFunc { return next }
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			handle(ctx.Response, ctx.Request)
			next(ctx)
		}
	}
}

// UnwrapFromMiddlewareFuncToHTTPHandler 将 leego.MiddlewareFunc 解包为 http.Handler。
//
// 解包后的处理器使用 NewContext 构造上下文，保证 startTime、URL、
// 响应包装器等非导出字段被正确初始化；业务处理链的最末端为空操作，
// 因此该函数适合只关心“中间件副作用”的场景（如指标采集、访问日志）。
func UnwrapFromMiddlewareFuncToHTTPHandler(handle leego.MiddlewareFunc) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		UnwrapFromMiddlewareFuncToHTTPHandleFunc(handle)(resp, req)
	})
}

// UnwrapFromMiddlewareFuncToHTTPHandleFunc 将 leego.MiddlewareFunc 解包为 http.HandlerFunc。
func UnwrapFromMiddlewareFuncToHTTPHandleFunc(handle leego.MiddlewareFunc) http.HandlerFunc {
	if handle == nil {
		return func(resp http.ResponseWriter, req *http.Request) {}
	}

	return func(resp http.ResponseWriter, req *http.Request) {
		next := handle(func(ctx *leego.Context) {})
		next(leego.NewContext(resp, req))
	}
}
