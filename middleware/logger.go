package middleware

import (
	"fmt"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
)

const (
	_FORMAT_DATETIME = "2006-01-02 15:04:05.000"

	// INFO : 10.254.249.236:39860 - "POST /api/v1/users/user@qq.com HTTP/1.1" | 200 | 123.456ms |
	_FORMAT_1XX = "\033[37mINFO \033[0m -  %s  -  \"%s  %s  %v\"  |\033[47m\033[30m %d \033[0m|\033[47m\033[30m %v \033[0m|\n" // 白色
	_FORMAT_2XX = "\033[32mINFO \033[0m -  %s  -  \"%s  %s  %v\"  |\033[42m\033[30m %d \033[0m|\033[42m\033[30m %v \033[0m|\n" // 绿色
	_FORMAT_3XX = "\033[36mINFO \033[0m -  %s  -  \"%s  %s  %v\"  |\033[46m\033[30m %d \033[0m|\033[46m\033[30m %v \033[0m|\n" // 蓝色
	_FORMAT_4XX = "\033[33mWARN \033[0m -  %s  -  \"%s  %s  %v\"  |\033[43m\033[30m %d \033[0m|\033[43m\033[30m %v \033[0m|\n" // 黄色
	_FORMAT_5XX = "\033[35mERROR\033[0m -  %s  -  \"%s  %s  %v\"  |\033[45m\033[30m %d \033[0m|\033[45m\033[30m %v \033[0m|\n" // 红色
)

/*
Log 打印请求日志
  - record：用于记录日志信息，可设为 nil
*/
func Log(record func(ctx *leego.Context)) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 调用下一个处理器
			handle(ctx)

			// 获取响应信息
			start := ctx.GetStartTime()
			status := ctx.GetStatusCode()

			// 打印基本信息
			switch {
			// 0 ~ 199
			case status < http.StatusOK:
				fmt.Printf(_FORMAT_1XX, start.Format(_FORMAT_DATETIME), ctx.GetProtocol(), ctx.GetMethod(), ctx.GetPath(), status, time.Since(start))

			// 200 ~ 299
			case status < http.StatusMultipleChoices:
				fmt.Printf(_FORMAT_2XX, start.Format(_FORMAT_DATETIME), ctx.GetProtocol(), ctx.GetMethod(), ctx.GetPath(), status, time.Since(start))

			// 300 ~ 399
			case status < http.StatusBadRequest:
				fmt.Printf(_FORMAT_3XX, start.Format(_FORMAT_DATETIME), ctx.GetProtocol(), ctx.GetMethod(), ctx.GetPath(), status, time.Since(start))

			// 400 ~ 499
			case status < http.StatusInternalServerError:
				fmt.Printf(_FORMAT_4XX, start.Format(_FORMAT_DATETIME), ctx.GetProtocol(), ctx.GetMethod(), ctx.GetPath(), status, time.Since(start))

			// 500 ~
			default:
				fmt.Printf(_FORMAT_5XX, start.Format(_FORMAT_DATETIME), ctx.GetProtocol(), ctx.GetMethod(), ctx.GetPath(), status, time.Since(start))
			}

			// 记录请求响应信息
			if record != nil {
				record(ctx)
			}
		}
	}
}
