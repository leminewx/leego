package middleware

import (
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
	"net/http"
)

// Limit 请求限流（最好在 Recover 中间件之前调用）
func Limit(limit uint) leego.MiddlewareFunc {
	// 创建令牌桶，使用缓冲通道实现
	tokens := make(chan struct{}, limit)
	// 初始化令牌
	for range limit {
		tokens <- struct{}{}
	}

	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			select {
			// 获取令牌，处理请求
			case <-tokens:
				handle(ctx)
				tokens <- struct{}{}

			// 请求过多，并发限流
			default:
				ctx.ResponseError(http.StatusTooManyRequests, response.STATUS_429_TOO_MANY_REQUESTS)
			}
		}
	}
}
