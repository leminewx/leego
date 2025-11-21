package middleware

import (
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/collector"
)

// Collect 收集服务基本指标数据
func Collect(collector *collector.Collector) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 采集请求数据
			collector.Increase()

			// 调用下一个处理器
			handle(ctx)

			// 采集响应数据
			collector.Decrease(ctx.GetStatusCode(), time.Since(ctx.GetStartTime()))
		}
	}
}
