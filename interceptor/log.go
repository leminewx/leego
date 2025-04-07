package interceptor

import (
	"fmt"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// Log 打印日志的拦截器
func Log() leego.InterceptorFunc {
	const (
		_FORMAT_DATETIME = "2006-01-02 15:04:05.000"

		// INFO:     10.254.249.236:39860 - "POST /datacenter/v1/files/cases/022_V2X_LED_OBU HTTP/1.1" 200 OK
		_FORMAT1XX = "\033[37mINFO \033[0m -  %s  -  \"%s %v %s\"  |\033[47m\033[30m %d \033[0m|\033[47m\033[30m %v \033[0m|\n\n" // 白色
		_FORMAT2XX = "\033[32mINFO \033[0m -  %s  -  \"%s %v %s\"  |\033[42m\033[30m %d \033[0m|\033[42m\033[30m %v \033[0m|\n\n" // 绿色
		_FORMAT3XX = "\033[36mINFO \033[0m -  %s  -  \"%s %v %s\"  |\033[46m\033[30m %d \033[0m|\033[46m\033[30m %v \033[0m|\n\n" // 蓝色
		_FORMAT4XX = "\033[33mWARN \033[0m -  %s  -  \"%s %v %s\"  |\033[43m\033[30m %d \033[0m|\033[43m\033[30m %v \033[0m|\n\n" // 黄色
		_FORMAT5XX = "\033[35mERROR\033[0m -  %s  -  \"%s %v %s\"  |\033[45m\033[30m %d \033[0m|\033[45m\033[30m %v \033[0m|\n\n" // 红色
	)

	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			start := time.Now()
			response := response.NewResponseWriter(resp)
			handler.ServeHTTP(response, req)
			status := response.GetStatusCode()

			switch {
			// 0 ~ 199
			case status < http.StatusOK:
				fmt.Printf(_FORMAT1XX, start.Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, status, time.Since(start))
			// 200 ~ 299
			case status < http.StatusMultipleChoices:
				fmt.Printf(_FORMAT2XX, start.Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, status, time.Since(start))
			// 300 ~ 399
			case status < http.StatusBadRequest:
				fmt.Printf(_FORMAT3XX, start.Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, status, time.Since(start))
			// 400 ~ 499
			case status < http.StatusInternalServerError:
				fmt.Printf(_FORMAT4XX, start.Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, status, time.Since(start))
			// 500 ~
			default:
				fmt.Printf(_FORMAT5XX, start.Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, status, time.Since(start))
			}
		})
	}
}
