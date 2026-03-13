package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"sync"
	"time"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/response/sse"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/cors"
)

func main() {
	// 创建服务
	engine := leego.NewEngine()

	// 中间件
	engine.UseMiddlewares(
		middleware.Log(),
		middleware.Recover(),
		cors.NewDefaultCORS().GetMiddleware(),
		func(next leego.HandleFunc) leego.HandleFunc {
			return func(ctx *leego.Context) {
				ctx.Response.Header().Set(headers.X_REQUEST_ID, "xxxx-xxxx-xxxx-xxxx-xxxx")
				next(ctx)
			}
		},
	)

	sseManager := sse.NewManager()
	sseManager.SetHeartbeatPeriod(10 * time.Second)
	defer func() { _ = sseManager.Close() }()

	var wg sync.WaitGroup
	wg.Go(func() {
		for err := range sseManager.GetErrorChan() {
			log.Printf("SSE manager error: %v", err)
		}
	})

	wg.Go(func() {
		for {
			if sseManager.IsClosed() {
				return
			}

			time.Sleep(1 * time.Second)
			_ = sseManager.Broadcast(&sse.Message{
				Id:    "xxxx-xxxx-xxxx-xxxx-xxxx",
				Event: "manager_message",
				Data:  []byte("hello, this is broadcast message, welcome to leego"),
			})
		}
	})

	engine.GET("/sse/v1/:username", func(ctx *leego.Context) {
		username := ctx.GetPathValue("username")
		client, err := sseManager.Register(ctx.Response, ctx.Request, username)
		if err != nil {
			log.Println(err)
		}

		for i := range 100 {
			data, _ := json.Marshal(&leego.JSON{"username": username, "desc": "hello", "time": time.Now().Format(time.DateTime)})
			_ = sseManager.SendTo(client.Id, &sse.Message{
				Id:    strconv.Itoa(i),
				Event: "person_message",
				Data:  data,
			})

			data, _ = json.Marshal(&leego.JSON{"desc": "hello, my name is " + username, "time": time.Now().Format(time.DateTime)})
			_ = sseManager.Broadcast(&sse.Message{
				Id:    strconv.Itoa(i),
				Event: "broadcast_message",
				Data:  data,
			})

			time.Sleep(1 * time.Second)
		}
	}, "SSE综合管理", "SSE")

	engine.GET("/sse/v2/:username", func(ctx *leego.Context) {
		_ = ctx.ResponseSSEvent(func(ctx context.Context) <-chan *sse.Message {
			events := make(chan *sse.Message, 1)
			go func() {
				for i := 0; i < 100; i++ {
					data, _ := json.Marshal(&leego.JSON{"code": "00000", "desc": "ok", "size": strconv.Itoa(i), "time": time.Now().Format(time.DateTime)})
					events <- &sse.Message{
						Id:    "tid1234567890",
						Event: "test_msg",
						Data:  data,
					}
					time.Sleep(1 * time.Second)
				}

				close(events)
			}()

			return events
		})
	}, "SSE接口", "v2", "SSE", "长连接")

	// 启动服务
	wg.Go(func() {
		_ = engine.Run(":10000")
	})

	_ = engine.WaitSignalAndShutdown(5*time.Second, func() error {
		return sseManager.Close()
	})

	wg.Wait()
}
