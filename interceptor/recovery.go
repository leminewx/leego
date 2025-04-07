package interceptor

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response/status"
)

// recover 异常恢复的拦截器
func Recover() leego.InterceptorFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf("|\033[45m\033[30m%s\033[0m\n\n", trace(fmt.Sprintf("%v", err)))
					http.Error(resp, status.INTERNAL_SERVER_ERROR, http.StatusInternalServerError)
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
