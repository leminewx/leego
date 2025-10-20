package controller

import "gitee.com/leminewx/leego"

// GetRequestMetricSummary 获取请求指标的摘要数据
func GetRequestMetricSummary(engine *leego.Engine) leego.HandleFunc {
	return func(ctx *leego.Context) {
		metrics := engine.GetAllRouteMonitors()

		//	统计成功率：状态码 < 400
		rateSuccess
	}
}
