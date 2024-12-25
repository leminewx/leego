package main

import (
	"fmt"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	_ "gitee.com/leminewx/leego/logo"
)

func main() {
	// engine := leego.NewEngine("test")
	// engine.WithMiddlewares(
	// 	leego.Cors(cors.NewCORS()),
	// 	leego.AuthFromHubuByHTTP(http.MethodPost, "http://10.254.249.225:12345/api/auth"),
	// 	leego.Log(),
	// )

	engine := leego.NewDefaultEngine("test")

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
			}, "登录系统")

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
			}, "获取系统信息")

			// group3.WithStatics("/files", "D:\\Projects\\Golang\\leego\\logo", "下载文件2")

			group3.GET("/files/download/*", func(ctx *leego.Context) {
				fmt.Println("*********ready download file*********")
				if err := ctx.ResponseFile("APIS.csv"); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			}, "下载文件")

			group3.GET("/files/show/*", func(ctx *leego.Context) {
				fmt.Println("*********ready response file content*********")
				if err := ctx.ResponseFileContent("test.go"); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			}, "显示文件")
		}
	}

	engine.ListenAndServe(&leego.Options{
		Addr: ":10000",
	})
}
