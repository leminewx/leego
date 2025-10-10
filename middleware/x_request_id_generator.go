package middleware

import (
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/headers"
	"net/http"
)

// GenerateXRequestId 生成 X-Request-Id 请求头字段
func GenerateXRequestId(generate func(req *http.Request) string) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 若请求头未携带 X-Request-Id，则生成请求 ID，并写入请求头
			if requestId := ctx.GetRequestHeader(headers.X_REQUEST_ID); requestId == "" {
				ctx.WithRequestHeader(headers.X_REQUEST_ID, generate(ctx.Request))
			}

			// 处理请求
			handle(ctx)
		}
	}
}
