# Leego

Leego 是一个构建在 Go 标准库 `net/http` 之上的轻量级 Web 框架。它在保持
`http.Handler` 完全兼容的前提下，提供路由分组、中间件编排、上下文封装、
参数绑定、指标采集、内置指标分析后台、SSE / WebSocket、JWT、限流、熔断
与压测工具等能力。

> 版本：`v0.14.0.0`（要求 Go 1.25+）

---

## 特性总览

| 能力 | 说明 |
| --- | --- |
| 零反射路由 | 分段 Trie 实现，注册期构建、运行期冻结，查询无锁 |
| 注册期冻结 | `Prepare()` 一次性折叠全局链与每条路由的中间件链，热路径只做一次指针加载 |
| 标准库兼容 | `Engine` 即 `http.Handler`，可挂载到任意标准库 `http.Server` |
| 统一上下文 | `Context` 覆盖参数、绑定、响应、错误链、模板、文件、SSE，全部来自 `sync.Pool`；响应统一为 `Response*` 命名族 |
| 指标内建 | 每条路由自带采集器，`CollectReport()` 直接产出可序列化的指标报告 |
| 分析后台 | `dashboard` 包内置单页后台，支持过滤、排序、JSON / CSV 导出与健康检查 |
| 可观测中间件 | 访问日志、panic 恢复、CORS、JWT、限流（固定 / 滑动 / 令牌桶）、熔断 |
| 长连接 | SSE（单连接回调式 + 管理器托管多连接）、WebSocket（自实现握手与帧解析） |
| 压测工具 | `stresstest` 包提供并发压测、延迟分位数与报告格式化 |

---

## 目录

- [安装](#安装)
- [快速开始](#快速开始)
- [核心概念](#核心概念)
- [引擎配置](#引擎配置)
- [路由](#路由)
- [Context 上下文](#context-上下文)
- [中间件](#中间件)
- [指标与分析后台](#指标与分析后台)
- [SSE 与 WebSocket](#sse-与-websocket)
- [压力测试](#压力测试)
- [启动与优雅关闭](#启动与优雅关闭)
- [测试与基准](#测试与基准)
- [项目结构](#项目结构)
- [设计取舍与命名规范](#设计取舍与命名规范)
- [文档索引](#文档索引)
- [许可](#许可)

---

## 安装

```bash
go get gitee.com/leminewx/leego
```

依赖：Go 1.25+、`golang.org/x/crypto`、`golang.org/x/net`、`golang.org/x/time`。

---

## 快速开始

```go
package main

import (
	"log"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
)

func main() {
	engine := leego.NewEngine(
		leego.UseEngineName("demo"),
		leego.UseServerTimeouts(5*time.Second, 10*time.Second, 10*time.Second, 60*time.Second),
	)

	engine.UseMiddlewares(middleware.Log(), middleware.Recover())

	engine.GET("/ping", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"message": "pong"})
	}, "健康检查")

	if err := engine.Run(":8080"); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
```

访问 `http://localhost:8080/ping`：

```json
{"message":"pong"}
```

---

## 核心概念

| 类型 | 说明 |
| --- | --- |
| `leego.Engine` | 框架引擎，同时是根路由组；负责配置、路由注册、中间件编排、请求分发与生命周期管理 |
| `leego.Group` | 路由组接口，共享前缀与中间件，支持任意层级嵌套 |
| `leego.Context` | 单请求上下文，封装 `*http.Request` 与 `response.Writer`，提供全部请求/响应能力 |
| `leego.HandleFunc` | 处理器类型：`func(ctx *leego.Context)` |
| `leego.MiddlewareFunc` | 中间件类型：`func(next leego.HandleFunc) leego.HandleFunc` |
| `leego.Handler` | 已注册路由的抽象，支持 `Enable`/`Disable`、指标采集与参数名查询 |
| `leego.JSON` | 便捷 JSON 类型：`map[string]any` |

处理链固定为：**全局中间件 → 父组中间件 → … → 子组中间件 → 路由处理器**。
中间件只包裹「处理逻辑」，404 / 405 与黑名单拒绝同样经过全局链，因此 CORS、
日志、恢复、请求 ID 等能力对未命中请求依然生效。

---

## 引擎配置

所有配置项都有安全默认值，通过选项函数覆盖：

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `UseEngineName(name)` | `"leego"` | 服务名称，出现在日志、指标后台与错误响应中 |
| `UseMultipartMemory(bytes)` | 32 MiB | multipart 表单解析可用的最大内存 |
| `UseMaxBodyBytes(bytes)` | 8 MiB | 单个请求体上限；`<= 0` 表示不限制 |
| `UseStrictJSON(strict)` | `false` | 为 `true` 时 JSON 绑定拒绝未知字段 |
| `UseH2C()` | 关闭 | 启用明文 HTTP/2（配合 `GetHTTPHandler()`） |
| `UseServerTimeouts(readHeader, read, write, idle)` | 10s / 60s / 60s / 120s | 任一参数 `<= 0` 时保留默认值，避免退化为无限等待 |
| `UseTrustedProxies(cidrs...)` | 空 | 仅信任这些网段传来的 `X-Forwarded-For` / `X-Real-IP` |
| `DisableStartupLog()` | 关闭 | 不打印启动期的路由注册信息 |

运行时读取与修改：

```go
config := engine.GetConfig()        // 返回深拷贝，修改不影响引擎
engine.SetName("order-service")     // 等价于替换服务名
engine.SetMaxMultipartMemory(8 << 20)
engine.SetTrustedProxies("10.0.0.0/8")
engine.SetErrorHandler(func(ctx *leego.Context, err error) {
	// 统一错误格式：依次尝试自定义处理器 → 默认 JSON 错误
	_ = ctx.ResponseJSON(http.StatusInternalServerError, leego.JSON{"error": err.Error()})
})
```

> `GetClientIP()` 只在直连对端属于可信代理网段时才解析转发头，避免客户端伪造
> `X-Forwarded-For` 绕过基于 IP 的限流或审计。

---

## 路由

### 注册

```go
engine.GET("/users", listUsers, "查询用户列表", "user")
engine.POST("/users", createUser)
engine.PUT("/users/:id", updateUser)
engine.PATCH("/users/:id", patchUser)
engine.DELETE("/users/:id", deleteUser)
engine.HEAD("/health", health)
engine.OPTIONS("/users", options)
```

`tags` 为可选参数：第一个元素作为路由名称，其余作为业务标签，用于指标归类与
后台展示。

### 参数与通配符

```go
// /users/42
engine.GET("/users/:id", func(ctx *leego.Context) {
	_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": ctx.GetPathValue("id")})
})

// /files/a/b/c.txt -> ctx.GetPathValue("filepath") == "a/b/c.txt"
engine.GET("/files/*filepath", func(ctx *leego.Context) {
	_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("filepath"))
})
```

匹配优先级：静态段 > `:param` > `*wildcard`。未命名的 `*` 使用默认参数名
`filepath`。

### 分组

```go
api := engine.NewGroup("/api/v1")
api.UseMiddlewares(authMiddleware)

admin := api.NewGroup("/admin")
admin.UseMiddlewares(requireRole("admin"))
admin.GET("/stats", showStats) // 实际路径：GET /api/v1/admin/stats
```

父组的前缀与中间件会被子组继承；中间件按「父 → 子」顺序执行。

### 静态文件

```go
engine.SetStatic("/static", "./public")
engine.SetStaticFS("/assets", http.Dir("./assets"))
```

框架内置路径遍历防护：`..`、反斜杠、`NUL`、绝对路径等非法请求直接拒绝。

### 黑名单与路由开关

```go
engine.AddURLBlacklist("/admin", "/internal") // 命中返回 403

if handler, ok := engine.GetHandler(http.MethodGet, "/maintenance"); ok {
	handler.Disable() // 之后访问返回 423；处理器与指标仍然保留
}
```

### 405、HEAD 与 OPTIONS

- 路径存在但方法不匹配：返回 `405`，并携带 `Allow` 头；
- 未注册 `HEAD` 的路径：自动回退到 `GET`（`net/http` 会丢弃消息体）；
- 未注册 `OPTIONS` 的路径：自动返回 `204` 并携带 `Allow`。

`Allow` 由实际注册的方法推导，始终包含 `OPTIONS`，存在 `GET` 时同时包含 `HEAD`。

### 路由表

```go
for _, route := range engine.GetRoutes() {
	// route.Method / route.Pattern / route.Name / route.Tags / route.Enabled
}
```

`GetRoutes()` 返回按方法与模式排序的只读快照，可直接序列化给前端或运维接口。

> 路由在 `Prepare()`（首次请求或 `Run` 时自动触发）后冻结。冻结后继续注册会
> panic，因此**所有注册必须在启动前完成**。

---

## Context 上下文

### 请求参数

```go
ctx.GetQueryValue("id")           // ?id=xxx
ctx.GetFormValue("name")          // Query + form-urlencoded
ctx.GetPostFormValue("name")      // 仅 form-urlencoded
ctx.GetMultipartFormValue("name") // multipart 字段
ctx.GetPathValue("id")            // 路由参数
ctx.GetPathValues()               // 全部路由参数
```

### 请求体与绑定

```go
raw, err := ctx.ReadBody()        // 读取并缓存请求体（受 MaxBodyBytes 限制）
body, _ := ctx.GetRequestBody()   // 仅返回已缓存内容，不触发读取

var payload User
if err := ctx.BindJSON(&payload); err != nil {
	ctx.ResponseError(http.StatusBadRequest, "JSON 无效: "+err.Error())
	return
}

_ = ctx.Bind(&payload)  // 按 Content-Type 自动选择 JSON / XML / Form
_ = ctx.BindXML(&payload)
_ = ctx.BindForm(&payload)
```

绑定目标必须是非 nil 指针，否则返回错误；`UseStrictJSON(true)` 时未知字段报错。

### 文件上传

```go
file, header, err := ctx.GetFormFile("avatar")
// 或直接落盘（权限默认 0o750，可自定义）
_ = ctx.SaveUploadedFile(header, "./uploads/avatar.png", 0o600)
```

### Cookie

```go
ctx.SetCookieData("sid", "abc123", 3600, "/", "", false, true)
ctx.SetSameSite(http.SameSiteLaxMode)

value, err := ctx.GetCookieValue("sid")
```

### 响应

| 方法 | 说明 |
| --- | --- |
| `ResponseJSON(code, data)` | `application/json` |
| `ResponseXML(code, data)` | `application/xml` |
| `ResponseText(code, text)` | `text/plain` |
| `ResponseHTML(code, html)` | `text/html` |
| `ResponseBytes(code, data)` | 原始字节 |
| `ResponseError(code, message)` | 记录错误链 + 状态码 + 纯文本错误体 |
| `Fail(err)` | 记录错误并交给 `Engine.SetErrorHandler` 或默认 JSON 错误格式 |
| `ResponseRedirect(code, location)` | 重定向；`code` 必须落在 300–308，否则 panic |
| `ResponseTemplate(name, data)` | 渲染已加载模板 |
| `ResponseTemplateFile(file, name, data)` | 渲染指定模板文件 |
| `ResponseFile(path)` | 浏览器内展示文件 |
| `ResponseFileFromFS(path, fs)` | 从自定义文件系统展示文件 |
| `ResponseAttachment(path)` | 作为附件下载 |
| `ResponseAttachmentFromFS(path, fs)` | 从自定义文件系统下载附件 |
| `ResponseSSE(callback)` | 单连接 SSE 事件流 |

```go
_ = ctx.ResponseJSON(http.StatusCreated, leego.JSON{"id": 1, "name": "leego"})
ctx.ResponseRedirect(http.StatusFound, "/login")
```

### Header、状态码与元信息

```go
ctx.SetStatusCode(http.StatusAccepted)
status := ctx.GetStatusCode()

ctx.SetRequestHeader("X-Trace-Id", "123")
ctx.GetRequestHeader("Authorization")
ctx.SetResponseHeader("X-Powered-By", "Leego")
ctx.DeleteResponseHeader("X-Powered-By")

ctx.GetClientIP()       // 可信代理感知
ctx.GetContentType()    // 去除参数后的媒体类型
ctx.IsWebsocket()
ctx.GetStartTime()      // 上下文创建时间，用于自定义耗时统计
ctx.GetServiceName()    // 引擎名称
ctx.GetEngine()         // 所属引擎（可能为 nil）
```

### 错误链与派生上下文

```go
ctx.AddError(errors.New("下游超时"))
last := ctx.GetLastError()

derived := ctx.DeriveContext(context.WithValue(ctx.Request.Context(), key, value))
value, _ := derived.GetContextValue(key)
```

### JWT 声明

```go
ctx.SetJWTClaims(claims)         // 由认证中间件写入
claims, ok := ctx.GetJWTClaims() // 由业务处理器读取
```

---

## 中间件

中间件签名为 `func(next leego.HandleFunc) leego.HandleFunc`：

```go
func authMiddleware(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		if ctx.GetRequestHeader("Authorization") == "" {
			ctx.ResponseError(http.StatusUnauthorized, "unauthorized")
			return // 不调用 next：请求在此终止
		}
		next(ctx) // 调用 next：继续后续链路
	}
}

engine.UseMiddlewares(authMiddleware)
```

### 内置中间件

| 入口 | 说明 |
| --- | --- |
| `middleware.Log()` / `middleware.LogWithConfig(cfg)` | 访问日志：方法、路径、状态码、耗时、客户端 IP，可配色、可跳过指定路径 |
| `middleware.Recover()` / `middleware.RecoverWithConfig(cfg)` | panic 恢复：输出堆栈与可选请求内容，默认返回 500 |
| `middleware.Collect(collector)` | 为手工挂载的第三方处理器补充指标采集（路由级采集已内建，勿重复叠加） |
| `middleware.WrapFromHTTPHandler(h)` | 把 `http.Handler` 包装成中间件 |
| `middleware.WrapFromHTTPHandlerFunc(f)` | 把 `http.HandlerFunc` 包装成中间件 |
| `middleware.UnwrapFromMiddlewareFuncToHTTPHandler(m)` | 把中间件解包为 `http.Handler` |
| `cors.NewDefaultCORS()` | CORS：源、方法、头、凭据、`Max-Age` 与预检处理 |
| `jwt.NewHMACSigner(alg, key)` / `jwt.NewParser()` | JWT 签发与解析（HS256/384/512） |
| `limiter.NewFixedWindowLimiter(limit, window)` | 固定窗口限流 |
| `limiter.NewSlidingWindowLimiter(limit, buckets, window)` | 滑动窗口限流 |
| `limiter.NewTokenBucketLimiter(limit, burst)` | 令牌桶限流 |
| `breaker.NewCircuitBreaker(config)` | 熔断器：失败率窗口 + 半开探测 |

```go
import (
	"time"

	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/breaker"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/middleware/limiter"
)

corsInstance := cors.NewDefaultCORS()
corsInstance.SetAllowOrigins("https://example.com")
corsInstance.SetMaxAge(3600)

engine.UseMiddlewares(
	middleware.Log(),
	middleware.Recover(),
	corsInstance.GetMiddleware(),
	limiter.NewTokenBucketLimiter(100, 200).GetMiddleware(3*time.Second),
)

circuitBreaker := breaker.NewCircuitBreaker(breaker.NewDefaultConfig())
engine.GET("/risky", func(ctx *leego.Context) {
	if err := circuitBreaker.ExecuteWithContext(ctx, func(ctx *leego.Context) error {
		return callDownstream(ctx)
	}); err != nil {
		ctx.ResponseError(http.StatusServiceUnavailable, err.Error())
	}
})
```

> 限流的 `GetMiddleware` 接收「等待配额的最长时间」，`<= 0` 表示立即返回 429；
> 熔断器的 `ExecuteWithContext` 会把 5xx 或返回错误视为失败样本。

---

## 指标与分析后台

### 采集

引擎为**每条路由**内置采集器，无需额外中间件。指标维度为「方法 + 模式 + 状态码」：

```go
report := engine.CollectReport()
// report.GeneratedAt / report.Routes（[]collector.Snapshot）/ report.Totals

for _, snapshot := range engine.GetAllHandlerSnapshots() {
	// snapshot.Name / Method / Pattern / Tags / Enabled
	// snapshot.Flights / Total / Failed / TotalCostMicro
	// snapshot.Metrics[i].Code / Total / MaxCostMicro / MinCostMicro / TotalCostMicro
	_ = snapshot.GetAverageCostMicro()
	_ = snapshot.GetFailureRate()
}
```

`Totals` 与 `Snapshot` 提供 `GetAverageCostMicro()`、`GetFailureRate()` 等派生方法，
避免前端重复计算。

### 后台

```go
import "gitee.com/leminewx/leego/dashboard"

_ = engine.RegisterDashboard("/metrics",
	dashboard.UseTitle("指标分析"),
	dashboard.UseRefreshInterval(5*time.Second),
	dashboard.UseMaxRoutes(200),
	dashboard.EnableExport(true),
	dashboard.UseAuthorizer(func(req *http.Request) bool {
		return req.Header.Get("Authorization") == "Bearer admin"
	}),
)
```

注册后可用的端点（相对前缀）：

| 端点 | 说明 |
| --- | --- |
| `GET /metrics` | 单页后台：总览、路由明细、状态码分布、关键字/方法/状态/启用状态过滤 |
| `GET /metrics/api/report` | 完整指标报告（JSON），支持同样的查询参数 |
| `GET /metrics/api/routes` | 路由清单（JSON） |
| `GET /metrics/api/health` | 健康检查 |
| `GET /metrics/export.csv` | CSV 导出（需开启导出） |

实现要点：

- 页面使用 `go:embed` 内嵌，无外部 CDN 依赖，离线可用；
- `Option` 为公开函数类型，三方可以自行编写选项扩展行为；
- `Provider` 与 `Authorizer` 均为函数注入，便于替换数据源或接入自有鉴权；
- 报告按路由名、模式、方法排序，保证同一份数据多次渲染结果稳定。

---

## SSE 与 WebSocket

### SSE：单连接回调式

```go
engine.GET("/sse/once", func(ctx *leego.Context) {
	_ = ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
		events := make(chan *sse.Message, 1)
		go func() {
			defer close(events)
			select {
			case <-requestCtx.Done():
			case events <- sse.NewMessageWithEvent("tick", []byte(`{"ok":true}`)):
			}
		}()
		return events
	})
})
```

### SSE：管理器托管多连接

```go
manager := sse.NewManager()
manager.SetHeartbeatPeriod(10 * time.Second)
defer manager.Close()

go func() {
	for err := range manager.GetErrorChan() { // 必须消费，否则错误会被丢弃
		log.Printf("SSE: %v", err)
	}
}()

engine.GET("/sse/:client", func(ctx *leego.Context) {
	// Serve 会写响应头 + connect 事件，然后阻塞到客户端断开
	_ = manager.Serve(ctx.Response, ctx.Request, ctx.GetPathValue("client"))
})

manager.Broadcast(sse.NewHeartbeatMessage())
manager.SendTo("client-1", sse.NewMessageWithEvent("notice", payload))
```

### WebSocket

```go
import (
	"gitee.com/leminewx/leego/response/websocket"
	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

manager := websocket.NewManager(upgrader.NewDefaultConfig())
go manager.Start(context.Background())

engine.GET("/ws", func(ctx *leego.Context) {
	if ctx.IsWebsocket() {
		_ = manager.Serve(ctx.Response, ctx.Request)
	}
})
```

自行升级连接时使用 `upgrader.NewUpgrader(config).Upgrade(req, writer, header)`；
`Config` 支持读/写超时、最大读字节、来源校验与子协议协商。

---

## 压力测试

`stresstest` 包只依赖标准库，可作为库调用或独立工具使用：

```go
import stress "gitee.com/leminewx/leego/stresstest"

config := stress.NewDefaultConfig("http://127.0.0.1:8080/ping")
config.Concurrent = 50
config.TotalRequests = 5000
config.Timeout = 5 * time.Second
config.KeepAlive = true

client, err := stress.NewClient(config)
if err != nil {
	log.Fatal(err)
}
defer client.Close()

snapshot := client.Run()
fmt.Println(stress.FormatReport(snapshot))
```

报告包含：总请求数、成功/失败数、成功率、错误数、状态码分布、平均/最小/最大
延迟与 P50/P90/P99 分位延迟。延迟统计使用「最近秩」分位数，避免插值引入的
乐观偏差。

---

## 启动与优雅关闭

```go
engine := leego.NewEngine()
// ... 注册路由与中间件

// 普通 HTTP
// engine.Run(":8080")

// HTTPS
// engine.RunTLS(":8443", "cert.pem", "key.pem")

// Unix Socket / 文件描述符 / 自定义 Listener
// engine.RunUnix("/tmp/leego.sock")
// engine.RunFd(3)
// engine.RunListener(listener)

// 交给标准库服务
// http.ListenAndServe(":8080", engine.GetHTTPHandler())

// 等待 SIGINT / SIGTERM，最多 10 秒优雅关闭
if err := engine.WaitSignalAndShutdown(10*time.Second, func() error {
	return closeDatabase() // 关闭前回调，可返回错误
}); err != nil {
	log.Printf("关闭失败: %v", err)
}
```

`Shutdown(ctx)` 可脱离信号手动关闭；`GetServer()` 返回内部 `http.Server`，
用于读取监听地址或做更细粒度的控制。启动日志会输出路由数量与监听地址，
可用 `DisableStartupLog()` 关闭。

---

## 测试与基准

```bash
go build ./...
go vet ./...
go test ./...                 # 单元测试
go test -race ./...           # 并发正确性
go test -cover ./...          # 覆盖率
go test -bench . -benchmem ./ # 基准
```

测试覆盖路由匹配、405/HEAD/OPTIONS、黑名单、冻结后行为、上下文绑定与响应、
中间件链、JWT 往返与伪造、SSE 并发投递、WebSocket 帧解析、指标报告、后台
过滤与导出、压测统计等路径；长连接相关测试使用自实现的并发安全记录器，
可在 `-race` 下稳定运行。

---

## 项目结构

```
leego/
├── engine.go / group.go / handler.go / router.go   # 引擎、分组、处理器、路由器
├── context.go                                      # 上下文与请求/响应能力
├── tree/                                           # 分段 Trie（静态 > 参数 > 通配）
├── response/                                       # 响应包装器、状态文本、Writer 池
│   ├── sse/                                        # SSE 客户端、消息、连接管理器
│   └── websocket/                                  # WebSocket 连接、帧解析、升级器
├── middleware/                                     # 日志、恢复、指标采集中间件
│   ├── collector/                                  # 指标类型与报告聚合
│   ├── cors/  jwt/  limiter/  breaker/             # 各功能中间件
├── dashboard/                                      # 指标分析后台（页面内嵌）
├── headers/                                        # HTTP 头常量与内容类型识别
├── logging/                                        # 框架内部日志输出
├── stresstest/                                     # 压测客户端与报告
└── examples/                                       # 可运行示例
```

---

## 设计取舍与命名规范

**为什么冻结路由？** 注册期可自由写入，运行期只读，从而避免每个请求加锁；
代价是启动后无法动态注册路由，需要用 `Handler.Enable/Disable` 做运行时开关。

**为什么上下文池化？** 每个请求都要创建上下文与响应包装器，池化后热路径无堆
分配；代价是处理器中**不得跨请求保存 `*Context`**，需要派生时使用
`DeriveContext`。

**为什么指标按状态码分桶？** 同一路由的 2xx / 4xx / 5xx 往往代表不同业务语义，
分桶后既能算成功率，也能直接绘制状态码分布；内存上限由状态码数量天然约束。

**命名规范**：函数与方法首词必须是动词、be 动词或情态动词（接口实现除外）；
构造函数统一 `New*` / `new*`；上下文响应统一 `Response*` 前缀；常量导出用
全大写下划线连接、包内私有用 `_` 开头全大写。`Group` 兼有动词词性（“分组、
把……归类”），因此 `NewGroup` 不构成名词开头违规。完整的动词表、词性判定、
常量例外与 API 迁移对照见 [docs/conventions.md](docs/conventions.md)。

**注释规范**：每个导出类型、字段、函数都有中文注释，说明「做什么」与「为什么
这样做」；涉及并发、池化、冻结、安全边界的实现必须写明约束。

---

## 文档索引

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 分层结构、请求生命周期、并发模型与设计取舍 |
| [docs/routing.md](docs/routing.md) | 路由语法、匹配优先级、405/HEAD/OPTIONS、黑名单与冻结 |
| [docs/context.md](docs/context.md) | 参数、绑定、响应、错误链、模板、文件与 SSE |
| [docs/middleware.md](docs/middleware.md) | 中间件模型、顺序、内置中间件与自定义中间件 |
| [docs/metrics-dashboard.md](docs/metrics-dashboard.md) | 指标模型、采集流程、后台端点与扩展方式 |
| [docs/sse-websocket.md](docs/sse-websocket.md) | SSE 两种用法、WebSocket 升级与连接管理 |
| [docs/jwt.md](docs/jwt.md) | 签发、解析、校验、时钟偏差与安全注意事项 |
| [docs/limiter-breaker.md](docs/limiter-breaker.md) | 三种限流算法与熔断状态机 |
| [docs/stresstest.md](docs/stresstest.md) | 压测配置、执行模型与报告指标 |
| [docs/testing.md](docs/testing.md) | 测试组织、并发测试技巧与基准 |
| [docs/conventions.md](docs/conventions.md) | 命名、注释、错误处理与 API 命名迁移对照 |
| [docs/extension-roadmap.md](docs/extension-roadmap.md) | 基于现有架构的拓展方向：功能、方案、场景与可行性分析 |
| [RELEASE.md](RELEASE.md) | 当前版本的行为变更、缺陷修复与升级步骤 |
| [ISSUES.md](ISSUES.md) | 能力边界、使用陷阱与后续规划 |

---

## 许可

本项目基于仓库中 `LICENSE` 文件所列条款发布。
