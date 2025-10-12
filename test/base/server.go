package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gitee.com/leminewx/leego/headers"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/response/sse"
)

func main() {
	// 创建服务
	engine := leego.NewEngine("test").
		WithURLBlacklist("/.well-known/appspecific/com.chrome.devtools.json")

	// 中间件
	engine.WithMiddlewares(
		middleware.Log(nil),
		middleware.Limit(1),
		middleware.Recover(nil),
		middleware.Cors(cors.NewDefault()),
		middleware.GenerateRequestId(headers.X_REQUEST_ID, func(req *http.Request) string {
			return fmt.Sprintf("rid_%d", time.Now().UnixNano())
		}),
	)

	// 路由组1
	{
		group1 := engine.Group("/v1").WithMiddlewares(
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v1 1 start")
					handle(ctx)
					fmt.Println("/v1 1 end")
				}
			},
			func(handle leego.HandleFunc) leego.HandleFunc {
				return func(ctx *leego.Context) {
					fmt.Println("/v1 2 start")
					handle(ctx)
					fmt.Println("/v1 2 end")
				}
			})

		group1.GET("/a", func(ctx *leego.Context) {
			fmt.Printf("/v1/a ok  -  request_id: %s\n", ctx.GetRequestHeader(headers.X_REQUEST_ID))
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
		}, "验证基本接口1", "v1", "JSON")

		go func() {
			time.Sleep(20 * time.Second)
			engine.DisenableRoute(http.MethodGet, "/v1/a")
			time.Sleep(30 * time.Second)
			engine.EnableRoute(http.MethodGet, "/v1/a")
		}()

		group1.GET("/metadata", func(ctx *leego.Context) {
			collectors := engine.GetAllRouteMonitors()
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok", "data": collectors})
		}, "获取所有路由监控数据", "v1", "监控数据")

		if err := group1.SetStatics("/files", ".", false, "静态文件服务接口", "v1", "文件", "下载", "前端显示", "静态文件服务"); err != nil {
			panic(err)
		}
	}

	// 路由组2
	{
		group2 := engine.Group("/v2").WithMiddlewares(
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
			_ = ctx.ResponseString(http.StatusOK, "this is a test")
		}, "验证基本接口2", "v2", "字符串")

		// 路由组3，继承于路由组2
		{
			group3 := group2.Group("/login").WithMiddlewares(
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
				_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
			}, "登录接口", "v2", "登录")

			group3.GET("/sse", func(ctx *leego.Context) {
				fmt.Println("*********ready response sse*********")
				_ = ctx.ResponseSSE(func(ctx context.Context) <-chan *sse.Message {
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
				if err := ctx.ResponseFile(ctx.GetPathValue("filepath")); err != nil {
					fmt.Println("download file error: ", err)
				}
			}, "下载文件", "v2", "文件", "下载")

			group3.GET("/files/show/*filepath", func(ctx *leego.Context) {
				fmt.Println("*********ready response file content*********")
				if err := ctx.ResponseContent(ctx.GetPathValue("filepath")); err != nil {
					ctx.ResponseError(http.StatusInternalServerError, err.Error())
				}
			}, "显示文件", "v2", "文件", "前端显示")
		}
	}

	// 启动服务
	go func() {
		_ = engine.ListenAndServe(&leego.Options{
			Addr: ":10000",
		})
	}()

	engine.Shutdown(5 * time.Second)
}
