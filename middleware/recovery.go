package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"runtime"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

/*
Recover 当请求触发 panic 时恢复正常
  - notify：异步通知，可设为 nil
*/
func Recover(notify func(req *http.Request, traceback string)) leego.MiddlewareFunc {
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 处理异常
			defer func() {
				if err := recover(); err != nil {
					// 获取错误信息
					msg := trace(fmt.Sprintf("%v", err))

					// 打印错误信息
					fmt.Printf("\033[45m\033[30m%s\033[0m\n\n", msg)

					// 异步通知
					if notify != nil {
						go notify(ctx.Request, msg)
					}

					// 若该请求还未处理，则响应错误
					if ctx.GetStatusCode() == 0 {
						ctx.ResponseError(http.StatusInternalServerError, response.STATUS_500_INTERNAL_SERVER_ERROR)
					}
				}
			}()

			// 处理请求
			handle(ctx)
		}
	}
}

func trace(message string) string {
	// 获取调用栈信息(跳过前3层调用)
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])

	// 封装调用信息
	var builder bytes.Buffer
	builder.WriteString(message)
	builder.WriteString("\nTraceback:")
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		file, line := fn.FileLine(pc)
		builder.WriteString(fmt.Sprintf("\n    %s:%d", file, line))
	}

	return builder.String()
}
