package leego

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/status"
	"gitee.com/leminewx/loggo"
)

// Interceptor 定义拦截器处理函数的类型
type Interceptor func(http.Handler) http.Handler

// Log 打印日志的拦截器
func Log() Interceptor {
	const (
		_FORMAT_DATETIME = "2006-01-02 15:04:05.000"

		// INFO:     10.254.249.236:39860 - "POST /datacenter/v1/files/cases/022_V2X_LED_OBU HTTP/1.1" 200 OK
		_FORMAT1XX = "\033[37mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[47m\033[30m %d \033[0m|\033[47m\033[30m %v \033[0m|\n\n"
		_FORMAT2XX = "\033[32mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[42m\033[30m %d \033[0m|\033[42m\033[30m %v \033[0m|\n\n"
		_FORMAT3XX = "\033[33mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[43m\033[30m %d \033[0m|\033[43m\033[30m %v \033[0m|\n\n"
		_FORMAT4XX = "\033[31mError\033[0m:  %s  -  \"%s %v %s\"  |\033[41m\033[30m %d \033[0m|\033[41m\033[30m %v \033[0m|\n\n"
		_FORMAT5XX = "\033[35mError\033[0m:  %s  -  \"%s %v %s\"  |\033[45m\033[30m %d \033[0m|\033[45m\033[30m %v \033[0m|\n\n"
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

// recover 异常恢复的拦截器
func Recover() Interceptor {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					loggo.Error(fmt.Sprintf("%s\n\n", trace(fmt.Sprintf("%v", err))))
					http.Error(resp, status.STATUS_INTERNAL_SERVER_ERROR, http.StatusInternalServerError)
				}
			}()

			handler.ServeHTTP(resp, req)
		})
	}
}

func trace(message string) string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:]) // skip first 3 caller

	var str strings.Builder
	str.WriteString(message + "\nTraceback:")
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		file, line := fn.FileLine(pc)
		str.WriteString(fmt.Sprintf("\n\t%s:%d", file, line))
	}

	return str.String()
}
