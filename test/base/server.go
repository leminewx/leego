package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gitee.com/leminewx/leego/headers"
	"net/http"
	"strconv"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/response/sse"
)

func main() {
	engine := leego.NewEngine("test").
		WithURLBlacklist("/.well-known/appspecific/com.chrome.devtools.json")

	engine.WithMiddlewares(
		middleware.Log(),
		middleware.Limit(1),
		middleware.Recover(),
		middleware.Cors(cors.New()),
		middleware.GenerateXRequestId(func(req *http.Request) string {
			return fmt.Sprintf("rid_%d", time.Now().UnixNano())
		}),
		func(handle leego.HandleFunc) leego.HandleFunc {
			return func(ctx *leego.Context) {
				handle(ctx)
				if ctx.GetStatusCode() == http.StatusNotFound {
					fmt.Println("Oh my god, not found")
				}
			}
		},
	)

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
		}, "通用API1")

		group1.GET("/metadata", func(ctx *leego.Context) {
			collectors := engine.GetCollectors()
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok", "data": collectors})
		})

		if err := group1.SetStatics("/files", ".", false, "下载文件1"); err != nil {
			panic(err)
		}
	}

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
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
		}, "通用API2")

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
				fmt.Printf("/v2/login/%s ok\n", ctx.GetPathValue("name"))
				_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"code": "00000", "desc": "ok"})
			}, "登录系统", "用户管理")

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
			}, "获取系统信息", "用户管理")

			group3.GET("/files/download/*filepath", func(ctx *leego.Context) {
				fmt.Println("*********ready download file*********")
				if err := ctx.ResponseFile("APIS.csv"); err != nil {
					ctx.ResponseError(http.StatusInternalServerError, err.Error())
				}
			}, "下载文件", "文件管理")

			group3.GET("/files/show/*filepath", func(ctx *leego.Context) {
				fmt.Println("*********ready response file content*********")
				if err := ctx.ResponseContent("server.go"); err != nil {
					ctx.ResponseError(http.StatusInternalServerError, err.Error())
				}
			}, "显示文件", "文件管理")
		}
	}

	_ = engine.ListenAndServe(&leego.Options{
		Addr: ":10000",
	})
}
