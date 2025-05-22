package middleware

import (
	"net/http"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/cors"
)

// Cors 跨域处理的中间件
func Cors(cors *cors.CORS) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 设置预检响应头
			cors.SetResponseHeader(ctx.GetResponse())

			// 预检请求（Preflight request）
			if ctx.GetMethod() == http.MethodOptions {
				// 直接返回 204 状态码
				ctx.WriteCode(http.StatusNoContent)
				return
			}

			handle(ctx)
		}
	}
}
