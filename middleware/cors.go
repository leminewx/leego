package middleware

import (
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/cors"
)

// Cors 跨域处理
func Cors(cors *cors.CORS) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 处理预检请求
			if cors.HandlePreflight(ctx.Response, ctx.Request) {
				return
			}

			// 处理普通请求，设置 CORS 头
			cors.SetResponseHeader(ctx.Response, ctx.Request)

			// 处理请求
			handle(ctx)
		}
	}
}
