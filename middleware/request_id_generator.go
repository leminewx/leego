package middleware

import (
	"net/http"

	"gitee.com/leminewx/leego"
)

/*
GenerateRequestId 生成请求 ID ，然后绑定到指定请求头
  - key：请求头名称
  - generate：生成请求 ID 的函数
*/
func GenerateRequestId(key string, generate func(req *http.Request) string) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 若指定头未指定或值为空，则生成请求 ID，并写入请求头
			if requestId := ctx.GetRequestHeader(key); requestId == "" {
				ctx.WithRequestHeader(key, generate(ctx.Request))
			}

			// 调用下一个处理器
			handle(ctx)
		}
	}
}
