package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/interceptor"
	_ "gitee.com/leminewx/leego/logo"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/response/websocket"
)

func main() {
	engine := leego.NewEngine("test")
	engine.WithInterceptors(
		interceptor.Recover(),
		interceptor.Log(),
		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
				response := response.NewResponseWriter(resp)
				handler.ServeHTTP(response, req)

				if response.GetStatusCode() == http.StatusNotFound {
					fmt.Println("Oh my god, not found")
				}
			})
		},
	)
	engine.WithMiddlewares(
		middleware.Cors(cors.New()),
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
			fmt.Println("/v1/a ok")
			ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
		}, "通用API1")

		group1.GET("/ws", func(ctx *leego.Context) {
			ctx.ResponseUpgrade(func(conn *websocket.Conn) {
				defer conn.Close()

				// 简单消息处理循环
				var idx int
				for {
					// 读取客户端消息（简化版，仅处理小文本消息）
					idx++
					frame, err := conn.Read()
					if err != nil {
						log.Println("Read error:", err)
						return
					}

					// 处理消息（示例：原样返回）
					log.Printf("Received: %s", frame.Payload)
					if idx == 10 {
						conn.SendText("this is last message!!!")
						return
					}

					conn.SendText(strconv.Itoa(idx))
				}
			})
		}, "websocket测试")

		group1.WithStatics("/files", "D:\\Projects\\Golang\\leego\\test", "下载文件1")
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
			ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
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

			group3.POST("/{name}", func(ctx *leego.Context) {
				fmt.Println("*********ready response login*********")
				fmt.Printf("/v2/login/%s ok\n", ctx.GetPathValue("name"))
				ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
			}, "登录系统", "用户管理")

			group3.GET("/sse", func(ctx *leego.Context) {
				fmt.Println("*********ready response sse*********")
				ctx.ResponseSSE(func() <-chan []byte {
					data := make(chan []byte)
					go func() {
						for i := 0; i < 10; i++ {
							data <- []byte(fmt.Sprintf("hello, now is %v", time.Now()))
							time.Sleep(1 * time.Second)
						}

						close(data)
					}()

					return data
				})
			}, "获取系统信息", "用户管理")

			// group3.WithStatics("/files", "D:\\Projects\\Golang\\leego\\logo", "下载文件2")

			group3.GET("/files/download/*", func(ctx *leego.Context) {
				fmt.Println("*********ready download file*********")
				if err := ctx.ResponseFile("APIS.csv"); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			}, "下载文件", "文件管理")

			group3.GET("/files/show/*", func(ctx *leego.Context) {
				fmt.Println("*********ready response file content*********")
				if err := ctx.ResponseFileContent("test.go"); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			}, "显示文件", "文件管理")
		}
	}

	engine.ListenAndServe(&leego.Options{
		Addr: ":10000",
	})
}
