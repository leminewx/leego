// Package main 演示服务端事件流（SSE）的两种用法。
//
// 用法一：由 sse.Manager 统一管理连接，支持按客户端定向推送与全局广播，
// 适合聊天室、行情推送、任务进度等“多连接 + 服务端主动推送”的场景。
//
// 用法二：直接调用 Context.ResponseSSE，由回调函数自行生产事件，
// 连接生命周期与请求绑定，适合简单的一次性数据流。
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/response/sse"
)

// ssePayload 是示例中推送的数据结构。
type ssePayload struct {
	Code string `json:"code"`
	Desc string `json:"desc"`
	Time string `json:"time"`
}

func main() {
	engine := leego.NewEngine(leego.UseEngineName("leego-sse-example"))
	engine.UseMiddlewares(
		middleware.Log(),
		middleware.Recover(),
		cors.NewDefaultCORS().GetMiddleware(),
	)

	manager := sse.NewManager()
	manager.SetHeartbeatPeriod(10 * time.Second)
	defer func() { _ = manager.Close() }()

	var waitGroup sync.WaitGroup

	// 后台消费错误通道：不消费会导致错误通道写满后错误被丢弃。
	waitGroup.Go(func() {
		for err := range manager.GetErrorChan() {
			log.Printf("SSE 管理器错误: %v", err)
		}
	})

	// 后台定时广播：每隔 5 秒向所有在线客户端推送一次。
	waitGroup.Go(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-manager.Done():
				return
			case <-ticker.C:
				payload, _ := json.Marshal(ssePayload{
					Code: "00000",
					Desc: "全局广播",
					Time: time.Now().Format(time.DateTime),
				})

				if err := manager.Broadcast(&sse.Message{Event: "broadcast", Data: payload}); err != nil {
					log.Printf("SSE 广播失败: %v", err)
				}
			}
		}
	})

	// 用法一：由管理器托管连接。Serve 会阻塞到客户端断开。
	engine.GET("/sse/v1/:username", func(ctx *leego.Context) {
		username := ctx.GetPathValue("username")
		if username == "" {
			ctx.ResponseError(http.StatusBadRequest, "缺少用户名")
			return
		}

		// Serve 内部会先写入响应头与 connect 事件，然后阻塞读循环直到客户端断开。
		// 该调用必须同步执行：提前返回会释放响应写入器，导致连接被意外关闭。
		if err := manager.Serve(ctx.Response, ctx.Request, username); err != nil {
			log.Printf("SSE 连接结束: %v", err)
		}
	}, "SSE 综合管理", "SSE", "长连接")

	// 用法二：回调式事件流。事件通道关闭后连接自动结束。
	engine.GET("/sse/v2/:username", func(ctx *leego.Context) {
		_ = ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
			events := make(chan *sse.Message, 1)

			go func() {
				defer close(events)

				for index := 0; index < 100; index++ {
					payload, _ := json.Marshal(ssePayload{
						Code: "00000",
						Desc: "ok",
						Time: time.Now().Format(time.DateTime),
					})

					select {
					case <-requestCtx.Done():
						return
					case events <- &sse.Message{Event: "tick", Id: strconv.Itoa(index), Data: payload}:
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
	}, "SSE 回调式事件流", "SSE", "长连接")

	// 在线客户端列表：便于前端调试与运维排查。
	engine.GET("/sse/clients", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{
			"code":    "00000",
			"desc":    "ok",
			"clients": manager.GetAllClientIds(),
			"count":   manager.CountClients(),
		})
	}, "查看在线 SSE 客户端", "SSE", "运维")

	go func() {
		if err := engine.Run(":10000"); err != nil {
			log.Printf("HTTP 服务退出: %v", err)
		}
	}()

	if err := engine.WaitSignalAndShutdown(5*time.Second, func() error {
		return manager.Close()
	}); err != nil {
		log.Printf("优雅关闭失败: %v", err)
	}

	waitGroup.Wait()
}
