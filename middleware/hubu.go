package middleware

// const _HEADER_HUBU_USER_ID = "Hubu-User-Id"

// // AuthFromHubuByHTTP 通过 HTTP 协议到 Hubu 进行接口请求鉴权，请求头需要携带 Hubu-User-Id
// func AuthFromHubuByHTTP(method, url string) leego.MiddlewareFunc {
// 	return func(handle leego.HandleFunc) leego.HandleFunc {
// 		return func(ctx *leego.Context) {
// 			// 序列化认证数据
// 			data, err := json.Marshal(map[string]any{
// 				"uid":     ctx.GetHeader(_HEADER_HUBU_USER_ID),
// 				"service": ctx.GetSeriveName(),
// 				"method":  ctx.GetMethod(),
// 				"path":    ctx.GetURL().Path,
// 			})
// 			if err != nil {
// 				loggo.Error("Failed to marshal auth data.", attr.New("error", err.Error()))
// 				ctx.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
// 				return
// 			}

// 			// 创建认证请求
// 			req, err := http.NewRequest(method, url, bytes.NewBuffer(data))
// 			if err != nil {
// 				loggo.Error("Failed to create auth request.", attr.New("error", err.Error()))
// 				ctx.ResponseFail(http.StatusInternalServerError, status.INTERNAL_SERVER_ERROR)
// 				return
// 			}
// 			req.Header.Set("Content-Type", "application/json")

// 			// 发起认证请求
// 			client := &http.Client{}
// 			resp, err := client.Do(req)
// 			if err != nil {
// 				loggo.Error("Failed to request to auth.", attr.New("error", err.Error()))
// 				ctx.ResponseFail(http.StatusBadGateway, status.BAD_GATEWAY)
// 				return
// 			}

// 			// 校验认证结果
// 			switch resp.StatusCode {
// 			case http.StatusOK:
// 				handle(ctx)
// 			case http.StatusForbidden:
// 				ctx.ResponseFail(http.StatusForbidden, status.FORBIDDEN)
// 				return
// 			default:
// 				body, _ := io.ReadAll(resp.Body)
// 				resp.Body.Close()
// 				loggo.Error("Failed to auth.", attr.New("error", string(body)))
// 				ctx.ResponseFail(http.StatusBadGateway, status.BAD_GATEWAY)
// 				return
// 			}
// 		}
// 	}
// }

// // AuthFromHubuByRPC 通过 RPC(gob) 协议到 Hubu 进行接口请求鉴权，请求头需要携带 Hubu-User-Id
// func AuthFromHubuByRPC(host string) leego.MiddlewareFunc {
// 	return func(handle leego.HandleFunc) leego.HandleFunc {
// 		return func(ctx *leego.Context) {
// 			// 创建连接
// 			client, err := rpc.Dial("tcp", host)
// 			if err != nil {
// 				loggo.Error("Failed to create auth connection.", attr.New("error", err.Error()))
// 				ctx.ResponseFail(http.StatusBadGateway, status.BAD_GATEWAY)
// 				return
// 			}

// 			// 封装请求参数
// 			args := &struct {
// 				Uid     string `json:"uid" xml:"uid"`
// 				Service string `json:"service" xml:"service"`
// 				Method  string `json:"method" xml:"method"`
// 				Path    string `json:"path" xml:"path"`
// 			}{
// 				Uid:     ctx.GetHeader(_HEADER_HUBU_USER_ID),
// 				Service: ctx.GetSeriveName(),
// 				Method:  ctx.GetMethod(),
// 				Path:    ctx.GetURL().Path,
// 			}

// 			// 响应参数：状态码
// 			var reply int

// 			// 远程调用
// 			err = client.Call("Authenticator.Auth", args, &reply)
// 			client.Close()

// 			// 校验认证结果
// 			switch reply {
// 			case http.StatusOK:
// 				handle(ctx)
// 			case http.StatusForbidden:
// 				ctx.ResponseFail(http.StatusForbidden, status.FORBIDDEN)
// 				return
// 			default:
// 				loggo.Error("Failed to auth.", attr.New("error", err.Error()))
// 				ctx.ResponseFail(http.StatusBadGateway, status.BAD_GATEWAY)
// 				return
// 			}
// 		}
// 	}
// }
