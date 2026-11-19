# Leego

![leego logo](docs/assets/logo.png)

Leego 是一个构建在 Go 标准库 `net/http` 之上的轻量级 Web 框架。它在保持
`http.Handler` 完全兼容的前提下，提供路由分组、中间件编排、上下文封装、
参数绑定、指标采集、内置指标分析后台、SSE / WebSocket、JWT、限流、熔断
与压测工具等能力，并提供零第三方依赖的工程化拓展包与开发者体验服务。

> 版本：`v0.17.0.0`（要求 Go 1.27.4+）

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
| 压测工具 | `harness/stresstest` 提供并发压测、延迟分位数与报告格式化 |
| 工程化拓展 | `session` / `upload` / `job` / `gateway` / `cache` / `compress` / `trace` / `metrics` / `openapi` / `config` / `validate` / `problem`，全部只依赖标准库与 `golang.org/x/*` |
| 体验工具 | `harness` 汇总测试客户端、压测入口与可运行示例，并内置带前端的实体服务 |

---

## 目录

- [安装](#安装)
- [快速开始](#快速开始)
- [核心概念](#核心概念)
- [引擎配置](#引擎配置)
- [路由](#路由)
- [Context 上下文](#context-上下文)
- [中间件](#中间件)
- [拓展能力](#拓展能力)
- [指标与分析后台](#指标与分析后台)
- [SSE 与 WebSocket](#sse-与-websocket)
- [压力测试](#压力测试)
- [开发者体验服务](#开发者体验服务)
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

依赖：Go 1.27.4+、`golang.org/x/crypto`、`golang.org/x/net`、`golang.org/x/time`。

---

## 快速开始

```go
package main

import (
	"log"
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middlewares"
)

func main() {
	engine := leego.NewEngine(
		leego.UseEngineName("demo"),
		leego.UseServerTimeouts(5*time.Second, 10*time.Second, 10*time.Second, 60*time.Second),
	)

	engine.UseMiddlewares(
		middlewares.NewLogger().AdaptToMiddleware(),
		middlewares.NewRecovery().AdaptToMiddleware(),
	)

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

### 启动标识

调用 `ShowLogo()` 可在服务启动时打印一次 ASCII 标识（`Run` 本身只输出启动日志）。
内置 4 种样式，默认沿用最初的宽幅艺术字：

```go
leego.SetLogoStyle(leego.LOGO_STYLE_PIXEL)
leego.ShowLogo()
```

| 常量 | 风格 | 宽度 | 依赖字符 |
| --- | --- | --- | --- |
| `LOGO_STYLE_CLASSIC` | 宽幅艺术字（默认） | 54 | 反斜杠、下划线、竖线 |
| `LOGO_STYLE_PIXEL` | 实心像素块 | 29 | `█` |
| `LOGO_STYLE_SHADOW` | 双线阴影 | 42 | `█`、`╔`、`║` |
| `LOGO_STYLE_ASCII` | 纯 ASCII 斜线体 | 30 | 仅 ASCII |

`GetLogoStyle()` 读取当前样式；越界值会被忽略。样式值用 `atomic.Int32` 存储，
可在启动协程中设置、并发调用 `ShowLogo()` 不产生数据竞争。

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
| `UseWriteTimeout(timeout)` | 60s | 单独设置写入超时；传入 `0` 关闭，SSE / WebSocket 长连接必需 |
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

> `SetCookieData` 使用 `url.PathEscape`，`GetCookieValue` 使用 `url.PathUnescape`，
> 空格与 `+` 都能原样往返；非法转义时原样返回而不是静默变成空串。

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
| `middlewares.NewLogger()` / `NewLoggerWithConfig(cfg)` | 访问日志：方法、路径、状态码、耗时、客户端 IP，可配色、可跳过指定路径 |
| `middlewares.NewRecovery()` / `NewRecoveryWithConfig(cfg)` | panic 恢复：输出堆栈与可选请求内容，默认返回 500 |
| `middlewares.NewMonitor(collector)` | 为手工挂载的第三方处理器补充指标采集（路由级采集已内建，勿重复叠加） |
| `middlewares.WrapFromHTTPHandler(h)` | 把 `http.Handler` 包装成中间件 |
| `middlewares.WrapFromHTTPHandlerFunc(f)` | 把 `http.HandlerFunc` 包装成中间件 |
| `middlewares.UnwrapFromMiddlewareFuncToHTTPHandler(m)` | 把中间件解包为 `http.Handler` |
| `cors.NewDefaultCORS()` | CORS：源、方法、头、凭据、`Max-Age` 与预检处理 |
| `jwt.NewHMACSigner(alg, key)` / `jwt.NewParser()` | JWT 签发与解析（HS256/384/512） |
| `limiter.NewFixedWindowLimiter(limit, window)` | 固定窗口限流 |
| `limiter.NewSlidingWindowLimiter(limit, buckets, window)` | 滑动窗口限流 |
| `limiter.NewTokenBucketLimiter(limit, burst)` | 令牌桶限流 |
| `breaker.NewCircuitBreaker(config)` | 熔断器：失败率窗口 + 半开探测 |
| `cache.NewCache(cache.Config{...})` | 响应缓存：LRU 容量控制、`X-Cache` 标记与 stale-while-revalidate，默认跳过带 Cookie 的请求 |
| `compress.NewCompress(compress.Config{...})` | gzip 压缩：按内容类型与最小长度阈值决定是否压缩 |
| `trace.NewTracer(exporter, ...)` | 链路追踪：解析/生成 W3C `traceparent`，批量异步导出 |

除包装/解包工具函数外，上表中的构造器只负责创建实例；要用作中间件需再调用
实例的 `AdaptToMiddleware()` 得到 `leego.MiddlewareFunc`。

```go
import (
	"time"

	"gitee.com/leminewx/leego/middlewares"
	"gitee.com/leminewx/leego/middlewares/breaker"
	"gitee.com/leminewx/leego/middlewares/cors"
	"gitee.com/leminewx/leego/middlewares/limiter"
)

corsInstance := cors.NewDefaultCORS()
corsInstance.SetAllowOrigins("https://example.com")
corsInstance.SetMaxAge(3600)

// 等待上限 3 秒，超时返回 429。
rateLimiter := limiter.NewTokenBucketLimiter(100, 200, limiter.Config{WaitTimeout: 3 * time.Second})

engine.UseMiddlewares(
	middlewares.NewLogger().AdaptToMiddleware(),
	middlewares.NewRecovery().AdaptToMiddleware(),
	corsInstance.AdaptToMiddleware(),
	rateLimiter.AdaptToMiddleware(),
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

> 限流的等待上限由 `limiter.Config.WaitTimeout` 决定（`0` 表示立即返回 429）；
> 熔断器的 `ExecuteWithContext` 会把 5xx 或返回错误视为失败样本。
>
> 引擎在请求入口还有一道 panic 兜底：即使没有挂 `NewRecovery()`，panic 也会
> 被记录堆栈并返回 500（`http.ErrAbortHandler` 仍按标准库语义上抛）；`NewRecovery()`
> 的价值在于可定制的堆栈输出、请求转储与收尾回调。

---

## 拓展能力

核心框架之外，仓库提供一组零第三方依赖的工程化包（仅标准库与
`golang.org/x/crypto`、`golang.org/x/net`、`golang.org/x/time`），按需引入；
不引入的包不会被链接进最终二进制。

| 包 | 能力 | 关键入口 |
| --- | --- | --- |
| `config` | 多源配置合并（Map / JSON / Env / Flag）、热更新与键级订阅 | `Load`、`GetString`、`SubscribeChange`、`Watch` |
| `validate` | 结构体标签校验，规则表可扩展，元信息按类型缓存 | `Check`、`CheckVar`、`RegisterRule` |
| `problem` | RFC 7807 风格的统一错误响应 | `New`、`NewFromValidation`、`Write` |
| `session` | 签名 Cookie 会话，存储接口可替换，支持轮换与销毁 | `session.New`、`GetFromContext` |
| `upload` | 分片上传：断点续传、乱序/重复分片、SHA-256 秒传（JSONL 追加索引，重启可恢复） | `NewLocalStorage`、`NewHandler` |
| `job` | 最小堆调度：周期/延迟任务、并发槽位与运行统计，单任务 panic 隔离 | `NewScheduler`、`ScheduleEvery`、`ScheduleAfter` |
| `gateway` | 反向代理与平滑加权轮询，支持标签灰度与超时预算 | `NewBalancer`、`NewProxy` |
| `middlewares/cache` | 响应缓存：LRU 容量、`X-Cache` 标记、stale-while-revalidate，默认跳过带 Cookie 的请求 | `cache.NewCache` |
| `middlewares/compress` | 按内容类型与最小长度阈值决定是否 gzip | `compress.NewCompress` |
| `middlewares/trace` | W3C `traceparent` 解析/生成与异步批量导出，未采样且不传播时零分配 | `trace.NewTracer` |
| `metrics` | 指标存储抽象、内存实现与 Prometheus 文本输出 | `NewMemoryStore`、`RegisterMetrics` |
| `openapi` | 从已注册路由快照生成 OpenAPI 文档与浏览页 | `NewSpec`、`RegisterOpenAPI` |

### 会话与分片上传

```go
import (
	"gitee.com/leminewx/leego/session"
	"gitee.com/leminewx/leego/upload"
)

store := session.NewMemoryStore()
defer store.Close()

engine.UseMiddlewares(session.New(
	store,
	session.UseTTL(30*time.Minute),
	session.UseSecure(true),
))

engine.POST("/login", func(ctx *leego.Context) {
	current, ok := session.GetFromRequest(ctx.Request)
	if !ok {
		ctx.ResponseError(http.StatusInternalServerError, "session unavailable")
		return
	}

	current.Set("user", "leminewx")
	// 登录成功必须轮换 ID，避免会话固定攻击。
	if err := current.Regenerate(ctx.Request.Context()); err != nil {
		ctx.ResponseError(http.StatusInternalServerError, err.Error())
		return
	}

	_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": current.GetID()})
})
```

分片上传由同一个处理器按 `action` 分发，分片体直接读原始字节，
不经过 multipart 解析：

```go
storage, err := upload.NewLocalStorage("./data")
if err != nil {
	log.Fatal(err)
}

handler := upload.NewHandler(storage,
	upload.UseMaxSize(1<<30),
	upload.UseDefaultPartSize(4<<20),
	upload.UseMaxParts(1024),
)

// POST ?action=init | PUT ?action=part&id=..&index=..
// POST ?action=complete&id=.. | GET ?action=status&id=.. | DELETE ?action=abort&id=..
engine.POST("/upload", handler)
engine.PUT("/upload", handler)
engine.GET("/upload", handler)
engine.DELETE("/upload", handler)
```

### 任务调度、网关与链路追踪

```go
scheduler := job.NewScheduler(job.UseWorkers(4))
scheduler.Start(context.Background())
defer scheduler.Stop(context.Background())

cancel := scheduler.ScheduleEvery(time.Minute, job.JobFunc(func(ctx context.Context) error {
	return refreshCache(ctx)
}))
defer cancel() // 取消周期任务

scheduler.ScheduleAfter(5*time.Second, job.JobFunc(func(context.Context) error {
	return warmUp()
}))

stats := scheduler.GetStats()
// stats.Runs / Dropped / Pending / Started / Stopped
```

```go
stable, _ := url.Parse("http://127.0.0.1:9001")
canary, _ := url.Parse("http://127.0.0.1:9002")

balancer, err := gateway.NewBalancer(
	gateway.Upstream{URL: stable, Weight: 9, Tag: "stable"},
	gateway.Upstream{URL: canary, Weight: 1, Tag: "v2"},
)
if err != nil {
	log.Fatal(err)
}

proxy := gateway.NewProxy(balancer,
	gateway.UseTimeout(3*time.Second),
	gateway.UseErrorHandler(func(ctx *leego.Context, target *url.URL, err error) {
		ctx.ResponseError(http.StatusBadGateway, "upstream unavailable")
	}),
)

engine.GET("/proxy/*path", proxy)
engine.POST("/proxy/*path", proxy)
// 请求头带 X-Leego-Canary: v2 时优先路由到标签为 v2 的上游。
```

```go
exporter := trace.NewMemoryExporter(1024)
tracer := trace.NewTracer(exporter, trace.WithFlushInterval(time.Second))
engine.UseMiddlewares(tracer.AdaptToMiddleware())
// 关闭前刷新剩余 Span：tracer.Shutdown(ctx)
```

### 配置、校验与统一错误

```go
settings, err := config.Load(
	config.NewJSONSource("config.json", config.UseWatchInterval(time.Second)),
	config.NewEnvSource("APP_"),
)
if err != nil {
	log.Fatal(err)
}
defer settings.Close()

addr := settings.GetString("http.addr", "127.0.0.1:8080")
settings.SubscribeChange("http.addr", func(value any) {
	log.Printf("监听地址变更: %v", value)
})

if err := settings.Watch(0); err != nil { // 0 表示默认 1 秒轮询
	log.Printf("配置监听未启动: %v", err)
}
```

```go
type CreateUser struct {
	Name  string `json:"name" validate:"required,min=2,max=32"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=0,lte=150"`
}

engine.POST("/users", func(ctx *leego.Context) {
	var request CreateUser
	if err := ctx.BindJSON(&request); err != nil {
		problem.New(http.StatusBadRequest, "invalid_json", err.Error()).Write(ctx)
		return
	}

	if err := validate.Check(&request); err != nil {
		problem.NewFromValidation(err).Write(ctx) // 422 + 字段级细节
		return
	}

	_ = ctx.ResponseJSON(http.StatusCreated, request)
})
```

内置校验规则包括 `required`、`omitempty`、`min`、`max`、`len`、`eq`、`ne`、
`gt`、`gte`、`lt`、`lte`、`oneof`、`contains`、`prefix`、`suffix`、`email`、
`url`、`uuid`、`ip`、`ipv4`、`ipv6`、`alpha`、`alnum`、`numeric`、`lowercase`、
`uppercase` 与 `dive`；未知规则会在首次校验时报错，避免规则名写错后静默通过。

### OpenAPI 文档

```go
import "gitee.com/leminewx/leego/openapi"

// 必须在 Prepare() 之前调用：文档在注册期从路由快照一次性构建，运行期只读。
_ = engine.RegisterOpenAPI("/openapi",
	openapi.UseTitle("订单服务"),
	openapi.UseVersion("1.4.0"),
	openapi.UseDescription("订单与支付相关接口"),
)
```

| 端点 | 说明 |
| --- | --- |
| `GET <prefix>` | 文档浏览页（`go:embed` 内嵌，离线可用，无外部 CDN） |
| `GET <prefix>/openapi.json` | OpenAPI 3.1 文档 JSON |

路由模式会转成 OpenAPI 路径模板（`:id` → `{id}`、`*path` → `{path}`），
路径参数统一声明为 `string`；前缀为空时默认 `/openapi`。

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

### 指标导出端点

需要给 Prometheus 抓取或历史趋势图提供数据时，可另外挂载 `RegisterMetrics`：

```go
store := metrics.NewMemoryStore(time.Hour, 60)
defer store.Close()

_ = engine.RegisterMetrics("/observe", store,
	leego.UseMetricsInterval(5*time.Second),
	leego.UseMetricsStep(time.Minute),
	leego.UseMetricsAuthorizer(func(ctx *leego.Context) bool {
		return ctx.GetRequestHeader("Authorization") == "Bearer "+os.Getenv("METRICS_TOKEN")
	}),
)
```

| 端点 | 说明 |
| --- | --- |
| `GET <prefix>` | 历史趋势 JSON（按 `Step` 聚合） |
| `GET <prefix>/prometheus` | Prometheus 文本格式（可关闭） |

`UseMetricsAuthorizer` 为 `nil` 时不做校验；生产环境必须配置，或将端点限制在内网。
`DisableMetricsPrometheus()` 可关闭抓取路由；`RegisterMetrics` 与 `RegisterDashboard`
的前缀不要相同，否则会因重复注册而失败。

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
import stress "gitee.com/leminewx/leego/harness/stresstest"

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
乐观偏差。`harness/stresstest/test/base` 提供可直接压测的基准服务：

```bash
go run ./harness/stresstest/test/base
```

---

## 开发者体验服务

`harness` 包把测试客户端（`harness/testutil`）、压测入口（`harness/stresstest`）、
可运行示例（`harness/examples/*`）与一个开箱即用的实体服务整合在一起。
实体服务内置单页前端，覆盖会话、缓存、限流、分片上传、SSE、网关灰度、
任务调度与链路追踪，适合本地体验与联调：

```bash
go run ./harness/cmd/harness                     # 默认 127.0.0.1:8080
go run ./harness/cmd/harness -addr :9000 -data ./tmp
```

打开 `http://127.0.0.1:8080/` 即为体验页面，页面顶部提供：

| 入口 | 说明 |
| --- | --- |
| `/` | 体验前端（十张能力卡片，纯原生 JS，无外部 CDN） |
| `/dashboard` | 指标分析后台 |
| `/openapi` | OpenAPI 文档与浏览页 |
| `/metrics/prometheus` | Prometheus 文本指标 |
| `/api/traces` | 最近链路（内存导出器） |

作为库使用时，`harness.NewService` 返回可嵌入的 `*Service`：

```go
service, err := harness.NewService(
	harness.UseAddr("127.0.0.1:0"),
	harness.UseDataDir(t.TempDir()),
	harness.UseLogging(false),
)
if err != nil {
	t.Fatal(err)
}
defer service.Shutdown(context.Background())

server := httptest.NewServer(service.GetHandler())
defer server.Close()
```

`Service.Run(ctx)` 负责监听、提供服务，并在 `ctx` 取消后优雅关闭；
`GetEngine()` 返回底层引擎，可在其基础上继续注册业务路由。

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
├── validatable.go / metrics.go / openapi.go        # 子包适配：校验、指标、OpenAPI
├── lifecycle.go / password.go                      # 关闭钩子、密码哈希
├── tree/                                           # 分段 Trie（静态 > 参数 > 通配）
├── response/                                       # 响应包装器、状态文本、Writer 池
│   ├── sse/                                        # SSE 客户端、消息、连接管理器
│   └── websocket/                                  # WebSocket 连接、帧解析、升级器
├── middlewares/                                    # 日志、恢复、指标采集中间件
│   ├── collector/                                  # 指标类型与报告聚合
│   ├── cache/  compress/  trace/                   # 缓存、压缩、链路追踪中间件
│   └── cors/  jwt/  limiter/  breaker/             # 各功能中间件
├── dashboard/                                      # 指标分析后台（页面内嵌）
├── metrics/                                        # 指标存储抽象与 Prometheus 输出
├── openapi/                                        # OpenAPI 快照与浏览页
├── config/  validate/  problem/                    # 配置、校验与统一错误响应
├── session/  upload/  job/  gateway/               # 会话、分片上传、调度与网关
├── headers/                                        # HTTP 头常量与内容类型识别
├── logging/                                        # 框架内部日志输出
└── harness/                                        # 开发者体验：测试客户端、压测、示例
    ├── testutil/                                   # HTTP 测试客户端与冻结时钟
    ├── stresstest/                                 # 压测客户端与报告（test/base 为基准服务）
    ├── examples/                                   # 可运行示例（base/file/sse/websocket/stress）
    ├── testdata/                                   # 演示数据：data.txt / ws-client.html / stress.html
    ├── internal/assetpath/                         # 统一解析 testdata 目录
    ├── cmd/harness/                                # 体验服务入口
    ├── statics/                                    # 体验前端（go:embed）
    └── tools/genlogo/                              # 品牌图生成器（go:build ignore）
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
| [docs/middlewares.md](docs/middlewares.md) | 中间件模型、顺序、内置中间件与自定义中间件 |
| [docs/metrics-dashboard.md](docs/metrics-dashboard.md) | 指标模型、采集流程、后台端点与扩展方式 |
| [docs/sse-websocket.md](docs/sse-websocket.md) | SSE 两种用法、WebSocket 升级与连接管理 |
| [docs/jwt.md](docs/jwt.md) | 签发、解析、校验、时钟偏差与安全注意事项 |
| [docs/limiter-breaker.md](docs/limiter-breaker.md) | 三种限流算法与熔断状态机 |
| [docs/stresstest.md](docs/stresstest.md) | 压测配置、执行模型与报告指标 |
| [docs/testing.md](docs/testing.md) | 测试组织、并发测试技巧与基准 |
| [docs/harness.md](docs/harness.md) | 体验服务、前端页面、测试客户端与压测入口 |
| [docs/conventions.md](docs/conventions.md) | 命名、注释、错误处理与 API 命名迁移对照 |
| [docs/extension-roadmap.md](docs/extension-roadmap.md) | 基于现有架构的拓展方向：功能、方案、场景与可行性分析 |
| [docs/extension-design.md](docs/extension-design.md) | 拓展模块的实现方式与代码结构：包布局、接口签名、难点与 PR 切片 |
| [RELEASE.md](RELEASE.md) | 当前版本的行为变更、缺陷修复与升级步骤 |
| [ISSUES.md](ISSUES.md) | 能力边界、使用陷阱与后续规划 |

---

## 许可

本项目基于仓库中 `LICENSE` 文件所列条款发布。
