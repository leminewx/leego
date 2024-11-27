package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/cors"
)

func main() {
	engine := leego.NewEngine()
	engine.WithMiddlewares(
		leego.Cors(cors.NewCORS()),
		leego.Log(),
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
		})

		group1.WithStatic("/files", "/root/project/gitee.com/leminewx/leego")
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
			fmt.Println("/v2/b ok")
			ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
		})

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
				fmt.Printf("/v2/login/%s ok\n", ctx.GetPathValue("name"))
				ctx.ResponseJSON(http.StatusOK, leego.Json{"code": "00000", "desc": "ok"})
			})

			group3.GET("/sse", func(ctx *leego.Context) {
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
			})

			group3.GET("/files/download/*", func(ctx *leego.Context) {
				if err := ctx.ResponseFile(strings.Split(ctx.GetURL().String(), "/files/download/")[1]); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			})

			group3.GET("/files/show/*/123", func(ctx *leego.Context) {
				if err := ctx.ResponseFileContent(strings.Split(ctx.GetURL().String(), "/files/show/")[1]); err != nil {
					ctx.ResponseFail(http.StatusInternalServerError, err.Error())
				}
			})

			group3.WithStatic("/files", "/root/project/gitee.com/leminewx/leego")
		}
	}

	engine.ListenAndServe(&leego.Options{
		Addr: ":10000",
	})
}
