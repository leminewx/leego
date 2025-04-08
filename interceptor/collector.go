package interceptor

import (
	"net/http"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/interceptor/collector"
)

// Collect 收集服务器基本指标数据
func Collect(collector *collector.Collector) leego.InterceptorFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			collector.Increase()
			handler.ServeHTTP(resp, req)
			collector.Decrease()
		})
	}
}
