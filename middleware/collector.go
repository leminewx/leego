package middleware

import (
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/metric"
)

// Collect 收集服务基本指标数据
func Collect(collector *metric.Collector) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 采集请求数据
			collector.Increase()

			// 处理请求
			handle(ctx)

			// 采集响应数据
			collector.Decrease(ctx.GetStatusCode(), time.Since(ctx.GetStartTime()))
		}
	}
}
