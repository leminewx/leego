// Package main 演示 leego 的 WebSocket 能力。
//
// 该示例使用 websocket.Manager 托管所有连接，并演示：
//   - 自定义升级配置（超时、消息上限、来源校验、子协议协商）；
//   - 连接建立后的欢迎消息与心跳保活；
//   - 客户端消息协议（identify / ping / broadcast / private）；
//   - 服务端定时广播与在线连接统计；
//   - 优雅关闭。
//
// 客户端可发送的 JSON 消息示例：
//
//	{"type": "identify", "data": {"user_id": "u1001", "name": "张三"}}
//	{"type": "ping"}
//	{"type": "broadcast", "data": "hello everyone"}
//	{"type": "private", "to": "<client_id>", "data": "hi"}
//
// 服务端下发的消息示例：
//
//	{"type": "connected", "timestamp": 1700000000, "data": {"client_id": "..."}}
//	{"type": "broadcast", "timestamp": 1700000000, "data": "..."}
//	{"type": "tick", "timestamp": 1700000000, "data": {"online": 3}}
package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/harness/assetpath"
	"gitee.com/leminewx/leego/middlewares"
	"gitee.com/leminewx/leego/response/websocket"
	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

// msgTypeTick 为服务端定时广播使用的自定义消息类型。
const msgTypeTick websocket.MessageType = "tick"

func main() {
	engine := leego.NewEngine(leego.WithEngineName("leego-websocket-example"))
	engine.UseMiddlewares(
		middlewares.NewLogger().AdaptToMiddleware(),
		middlewares.NewRecovery().AdaptToMiddleware(),
	)

	manager, err := newManager()
	if err != nil {
		log.Fatalf("创建 WebSocket 管理器失败: %v", err)
	}

	// 心跳间隔：服务端每 30 秒发送一次 PING 控制帧。
	manager.SetPingInterval(30 * time.Second)

	// WebSocket 升级路由：Serve 会阻塞到连接结束，必须直接作为处理函数调用。
	engine.GET("/ws", func(ctx *leego.Context) {
		if err := manager.Serve(ctx.Response, ctx.Request); err != nil {
			log.Printf("WebSocket 连接结束: %v", err)
		}
	}, "WebSocket 连接入口", "WebSocket", "长连接")

	// 调试页面：浏览器打开根路径即可连接 /ws（页面位于 harness/testdata）。
	engine.GET("/", func(ctx *leego.Context) {
		ctx.ResponseFile(filepath.Join(assetpath.GetDir(), "ws-client.html"))
	}, "WebSocket 调试页面", "WebSocket", "调试")

	// 连接统计：返回在线数量与每个连接的快照（客户端 ID、用户 ID、连接时间等）。
	engine.GET("/ws/stats", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{
			"code":             "00000",
			"desc":             "ok",
			"connection_count": manager.CountConnections(),
			"connections":      manager.GetClients(),
		})
	}, "WebSocket 连接统计", "WebSocket", "运维")

	// 后台定时广播：把在线连接数推送给所有客户端。
	var broadcastCtx, stopBroadcast = context.WithCancel(context.Background())
	defer stopBroadcast()

	go broadcastLoop(broadcastCtx, manager)

	go func() {
		if err := engine.Run(":8080"); err != nil {
			log.Printf("HTTP 服务退出: %v", err)
		}
	}()

	// 关闭顺序：先停止广播，再关闭管理器（通知客户端并关闭连接），最后关闭 HTTP 服务。
	if err := engine.WaitSignalAndShutdown(5*time.Second, func() error {
		stopBroadcast()
		return manager.Close()
	}); err != nil {
		log.Printf("优雅关闭失败: %v", err)
	}
}

// newManager 创建带自定义配置的 WebSocket 管理器。
func newManager() (*websocket.Manager, error) {
	config := upgrader.NewDefaultConfig()
	config.ReadTimeout = 60 * time.Second
	config.WriteTimeout = 10 * time.Second
	config.MaxReadLimit = 1 << 20 // 单条消息上限 1 MiB
	config.Subprotocols = []string{"leego.v1"}

	// 来源校验：生产环境务必改为校验真实域名白名单，否则存在跨站 WebSocket 劫持风险。
	config.OriginCheckFunc = func(req *http.Request) bool {
		origin := req.Header.Get("Origin")
		if origin == "" {
			// 非浏览器客户端通常不带 Origin，这里直接放行。
			return true
		}

		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}

		// 示例允许本机任意端口访问。
		switch parsed.Hostname() {
		case "127.0.0.1", "localhost", "::1":
			return true
		default:
			return false
		}
	}

	return websocket.NewManager(config), nil
}

// broadcastLoop 定时向全部客户端广播在线连接数。
func broadcastLoop(ctx context.Context, manager *websocket.Manager) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-manager.GetDoneChan():
			return
		case <-ticker.C:
			payload := map[string]any{
				"online": manager.CountConnections(),
				"time":   time.Now().Format(time.DateTime),
			}

			if err := manager.Broadcast(msgTypeTick, payload); err != nil {
				log.Printf("WebSocket 广播失败: %v", err)
			}
		}
	}
}
