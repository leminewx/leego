package leego

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/rpc"

	"gitee.com/leminewx/leego/cors"
	"gitee.com/leminewx/loggo"
	"gitee.com/leminewx/loggo/attr"
)

// MiddlewareFunc 定义处理器中间件函数的类型
type MiddlewareFunc func(HandleFunc) HandleFunc

// Cors 跨域处理的中间件
func Cors(cors *cors.CORS) MiddlewareFunc {
	cors.Init()
	return func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			// 设置预检响应头
			cors.SetResponseHeader(ctx.GetResponse())

			// 预检请求（Preflight request）
			if ctx.GetMethod() == http.MethodOptions {
				// 直接返回 204 状态码
				ctx.WriteCode(http.StatusNoContent)
				return
			}

			handle(ctx)
		}
	}
}

const _HEADER_HUBU_USER_ID = "Hubu-User-Id"

// AuthFromHubuByHTTP 通过 HTTP 协议到 Hubu 进行接口请求鉴权，请求头需要携带 Hubu-User-Id
func AuthFromHubuByHTTP(method, url string) MiddlewareFunc {
	return func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			// 序列化认证数据
			data, err := json.Marshal(map[string]any{
				"uid":     ctx.GetHeader(_HEADER_HUBU_USER_ID),
				"service": ctx.GetSeriveName(),
				"method":  ctx.GetMethod(),
				"path":    ctx.GetURL().Path,
			})
			if err != nil {
				loggo.Error("Failed to marshal auth data.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusInternalServerError, _STATUS_INTERNAL_SERVER_ERROR)
				return
			}

			// 创建认证请求
			req, err := http.NewRequest(method, url, bytes.NewBuffer(data))
			if err != nil {
				loggo.Error("Failed to create auth request.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusInternalServerError, _STATUS_INTERNAL_SERVER_ERROR)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			// 发起认证请求
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				loggo.Error("Failed to request to auth.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusBadGateway, _STATUS_BAD_GATEWAY)
				return
			}

			// 校验认证结果
			switch resp.StatusCode {
			case http.StatusOK:
				handle(ctx)
			case http.StatusForbidden:
				ctx.ResponseFail(http.StatusForbidden, _STATUS_FORBIDDEN)
				return
			default:
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				loggo.Error("Failed to auth.", attr.New("error", string(body)))
				ctx.ResponseFail(http.StatusBadGateway, _STATUS_BAD_GATEWAY)
				return
			}
		}
	}
}

// AuthFromHubuByRPC 通过 RPC(gob) 协议到 Hubu 进行接口请求鉴权，请求头需要携带 Hubu-User-Id
func AuthFromHubuByRPC(host string) MiddlewareFunc {
	return func(handle HandleFunc) HandleFunc {
		return func(ctx *Context) {
			// 创建连接
			client, err := rpc.Dial("tcp", host)
			if err != nil {
				loggo.Error("Failed to create auth connection.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusBadGateway, _STATUS_BAD_GATEWAY)
				return
			}

			// 封装请求参数
			args := &struct {
				Uid     string `json:"uid" xml:"uid"`
				Service string `json:"service" xml:"service"`
				Method  string `json:"method" xml:"method"`
				Path    string `json:"path" xml:"path"`
			}{
				Uid:     ctx.GetHeader(_HEADER_HUBU_USER_ID),
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
				ctx.ResponseFail(http.StatusForbidden, _STATUS_FORBIDDEN)
				return
			default:
				loggo.Error("Failed to auth.", attr.New("error", err.Error()))
				ctx.ResponseFail(http.StatusBadGateway, _STATUS_BAD_GATEWAY)
				return
			}
		}
	}
}
