package interceptor

import (
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/interceptor/collector"
	"gitee.com/leminewx/leego/response"
	"net/http"
	"time"
)

// Collect 收集服务基本指标数据的拦截器
func Collect(collector *collector.Collector) leego.InterceptorFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			// 记录起始时间
			start := time.Now()

			// 转换响应体
			var res response.Writer
			if _, ok := resp.(response.Writer); !ok {
				res = response.NewWriter(resp)
			}

			// 处理响应，并收集数据
			collector.Increase()
			handler.ServeHTTP(res, req)
			collector.Decrease(res.GetStatusCode(), time.Since(start))
		})
	}
}
