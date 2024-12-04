package leego

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/rpc"
	"strconv"
	"time"

	"gitee.com/leminewx/leego/cors"
	"gitee.com/leminewx/loggo"
	"gitee.com/leminewx/loggo/attr"
)

// MiddlewareFunc 定义中间件函数的类型
type MiddlewareFunc func(HandleFunc) HandleFunc

// Log 打印日志的中间件
func Log() MiddlewareFunc {
	const (
		formatTime = "2006-01-02 15:04:05.000"

		// INFO:     10.254.249.236:39860 - "POST /datacenter/v1/files/cases/022_V2X_LED_OBU HTTP/1.1" 200 OK
		format1XX = "\n\033[37mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[47m\033[30m %d \033[0m|\033[47m\033[30m %v \033[0m|\n"
		format2XX = "\n\033[32mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[42m\033[30m %d \033[0m|\033[42m\033[30m %v \033[0m|\n"
		format3XX = "\n\033[33mInfo \033[0m:  %s  -  \"%s %v %s\"  |\033[43m\033[30m %d \033[0m|\033[43m\033[30m %v \033[0m|\n"
		format4XX = "\n\033[31mError\033[0m:  %s  -  \"%s %v %s\"  |\033[41m\033[30m %d \033[0m|\033[41m\033[30m %v \033[0m|\n"
		format5XX = "\n\033[35mError\033[0m:  %s  -  \"%s %v %s\"  |\033[45m\033[30m %d \033[0m|\033[45m\033[30m %v \033[0m|\n"
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
				fmt.Printf(format1XX, start.Format(formatTime), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 200 ~ 299
			case ctx.StatusCode < http.StatusMultipleChoices:
				fmt.Printf(format2XX, start.Format(formatTime), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 300 ~ 399
			case ctx.StatusCode < http.StatusBadRequest:
				fmt.Printf(format3XX, start.Format(formatTime), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 400 ~ 499
			case ctx.StatusCode < http.StatusInternalServerError:
				fmt.Printf(format4XX, start.Format(formatTime), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			// 500 ~
			default:
				fmt.Printf(format5XX, start.Format(formatTime), ctx.GetMethod(), ctx.GetURL(), ctx.GetProtocol(), ctx.StatusCode, time.Since(start))
			}
		}
	}
}

// Cors 跨域处理的中间件
func Cors(cors *cors.CORS) MiddlewareFunc {
	return func(handle HandleFunc) HandleFunc {
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
			if origin := cors.GetAllowOrigins(ctx.GetHeader(HeaderOrigin)); origin != "" {
				ctx.SetHeader(HeaderAccessControlAllowCredentials, valAllowCredentials)
				ctx.SetHeader(HeaderAccessControlAllowOrigin, origin)

				if valAllowMethods != "" {
					ctx.SetHeader(HeaderAccessControlAllowMethods, valAllowMethods)
				}

				if valAllowHeaders != "" {
					ctx.SetHeader(HeaderAccessControlAllowHeaders, valAllowHeaders)
				}

				if valExposeHeaders != "" {
					ctx.SetHeader(HeaderAccessControlExposeHeaders, valExposeHeaders)
				}

				ctx.SetHeader(HeaderAccessControlMaxAge, valMaxAge)
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

// AuthFromHubuByHTTP 通过 HTTP 协议到 Hubu 进行接口请求鉴权，请求头需要携带 Hubu-User-Id
func AuthFromHubuByHTTP(method, url string) MiddlewareFunc {
	const keyHubuUserId = "Hubu-User-Id"
	return func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			// 序列化认证数据
			data, err := json.Marshal(map[string]any{
				"uid":     ctx.GetHeader(keyHubuUserId),
				"service": ctx.GetSeriveName(),
				"method":  ctx.GetMethod(),
				"path":    ctx.GetURL().Path,
			})
			if err != nil {
				loggo.Error("Failed to marshal auth data.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusInternalServerError, _StatusMethodInternalServerError)
				return
			}

			// 创建认证请求
			req, err := http.NewRequest(method, url, bytes.NewBuffer(data))
			if err != nil {
				loggo.Error("Failed to create auth request.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusInternalServerError, _StatusMethodInternalServerError)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			// 发起认证请求
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				loggo.Error("Failed to request to auth.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusBadGateway, _StatusBadGatewayOnHubu)
				return
			}

			// 校验认证结果
			switch resp.StatusCode {
			case http.StatusOK:
				handle(ctx)
			case http.StatusForbidden:
				ctx.ResponseFail(http.StatusForbidden, _StatusForbidden)
				return
			default:
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				loggo.Error("Failed to auth.", attr.New("error", string(body)))
				ctx.ResponseFail(http.StatusBadGateway, _StatusBadGatewayOnHubu)
				return
			}
		}
	}
}

// AuthFromHubuByRPC 通过 RPC(gob) 协议到 Hubu 进行接口请求鉴权，请求头需要携带 Hubu-User-Id
func AuthFromHubuByRPC(host string) MiddlewareFunc {
	const keyHubuUserId = "Hubu-User-Id"
	return func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {

			// 创建连接
			client, err := rpc.Dial("tcp", host)
			if err != nil {
				loggo.Error("Failed to create auth connection.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusBadGateway, _StatusBadGatewayOnHubu)
				return
			}

			// 封装请求参数
			args := &struct {
				Uid     string `json:"uid" xml:"uid"`
				Service string `json:"service" xml:"service"`
				Method  string `json:"method" xml:"method"`
				Path    string `json:"path" xml:"path"`
			}{
				Uid:     ctx.GetHeader(keyHubuUserId),
				Service: ctx.GetSeriveName(),
				Method:  ctx.GetMethod(),
				Path:    ctx.GetURL().Path,
			}

			// 响应参数：状态码
			var reply int

			// 远程调用
			err = client.Call("Authenticator.Auth", args, &reply)
			client.Close()

			// 校验认证结果
			switch reply {
			case http.StatusOK:
				handle(ctx)
			case http.StatusForbidden:
				ctx.ResponseFail(http.StatusForbidden, _StatusForbidden)
				return
			default:
				loggo.Error("Failed to auth.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusBadGateway, _StatusBadGatewayOnHubu)
				return
			}
		}
	}
}
