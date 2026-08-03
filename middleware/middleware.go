package middleware

import (
	"net/http"

	"gitee.com/leminewx/leego"
)

// WarpFromHTTPHandler 将 http.Handler 包装为 leego.MiddlewareFunc
func WarpFromHTTPHandler(handler http.Handler) leego.MiddlewareFunc {
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			handler.ServeHTTP(ctx.Response, ctx.Request)
			next(ctx)
		}
	}
}

// WarpFromHTTPHandlerFunc 将 http.HandlerFunc 包装为 leego.MiddlewareFunc
func WarpFromHTTPHandlerFunc(handle http.HandlerFunc) leego.MiddlewareFunc {
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			handle(ctx.Response, ctx.Request)
			next(ctx)
		}
	}
}

// UnwarpFromMiddlewareFuncToHTTPHandler 将 leego.MiddlewareFunc 解包为 http.Handler
func UnwarpFromMiddlewareFuncToHTTPHandler(handle leego.MiddlewareFunc) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		next := handle(func(ctx *leego.Context) {})
		// 使用 leego.NewContext 构造，保证 startTime、url、pathValues 等
		// 非导出字段被正确初始化；直接用结构体字面量会让 GetStartTime 返回零值，
		// 导致日志中的 latency 异常，且 GetServiceName 触发 nil 解引用
		next(leego.NewContext(resp, req))
	})
}

// UnwarpFromMiddlewareFuncToHTTPHandleFunc 将 leego.MiddlewareFunc 解包为 http.HandlerFunc
func UnwarpFromMiddlewareFuncToHTTPHandleFunc(handle leego.MiddlewareFunc) http.HandlerFunc {
	return func(resp http.ResponseWriter, req *http.Request) {
		next := handle(func(ctx *leego.Context) {})
		next(leego.NewContext(resp, req))
	}
}
