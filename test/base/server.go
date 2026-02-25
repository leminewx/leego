package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"gitee.com/leminewx/gokit/ggid"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/middleware/jwt"
	"gitee.com/leminewx/leego/middleware/limiter"
	"gitee.com/leminewx/leego/response/sse"
)

func main() {
	// 创建 HTTP 引擎
	leego.ShowLogo()
	engine := leego.NewEngine()

	// Limiter 限流中间件
	middlewareLimiter := limiter.NewTokenBucketLimiter(30, 1000)

	// CORS 跨域中间件
	middlewareCORS := cors.NewDefaultCORS()
	middlewareCORS.SetMaxAge(100)
	middlewareCORS.SetAllowOrigins("*")

	// JWT 认证中间件
	// middlewareJWTParser := jwt.NewParser()
	middlewareJWTSigner := jwt.NewHMACSigner(jwt.HS384, []byte("secret123456789."))

	// 注册中间件
	engine.UseMiddlewares(
		middleware.Recover(),
		middlewareLimiter.GetMiddleware(5*time.Second),
		middlewareCORS.GetMiddleware(),
	)

	engine.UseMiddlewares(
		func(next leego.HandleFunc) leego.HandleFunc {
			fmt.Println("Init middleware 1")
			return func(ctx *leego.Context) {
				ctx.Request.Header.Set("X-Request-ID", ggid.New().String())
				next(ctx)
			}
		},
	)

	// 路由组1 - 基本功能
	{
		group1 := engine.Group("/v1")
		group1.UseMiddlewares(
			func(next leego.HandleFunc) leego.HandleFunc {
				fmt.Println("Init middleware 2")
				return func(ctx *leego.Context) {
					// fmt.Println("/v1 1 start")
					next(ctx)
					// fmt.Println("/v1 1 end")
				}
			},
			func(next leego.HandleFunc) leego.HandleFunc {
				fmt.Println("Init middleware 3")
				return func(ctx *leego.Context) {
					// fmt.Println("/v1 2 start")
					next(ctx)
					// fmt.Println("/v1 2 end")
				}
			},
		)

		group1.GET("/test", func(ctx *leego.Context) {
			// fmt.Printf("/v1/a ok  -  request_id: %s\n", ctx.GetRequestHeader(headers.X_REQUEST_ID))
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
		}, "验证基本接口", "v1", "JSON")

		group1.GET("/metadata", func(ctx *leego.Context) {
			collectors := engine.GetAllHandlerCollectors()
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok", "data": collectors})
		}, "获取所有路由监控数据", "v1", "监控数据")

		group1.GET("/error", func(ctx *leego.Context) {
			slice := make([]int, 0)
			_ = slice[1]
		}, "认证接口", "v1", "认证")

		// group1.SetStatic("/files", ".", "静态文件服务接口", "文件", "下载")
	}

	// 路由组2
	{
		group2 := engine.Group("/v2")
		group2.UseMiddlewares(
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v2 1 start")
					handle(ctx)
					fmt.Println("/v2 1 end")
				}
			},
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v2 2 start")
					handle(ctx)
					fmt.Println("/v2 2 end")
				}
			})

		group2.GET("/b", func(ctx *leego.Context) {
			fmt.Println("*********ready response b*********")
			fmt.Println("/v2/b ok")
			_ = ctx.ResponsePlain(http.StatusOK, "this is a test")
		}, "验证基本接口2", "v2", "字符串")
	}

	// 路由组3
	{
		group3 := engine.Group("/login")
		group3.UseMiddlewares(
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v2/login 1 start")
					handle(ctx)
					fmt.Println("/v2/login 1 end")
				}
			},
		)

		group3.POST("/:name", func(ctx *leego.Context) {
			fmt.Println("*********ready response login*********")
			fmt.Printf("%s is logged in\n", ctx.GetPathValue("name"))

			name := ctx.GetPathValue("name")
			signature, err := middlewareJWTSigner.Sign(&struct {
				*jwt.StandardClaims
				Email string `json:"email"`
				Name  string `json:"name"`
			}{
				StandardClaims: &jwt.StandardClaims{
					IssuedAt:  time.Now().Unix(),
					Issuer:    "ToolHub",
					ExpiresAt: time.Now().Add(time.Hour * 24 * 7).Unix(),
					Subject:   "tester",
					Audiences: []string{"ToolHub"},
					Id:        "tester",
				},
				Email: "tester@toolhub.com",
				Name:  name,
			})
			if err != nil {
				fmt.Println("sign error: ", err)
			}

			ctx.SetResponseHeader("Authorization", "Bearer "+string(signature))
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
		}, "登录接口", "v2", "登录")

		group3.GET("/sse", func(ctx *leego.Context) {
			fmt.Println("*********ready response sse*********")
			_ = ctx.ResponseSSEvent(func(ctx context.Context) <-chan *sse.Message {
				events := make(chan *sse.Message, 1)
				go func() {
					for i := 0; i < 10; i++ {
						data, _ := json.Marshal(&leego.JSON{"code": "00000", "desc": "ok", "size": strconv.Itoa(i), "time": time.Now().Format(time.DateTime)})
						events <- &sse.Message{
							Event: "test_msg",
							Id:    "tid1234567890",
							Data:  data,
						}
						time.Sleep(1 * time.Second)
					}

					close(events)
				}()

				return events
			})
		}, "SSE接口", "v2", "SSE", "长连接")

		group3.GET("/files/download/*filepath", func(ctx *leego.Context) {
			fmt.Println("*********ready download file*********")
			ctx.ResponseFileAttachment(ctx.GetPathValue("filepath"))
		}, "下载文件", "v2", "文件", "下载")

		group3.GET("/files/show/*filepath", func(ctx *leego.Context) {
			fmt.Println("*********ready response file content*********")
			ctx.ResponseFileContent(ctx.GetPathValue("filepath"))
		}, "显示文件", "v2", "文件", "前端显示")

		//	路由组4，继承于路由组3
		// group4 := group3.Group("/admin")
		// group4.SetStatic("views", "../../admin/views", "后台管理静态文件服务", "admin")
	}

	// 启动服务
	go func() {
		_ = engine.Run(":8080")
	}()

	if err := engine.WaitSignalAndShutdown(5*time.Second, nil); err != nil {
		log.Printf("Faild to shutdown: %v", err)
	}
}
