package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"sync"
	"time"

	"gitee.com/leminewx/gokit/ggid"
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
				ctx.Response.Header().Set(headers.X_REQUEST_ID, ggid.New().String())
				next(ctx)
			}
		},
	)

	sseManager := sse.NewManager()
	defer func() { _ = sseManager.Close() }()

	var wg sync.WaitGroup
	wg.Go(func() {
		for err := range sseManager.GetErrorChan() {
			log.Printf("SSE manager error: %v", err)
		}
	})

	wg.Go(func() {
		id := ggid.New().GetShort()
		for {
			if sseManager.IsClosed() {
				return
			}

			time.Sleep(1 * time.Second)
			_ = sseManager.Broadcast(&sse.Message{
				Event: "manager_message",
				Id:    id,
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

		id := ggid.New().GetShort()
		for range 10 {
			data, _ := json.Marshal(&leego.JSON{"username": username, "desc": "hello", "time": time.Now().Format(time.DateTime)})
			_ = sseManager.SendTo(client.Id, &sse.Message{
				Event: "person_message",
				Id:    id,
				Data:  data,
			})

			data, _ = json.Marshal(&leego.JSON{"desc": "hello, my name is " + username, "time": time.Now().Format(time.DateTime)})
			_ = sseManager.Broadcast(&sse.Message{
				Event: "broadcast_message",
				Id:    id,
				Data:  data,
			})

			time.Sleep(1 * time.Second)
		}
	}, "SSE综合管理", "SSE")

	engine.GET("/sse/v2/:username", func(ctx *leego.Context) {
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

	// 启动服务
	wg.Go(func() {
		_ = engine.Run(":10000")
	})

	if err := engine.Shutdown(5 * time.Second); err != nil {
		log.Printf("Faild to shutdown: %v", err)
	}

	wg.Wait()
}
