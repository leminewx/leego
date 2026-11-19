package middlewares

import (
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middlewares/collector"
)

// Monitor 给独立处理器补充指标采集。
type Monitor struct {
	// target 为指标采集器；nil 表示空操作。
	target *collector.Collector
}

// NewMonitor 创建指标采集中间件。
//
// 该中间件用于给“路由级采集器之外的独立处理器”补充指标采集，
// 例如手工挂载的子路由或第三方处理器。引擎内置的路由会自动采集，
// 无需再叠加本中间件，否则指标会被重复计数。
//
// 参数 target 为 nil 时适配出的中间件为空操作，避免空指针解引用。
func NewMonitor(target *collector.Collector) *Monitor {
	return &Monitor{target: target}
}

// AdaptToMiddleware 将指标采集适配为 leego.MiddlewareFunc。
func (own *Monitor) AdaptToMiddleware() leego.MiddlewareFunc {
	monitor := own.target
	if monitor == nil {
		return func(next leego.HandleFunc) leego.HandleFunc { return next }
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			monitor.Increase()
			start := time.Now()

			// 处理器 panic 时也要上报：否则在途计数永远不回落，
			// 且 panic 会被当成成功（状态码 0 会被归一化为 200）。
			defer func() {
				status := ctx.GetStatusCode()
				if recovered := recover(); recovered != nil {
					if status == 0 {
						status = http.StatusInternalServerError
					}

					monitor.Decrease(status, time.Since(start))
					panic(recovered)
				}

				monitor.Decrease(status, time.Since(start))
			}()

			next(ctx)
		}
	}
}
