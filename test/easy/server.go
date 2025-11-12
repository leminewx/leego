package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"gitee.com/leminewx/gokit/ggid"
	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/response/sse"
)

func main() {
	engine := leego.NewEngine()
	engine.UseMiddlewareFuncs(
		middleware.Log(),
		middleware.Recover(nil),
	)

	group := engine.Group("/v1")
	group.UseMiddlewareFuncs(
		func(next leego.HandleFunc) leego.HandleFunc {
			fmt.Println("init middleware 1")
			return func(ctx *leego.Context) {
				fmt.Println("get in group v1")
				next(ctx)
				fmt.Println("get out group v1")
			}
		},
		func(next leego.HandleFunc) leego.HandleFunc {
			fmt.Println("init middleware 2")
			return func(ctx *leego.Context) {
				fmt.Println("get in group v2")
				next(ctx)
				fmt.Println("get out group v2")
			}
		},
	)

	group.GET("/get", func(ctx *leego.Context) {
		fmt.Println("request is dealing...")
		time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"hello": "get"})
	})

	group.POST("/post", func(ctx *leego.Context) {
		fmt.Println("request is dealing...")
		time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"hello": "post"})
	})

	group.GET("/sse", func(ctx *leego.Context) {
		fmt.Println("request is dealing...")
		_ = ctx.ResponseSSE(func(ctx context.Context) <-chan *sse.Message {
			messages := make(chan *sse.Message)
			go func() {
				for range 10 {
					time.Sleep(1 * time.Second)
					select {
					case <-ctx.Done():
						break
					case messages <- &sse.Message{Id: ggid.New().String(), Data: []byte(time.Now().String())}:
					}
				}
				close(messages)
			}()

			return messages
		})
	})

	panic(engine.Run(":8080"))
}
