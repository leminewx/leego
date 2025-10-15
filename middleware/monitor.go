package middleware

import (
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/monitor"
)

// Collect 收集服务基本指标数据
func Collect(monitor *monitor.Monitor) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 采集请求数据
			monitor.Increase()

			// 调用下一个处理器
			handle(ctx)

			// 采集响应数据
			monitor.Decrease(ctx.GetStatusCode(), time.Since(ctx.GetStartTime()))
		}
	}
}
