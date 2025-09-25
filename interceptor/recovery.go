package interceptor

import (
	"fmt"
	"gitee.com/leminewx/gokit/concurrency/pool"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
	"net/http"
	"runtime"
)

// Recover 异常恢复的拦截器
func Recover() leego.InterceptorFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf("|\033[45m\033[30m%s\033[0m\n\n", trace(fmt.Sprintf("%v", err)))
					http.Error(resp, response.STATUS_INTERNAL_SERVER_ERROR, http.StatusInternalServerError)
				}
			}()

			handler.ServeHTTP(resp, req)
		})
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
