package middleware

import (
	"fmt"
	"net/http"
	"runtime"

	"gitee.com/leminewx/gokit/concurrency/pool"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

// Recover 异常恢复
func Recover() leego.MiddlewareFunc {
	fmt.Println()
	return func(handle leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf("|\033[45m\033[30m%s\033[0m\n\n", trace(fmt.Sprintf("%v", err)))
					ctx.ResponseError(http.StatusInternalServerError, response.STATUS_INTERNAL_SERVER_ERROR)
				}
			}()

			handle(ctx)
		}
	}
}

func trace(message string) string {
	// 获取调用栈信息(跳过前3层调用)
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])

	builder := pool.DefaultBytesBufferPool.Alloc()
	defer pool.DefaultBytesBufferPool.Dealloc(builder)

	// 封装调用信息
	builder.WriteString(message + "\nTraceback:")
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		file, line := fn.FileLine(pc)
		builder.WriteString(fmt.Sprintf("\n\t%s:%d", file, line))
	}

	return builder.String()
}
