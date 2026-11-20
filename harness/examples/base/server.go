// Package main 演示 leego 的常用能力。
//
// 该示例同时作为框架的“可运行文档”，覆盖：
//   - 引擎创建与全局配置（名称、超时、请求体上限、可信代理）；
//   - 全局中间件与路由组中间件；
//   - JSON / 纯文本 / SSE / 文件下载 / 静态目录；
//   - JWT 签发、限流、CORS；
//   - 指标采集与内置指标分析后台；
//   - 优雅关闭。
//
// 启动后可以访问：
//
//	GET  /v1/examples                 基本 JSON 接口
//	GET  /v1/metadata                 查看全部路由指标快照
//	GET  /v1/routes                   查看全部路由定义
//	GET  /v1/error                    触发 panic，验证恢复中间件
//	GET  /v2/plain                    纯文本响应
//	POST /login/:name                 登录并签发 JWT
//	GET  /login/sse                   服务端事件流（SSE）
//	GET  /login/files/show/*filepath  在线查看文件
//	GET  /login/files/download/*filepath 下载文件
//	GET  /dashboard                   指标分析后台
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/dashboard"
	"gitee.com/leminewx/leego/harness/assetpath"
	"gitee.com/leminewx/leego/middlewares"
	"gitee.com/leminewx/leego/middlewares/cors"
	"gitee.com/leminewx/leego/middlewares/jwt"
	"gitee.com/leminewx/leego/middlewares/limiter"
	"gitee.com/leminewx/leego/response/sse"
)

// jsonResponse 是示例中统一的 JSON 响应结构。
type jsonResponse struct {
	Code string `json:"code"`
	Desc string `json:"desc"`
	Data any    `json:"data,omitempty"`
}

func main() {
	engine := newEngine()
	registerMiddlewares(engine)
	registerBasicRoutes(engine)
	registerLoginRoutes(engine)
	registerDashboard(engine)

	// 服务启动放在后台协程，主协程负责等待退出信号。
	go func() {
		if err := engine.Run(":8080"); err != nil {
			log.Printf("HTTP 服务退出: %v", err)
		}
	}()

	// 收到 SIGINT/SIGTERM 后，最多等待 5 秒让存量请求结束。
	if err := engine.WaitSignalAndShutdown(5*time.Second, nil); err != nil {
		log.Printf("优雅关闭失败: %v", err)
	}
}

// newEngine 创建引擎并设置全局配置。
func newEngine() *leego.Engine {
	leego.ShowLogo()

	return leego.NewEngine(
		leego.WithEngineName("leego-base-example"),
		// 四个服务端超时，避免慢连接长期占用文件描述符。
		leego.WithServerTimeouts(5*time.Second, 10*time.Second, 10*time.Second, 60*time.Second),
		// 请求体上限 8 MiB，防止超大请求打爆内存。
		leego.WithMaxBodyBytes(8<<20),
		// 严格 JSON：未知字段直接报错，便于尽早发现前后端字段不一致。
		leego.WithStrictJSON(true),
		// 仅信任本机回环地址上代理传来的 X-Forwarded-For。
		leego.WithTrustedProxies("127.0.0.1/32", "::1/128"),
	)
}

// registerMiddlewares 注册全局与路由组中间件。
func registerMiddlewares(engine *leego.Engine) {
	// 令牌桶：每秒补充 30 个令牌，允许 60 个瞬时突发；等待上限 5 秒。
	rateLimiter := limiter.NewTokenBucketLimiter(30, 60, limiter.Config{WaitTimeout: 5 * time.Second})

	corsMiddleware := cors.New()
	corsMiddleware.SetAllowOrigins("*")
	corsMiddleware.SetMaxAge(3600)

	engine.UseMiddlewares(
		// 访问日志：输出方法、路径、状态码与耗时。
		middlewares.NewLogger().AdaptToMiddleware(),
		// 恢复：捕获 panic 并返回 500，避免进程退出。
		middlewares.NewRecovery().AdaptToMiddleware(),
		// 限流：等待上限由 Config.WaitTimeout 决定，超时返回 429。
		rateLimiter.AdaptToMiddleware(),
		// 跨域：自动处理预检请求。
		corsMiddleware.AdaptToMiddleware(),
		// 自定义中间件：注入请求 ID，便于链路追踪。
		func(next leego.HandleFunc) leego.HandleFunc {
			return func(ctx *leego.Context) {
				ctx.SetRequestHeader("X-Request-ID", "leego-"+strconv.FormatInt(time.Now().UnixNano(), 10))
				next(ctx)
			}
		},
	)
}

// registerBasicRoutes 注册基础演示路由。
func registerBasicRoutes(engine *leego.Engine) {
	// 路由组 /v1：演示组级中间件与指标接口。
	v1 := engine.NewGroup("/v1")
	v1.UseMiddlewares(
		func(next leego.HandleFunc) leego.HandleFunc {
			return func(ctx *leego.Context) {
				// 组级前置逻辑（此处仅打印，实际项目可做鉴权、租户隔离等）。
				next(ctx)
			}
		},
	)

	v1.GET("/examples", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, jsonResponse{Code: "00000", Desc: "ok", Data: leego.JSON{
			"request_id": ctx.GetRequestHeader("X-Request-ID"),
			"client_ip":  ctx.GetClientIP(),
			"server":     ctx.GetServiceName(),
			"time":       time.Now().Format(time.DateTime),
		}})
	}, "验证基本接口", "v1", "JSON")

	// 指标快照：直接返回 dashboard 使用的同一份数据。
	v1.GET("/metadata", func(ctx *leego.Context) {
		report := engine.CollectReport()
		_ = ctx.ResponseJSON(http.StatusOK, jsonResponse{Code: "00000", Desc: "ok", Data: report})
	}, "获取所有路由监控数据", "v1", "监控数据")

	// 路由表：展示方法、模式、中间件数量与启用状态。
	v1.GET("/routes", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, jsonResponse{Code: "00000", Desc: "ok", Data: engine.GetRoutes()})
	}, "获取路由表", "v1", "路由")

	// 故意触发 panic，验证 Recover 中间件不会让进程崩溃。
	v1.GET("/error", func(ctx *leego.Context) {
		var numbers []int
		_ = numbers[1]
	}, "触发 panic 验证恢复中间件", "v1", "异常")

	// 纯文本响应。
	engine.GET("/v2/plain", func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusOK, "this is a plain text response")
	}, "验证纯文本响应", "v2", "字符串")

	// 请求体读取：POST 内容原样回显。
	engine.POST("/v2/echo", func(ctx *leego.Context) {
		body, err := ctx.ReadBody()
		if err != nil {
			ctx.ResponseError(http.StatusBadRequest, "读取请求体失败: "+err.Error())
			return
		}

		_ = ctx.ResponseBytes(http.StatusOK, body)
	}, "回显请求体", "v2", "请求体")
}

// registerLoginRoutes 注册登录、SSE 与文件相关路由。
func registerLoginRoutes(engine *leego.Engine) {
	// HMAC-SHA384 签名器：生产环境应从配置中心或环境变量读取密钥。
	signer, err := jwt.NewHMACSigner(jwt.HS384, []byte("leego-demo-secret-please-change"))
	if err != nil {
		log.Fatalf("初始化 JWT 签名器失败: %v", err)
	}

	// 解析器负责校验签名、有效期与签发者。
	parser := jwt.NewParser()
	parser.SetClockSkew(30 * time.Second)

	login := engine.NewGroup("/login")

	login.POST("/:name", func(ctx *leego.Context) {
		name := ctx.GetPathValue("name")
		if name == "" {
			ctx.ResponseError(http.StatusBadRequest, "缺少用户名")
			return
		}

		signature, signErr := signer.Sign(&struct {
			*jwt.StandardClaims
			Email string `json:"email"`
			Name  string `json:"name"`
		}{
			StandardClaims: &jwt.StandardClaims{
				IssuedAt:  time.Now().Unix(),
				Issuer:    "leego",
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
				Subject:   name,
				Audiences: []string{"leego"},
				Id:        name,
			},
			Email: name + "@example.com",
			Name:  name,
		})
		if signErr != nil {
			ctx.ResponseError(http.StatusInternalServerError, "签发令牌失败")
			return
		}

		ctx.SetResponseHeader("Authorization", "Bearer "+string(signature))
		_ = ctx.ResponseJSON(http.StatusOK, jsonResponse{Code: "00000", Desc: "ok", Data: leego.JSON{
			"token": string(signature),
		}})
	}, "登录接口", "认证", "JWT")

	// 受保护接口：手动解析令牌，演示解析器的完整用法。
	login.GET("/profile", func(ctx *leego.Context) {
		token := strings.TrimPrefix(ctx.GetRequestHeader("Authorization"), "Bearer ")
		if token == "" {
			ctx.ResponseError(http.StatusUnauthorized, "缺少令牌")
			return
		}

		claims := &jwt.StandardClaims{}
		if parseErr := parser.ParseAndValidateStandardClaimsWithIssuerAndAudiences(signer, token, "leego", "leego", claims); parseErr != nil {
			ctx.ResponseError(http.StatusUnauthorized, "令牌无效: "+parseErr.Error())
			return
		}

		_ = ctx.ResponseJSON(http.StatusOK, jsonResponse{Code: "00000", Desc: "ok", Data: claims})
	}, "获取当前登录信息", "认证", "JWT")

	// 单连接 SSE：由回调函数自行生产事件。
	login.GET("/sse", func(ctx *leego.Context) {
		_ = ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
			events := make(chan *sse.Message, 1)

			go func() {
				defer close(events)

				for index := 0; index < 10; index++ {
					data, _ := json.Marshal(jsonResponse{
						Code: "00000",
						Desc: "ok",
						Data: leego.JSON{
							"index": index,
							"time":  time.Now().Format(time.DateTime),
						},
					})

					select {
					case <-requestCtx.Done():
						return
					case events <- &sse.Message{Event: "tick", Id: strconv.Itoa(index), Data: data}:
					}

					select {
					case <-requestCtx.Done():
						return
					case <-time.After(time.Second):
					}
				}
			}()

			return events
		})
	}, "SSE 接口", "长连接", "SSE")

	// 文件在线查看与下载：路径限定在演示数据目录 harness/testdata 内。
	login.GET("/files/show/*filepath", func(ctx *leego.Context) {
		ctx.ResponseFile(assetpath.Resolve(ctx.GetPathValue("filepath")))
	}, "在线查看文件", "文件", "查看")

	login.GET("/files/download/*filepath", func(ctx *leego.Context) {
		ctx.ResponseAttachment(assetpath.Resolve(ctx.GetPathValue("filepath")))
	}, "下载文件", "文件", "下载")

	// 静态目录：把演示数据目录 harness/testdata 挂载到 /static。
	engine.SetStatic("/static", assetpath.GetDir())
}

// registerDashboard 注册内置指标分析后台。
//
// 后台提供总览、路由明细、CSV/JSON 导出与健康检查，
// 默认挂载路径为 /dashboard、/dashboard/routes、/dashboard/health。
func registerDashboard(engine *leego.Engine) {
	dashboardError := engine.RegisterDashboard(
		"/dashboard",
		dashboard.WithTitle("Leego 指标分析"),
		dashboard.WithRefreshInterval(5*time.Second),
		dashboard.WithMaxRoutes(200),
		dashboard.EnableExport(true),
	)
	if dashboardError != nil {
		fmt.Printf("注册指标后台失败: %v\n", dashboardError)
	}
}
