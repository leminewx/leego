package interceptor

import (
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/interceptor/collector"
	"time"
)

// Collect 收集服务基本指标数据
func Collect(collector *collector.Collector) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 记录起始时间
			start := time.Now()

			// 处理响应，并收集数据
			collector.Increase()
			handle(ctx)
			collector.Decrease(ctx.GetStatusCode(), time.Since(start))
		}
	}
}
