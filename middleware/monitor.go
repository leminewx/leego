package middleware

import (
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware/collector"
)

// Collect 创建一个指标采集中间件。
//
// 该中间件用于给“路由级采集器之外的独立处理器”补充指标采集，
// 例如手工挂载的子路由或第三方处理器。引擎内置的路由会自动采集，
// 无需再叠加本中间件，否则指标会被重复计数。
//
// 参数 monitor 为 nil 时返回空操作中间件，避免空指针解引用。
func Collect(monitor *collector.Collector) leego.MiddlewareFunc {
	if monitor == nil {
		return func(next leego.HandleFunc) leego.HandleFunc { return next }
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			monitor.Increase()
			start := time.Now()

			next(ctx)

			monitor.Decrease(ctx.GetStatusCode(), time.Since(start))
		}
	}
}
