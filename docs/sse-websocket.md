# SSE 与 WebSocket

长连接有两种典型形态：服务端单向推送（SSE）与全双工（WebSocket）。Leego 为两者
都提供了完整实现，并且都不依赖第三方库。

---

## SSE

SSE（Server-Sent Events）基于普通 HTTP 响应，服务端以 `text/event-stream` 持续
写入事件，浏览器通过 `EventSource` 消费。优势是协议简单、自动重连、天然穿透
代理；限制是**单向**（服务端 → 客户端）与文本载荷。

### 消息格式

```go
type Message struct {
	Id    string
	Event string
	Retry int64
	Data  []byte
}
```

序列化规则（`Message.Write`）：

```
id: <Id>\n          // Id 非空时写入
event: <Event>\n    // Event 非空时写入
retry: <Retry>\n    // Retry > 0 时写入
data: <行1>\n
data: <行2>\n       // Data 内每个换行都会拆成独立的 data 行
\n                  // 事件结束空行
```

- 完全空的消息（无 Id、Event、Retry、Data）不写任何字节；
- 只设置 `Id` 时仍会写 `data: ` 空行，保证事件完整；
- 序列化结果被缓存，重复写同一消息不会重复编码；`Reset()` 可重建消息以复用对象；
- 内置构造函数：`NewConnectedMessage`、`NewHeartbeatMessage`、
  `NewClosedMessage`、`NewShutdownMessage`、`NewMessageWithEvent`。

### 方式一：单连接回调

```go
engine.GET("/sse/once", func(ctx *leego.Context) {
	_ = ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
		events := make(chan *sse.Message, 1)

		go func() {
			defer close(events)
			for index := 0; index < 10; index++ {
				select {
				case <-requestCtx.Done():
					return
				case events <- &sse.Message{Event: "tick", Data: []byte(`{"ok":true}`)}:
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
})
```

`ctx.ResponseSSE` 等价于 `sse.ServeEventStream(ctx.Response, ctx.Request, event, 8)`：

1. 写入响应头（`text/event-stream`、`no-cache`、`keep-alive`、`X-Accel-Buffering: no`）；
2. 调用回调拿到事件通道；
3. 单写者循环：从通道读取消息 → 写响应 → `Flush`；
4. 通道关闭、客户端断开或请求取消时结束。

> `ResponseSSE` 必须同步阻塞到连接结束。处理器提前返回会把响应写入器归还对象池，
> 连接会被意外关闭。

### 方式二：管理器托管多连接

```go
manager := sse.NewManager()
manager.SetMessageCacheNumber(16)                 // 每个连接的消息缓冲
manager.SetWriteTimeout(300 * time.Millisecond)   // 投递超时
manager.SetHeartbeatPeriod(15 * time.Second)      // 心跳周期
defer manager.Close()

// 必须消费错误通道，否则缓冲满后错误会被丢弃
go func() {
	for err := range manager.GetErrorChan() {
		log.Printf("SSE: %v", err)
	}
}()

engine.GET("/sse/:client", func(ctx *leego.Context) {
	// 同步阻塞：内部完成注册、写响应头、connect 事件、写循环
	_ = manager.Serve(ctx.Response, ctx.Request, ctx.GetPathValue("client"))
})
```

管理器能力：

| 方法 | 说明 |
| --- | --- |
| `Serve(resp, req, clientId)` | 注册并服务一个连接，阻塞到断开 |
| `SendTo(clientId, msg)` | 定向推送 |
| `Broadcast(msg)` | 广播给全部连接 |
| `BroadcastTo(clientIds, msg)` | 广播给指定集合 |
| `Unregister(clientId)` | 主动注销 |
| `CountClients()` / `GetAllClientIds()` | 统计与枚举（返回按字典序排序） |
| `IsClientConnected(id)` | 判断连接是否在线 |
| `GetErrorChan()` | 投递失败等异步错误 |
| `Done()` / `IsClosed()` / `Close()` | 关闭通知与关闭 |

默认值：

| 项 | 默认 |
| --- | --- |
| 每连接消息缓冲 | 8 |
| 投递超时 | 300 ms |
| 心跳周期 | 30 s |
| 错误通道缓冲 | 16 |
| 广播并发上限 | 64 |

行为细节：

- 每个连接是单写者模型：只有连接自身的写循环会写响应，避免并发写导致的帧错乱；
- 投递时若缓冲已满，等待 `WriteTimeout` 后返回 `ErrMessageChannelIsFull`；
- 投递失败会注销该连接，并把错误写入错误通道；
- `Broadcast` / `BroadcastTo` / `Close` 的逐连接通知由信号量限制在 64 个并发协程，
  慢客户端不会放大 goroutine 数量；每个连接仍由自身写循环串行写入；
- `Close()` 会先向所有连接投递 `shutdown` 消息，并等待最多 50ms 让关闭消息刷出，
  然后关闭连接与错误通道；
- `Close()` 幂等，且不会关闭连接的 `messageChan`（投递方可能仍持有引用）。

### 与引擎超时共存

SSE 连接通常持续数分钟以上，而引擎默认 `WriteTimeout` 为 60 秒，到时
`net/http` 会直接切断连接。启用 SSE / WebSocket 的服务必须显式关闭该超时：

```go
engine := leego.NewEngine(leego.WithWriteTimeout(0))
```

`WithServerTimeouts` 会忽略非正数，无法用它关闭写入超时，因此单独提供了
`WithWriteTimeout`。

### 连接生命周期

```
Manager.Serve
  ├─ 创建 Client（缓冲、超时、心跳）
  ├─ 注册到 sync.Map（重复 id 返回 ErrClientAlreadyExists）
  ├─ 写响应头 + connect 事件
  ├─ 写循环：心跳 ticker + 消息通道 + closeChan + 请求取消
  └─ 退出：注销、关闭客户端
```

---

## WebSocket

`response/websocket` 实现了 RFC 6455 的握手、帧解析、掩码校验、控制帧与关闭码处理；
`upgrader` 子包负责握手与配置。当前版本**不支持分片消息与 `permessage-deflate`**：
收到 `FIN=0` 的数据帧或 `OP_CODE_CONTINUE` 会先发 1003（unsupported data）再断开，
任一 `RSV` 位非 0 则按协议错误直接断开；写出方向始终 `FIN=1`、`RSV=0`。
因此单条消息必须一次写完，超大载荷请自行拆成多条独立消息。

### 升级配置

```go
config := upgrader.NewDefaultConfig()
config.ReadTimeout = 90 * time.Second
config.WriteTimeout = 10 * time.Second
config.MaxReadLimit = 1 << 20
config.Subprotocols = []string{"chat.v1"}
config.OriginCheckFunc = func(req *http.Request) bool {
	return req.Header.Get("Origin") == "https://app.example.com"
}

upgraderInstance := upgrader.New(config)
connection, err := upgraderInstance.Upgrade(ctx.Request, ctx.Response, nil)
```

默认配置：

| 项 | 默认 |
| --- | --- |
| 读超时 | 60 s |
| 写超时 | 10 s |
| 最大读字节 | 16 MiB |
| 来源校验 | 默认同源（Origin 缺失放行，跨站拒绝） |
| 子协议 | 空 |

> 默认同源校验可以拦截浏览器发起的跨站 WebSocket 劫持（CSWSH）；
> 部署在独立域名下时，应显式配置 `OriginCheckFunc` 白名单。

握手校验包括：方法为 `GET`、`Upgrade: websocket`、`Connection` 包含 `upgrade`、
`Sec-WebSocket-Version: 13`、`Sec-WebSocket-Key` 为 16 字节 Base64。任一不满足
返回错误，不会写出 101 响应。

### 连接读写

```go
for {
	opCode, payload, err := connection.Read()
	if err != nil {
		var closeErr *upgrader.CloseError
		if errors.As(err, &closeErr) {
			// 对端关闭：Code / Text
		}
		return
	}

	switch opCode {
	case upgrader.OP_CODE_TEXT:
		_ = connection.Write(upgrader.OP_CODE_TEXT, payload)
	case upgrader.OP_CODE_PING:
		_ = connection.WriteControl(upgrader.OP_CODE_PONG, payload)
	}
}
```

约束与保护：

- 服务端要求客户端帧**必须掩码**，客户端要求服务端帧**不能掩码**；
- 控制帧（PING / PONG / CLOSE）负载不得超过 125 字节；
- 文本帧必须是合法 UTF-8；
- 超过 `MaxReadLimit` 的帧直接拒绝；
- 写操作由互斥锁串行化；
- `Close()` 幂等；`CloseWithCode(code, text)` 发送关闭帧后关闭底层连接；
- `ErrConnectionIsClosed` / `ErrWriteIsClosed` 用于区分「已关闭」的读写错误。

### 管理器

```go
manager := websocket.NewManager(upgrader.NewDefaultConfig())
manager.SetPingInterval(30 * time.Second)

go manager.Start(context.Background()) // 阻塞到 ctx 取消或 manager.Close()
defer manager.Close()

engine.GET("/ws", func(ctx *leego.Context) {
	if !ctx.IsWebsocket() {
		ctx.ResponseError(http.StatusBadRequest, "expect websocket")
		return
	}
	// 阻塞到连接结束；内部启动读/写协程与欢迎消息
	_ = manager.Serve(ctx.Response, ctx.Request)
})
```

管理器能力：

| 方法 | 说明 |
| --- | --- |
| `Serve(resp, req)` | 升级连接、注册、写欢迎消息、启动读写协程并阻塞 |
| `Start(ctx)` | 启动心跳循环，阻塞到 ctx 取消或管理器关闭 |
| `Broadcast(type, data)` | 广播消息 |
| `SendToClient(id, type, data)` | 定向发送 |
| `GetClients()` / `GetClient(id)` | 客户端快照 |
| `CountConnections()` | 在线连接数 |
| `CloseClient(id)` | 关闭指定连接 |
| `Done()` / `IsClosed()` / `Close()` | 关闭通知与关闭 |

消息类型由 `MessageType` 字符串标识，`NewWelcomeMessage`、
`NewPingMessage`、`NewPongMessage`、`NewSimpleMessage` 提供常用构造函数。

客户端元信息：

```go
client.SetUserId("u-1")
client.SetMetadata("room", "lobby")
snapshot := client.GetSnapshot() // Id / UserId / Metadata 等
```

---

## 选型建议

| 场景 | 建议 |
| --- | --- |
| 行情、通知、任务进度（单向） | SSE：协议简单、自动重连、易穿透代理 |
| 聊天、协同编辑、双向指令 | WebSocket：全双工、二进制支持 |
| 只需要「服务端推一次」 | 普通 JSON + 轮询更简单 |

---

## 常见问题

**Q：SSE 消息延迟很高，像是攒了一批才到？**
检查反向代理是否开启了响应缓冲。框架已设置 `X-Accel-Buffering: no`，但部分
代理需要额外配置；同时确认每次写入后调用了 `Flush`（框架内部已处理）。

**Q：SSE 的 `SendTo` 返回错误，但客户端看起来还在线？**
投递失败（缓冲满）会触发注销。错误同时写入 `GetErrorChan()`，如果不消费错误
通道，排查时看不到原因。

**Q：WebSocket 升级总是 400？**
确认请求头完整：`Connection` 必须包含 `upgrade`（大小写不敏感），
`Sec-WebSocket-Version` 必须为 `13`，`Sec-WebSocket-Key` 必须是 16 字节的
Base64；另外确认路由注册的是 `GET`。

**Q：能在 SSE / WebSocket 处理器里 `defer` 释放资源吗？**
可以，但释放时机是连接结束（处理器返回）时。不要启动后台协程后立即返回处理器，
否则连接会被关闭。
