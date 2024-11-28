package leego

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gitee.com/leminewx/leego/cors"
)

// MiddlewareFunc 定义中间件函数的类型
type MiddlewareFunc func(HandleFunc) HandleFunc

// Log 打印日志的中间件
func Log() MiddlewareFunc {
	const (
		TimeFormatter = "2006-01-02 15:04:05.000"

		// INFO:     10.254.249.236:39860 - "POST /datacenter/v1/files/cases/022_V2X_LED_OBU HTTP/1.1" 200 OK
		Format1XX = "\n\033[37mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[47m\033[30m %d \033[0m|\033[47m\033[30m %v \033[0m|\n"
		Format2XX = "\n\033[32mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[42m\033[30m %d \033[0m|\033[42m\033[30m %v \033[0m|\n"
		Format3XX = "\n\033[33mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[43m\033[30m %d \033[0m|\033[43m\033[30m %v \033[0m|\n"
		Format4XX = "\n\033[31mError\033[0m:  %s  -  \"%s %v %s\"  |\033[41m\033[30m %d \033[0m|\033[41m\033[30m %v \033[0m|\n"
		Format5XX = "\n\033[35mError\033[0m:  %s  -  \"%s %v %s\"  |\033[45m\033[30m %d \033[0m|\033[45m\033[30m %v \033[0m|\n"
	)

	return func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			start := time.Now()

			handle(ctx)
			if ctx.StatusCode == 0 {
				ctx.StatusCode = http.StatusOK
			}

			switch {
			// 0 ~ 199
			case ctx.StatusCode < http.StatusOK:
				fmt.Printf(Format1XX, start.Format(TimeFormatter), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 200 ~ 299
			case ctx.StatusCode < http.StatusMultipleChoices:
				fmt.Printf(Format2XX, start.Format(TimeFormatter), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 300 ~ 399
			case ctx.StatusCode < http.StatusBadRequest:
				fmt.Printf(Format3XX, start.Format(TimeFormatter), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 400 ~ 499
			case ctx.StatusCode < http.StatusInternalServerError:
				fmt.Printf(Format4XX, start.Format(TimeFormatter), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 500 ~
			default:
				fmt.Printf(Format5XX, start.Format(TimeFormatter), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			}
		}
	}
}

// Cors 跨域处理的中间件
func Cors(cors *cors.CORS) MiddlewareFunc {
	const (
		keyAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
		keyAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
		keyAccessControlAllowMethods     = "Access-Control-Allow-Methods"
		keyAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
		keyAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
		keyAccessControlMaxAge           = "Access-Control-Max-Age"
	)

	return func(handle HandleFunc) HandleFunc {
		const keyOrigin = "Origin"

		var (
			valAllowCredentials = "false"
			valAllowMethods     = cors.GetAllowMethods()
			valAllowHeaders     = cors.GetAllowHeaders()
			valExposeHeaders    = cors.GetExposeHeaders()
			valMaxAge           = strconv.Itoa(int(cors.MaxAge))
		)

		if cors.AllowCredentials {
			valAllowCredentials = "true"
		}

		return func(ctx *Context) {
			if origin := cors.GetAllowOrigins(ctx.GetHeader(keyOrigin)); origin != "" {
				ctx.SetHeader(keyAccessControlAllowOrigin, origin)
				ctx.SetHeader(keyAccessControlAllowCredentials, valAllowCredentials)
				ctx.SetHeader(keyAccessControlAllowMethods, valAllowMethods)
				ctx.SetHeader(keyAccessControlAllowHeaders, valAllowHeaders)
				ctx.SetHeader(keyAccessControlExposeHeaders, valExposeHeaders)
				ctx.SetHeader(keyAccessControlMaxAge, valMaxAge)
			}

			if ctx.GetMethod() == http.MethodOptions {
				// 预检请求（Preflight request），直接返回 204 状态码
				ctx.WriteCode(http.StatusNoContent)
				return
			}

			handle(ctx)
		}
	}
}
