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

依赖：Go 1.27.4+、`golang.org/x/crypto`、`golang.org/x/time`。

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
	// 选项在构造期一次性写入配置：服务名进入日志与指标，四个超时进入内置的
	// http.Server；任一超时 <= 0 时保留默认值，不会退化成无限等待。
	engine := leego.NewEngine(
		leego.UseEngineName("demo"),
		leego.UseServerTimeouts(5*time.Second, 10*time.Second, 10*time.Second, 60*time.Second),
	)

	// 全局链按追加顺序执行：日志在外层（覆盖全部响应），恢复在内层（兜住后续 panic）。
	// 必须在首个请求前注册——引擎冻结后再追加中间件会 panic。
	engine.UseMiddlewares(
		middlewares.NewLogger().AdaptToMiddleware(),
		middlewares.NewRecovery().AdaptToMiddleware(),
	)

	// 第三个参数是路由名称，会出现在启动日志、指标后台与 OpenAPI 文档里。
	engine.GET("/ping", func(ctx *leego.Context) {
		// Context 取自 sync.Pool，禁止跨请求保存；ResponseJSON 会自行写出
		// Content-Type 与状态码，返回值只在编码阶段失败时非 nil。
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"message": "pong"})
	}, "健康检查")

	// Run 阻塞到服务退出；正常关闭的 http.ErrServerClosed 会被转成 nil，
	// 因此这里的 err 只代表端口占用一类的真实故障。
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
// 样式是进程级全局状态，启动前设置一次即可；越界值会被忽略。
leego.SetLogoStyle(leego.LOGO_STYLE_PIXEL)
// ShowLogo 只写 os.Stdout，Run 不会自动调用它，需要显式打印。
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
| `UseH2C()` | 关闭 | 同端口接受明文 HTTP/2（h2c），与 HTTP/1.1 并存 |
| `UseServerTimeouts(readHeader, read, write, idle)` | 10s / 60s / 60s / 120s | 任一参数 `<= 0` 时保留默认值，避免退化为无限等待 |
| `UseWriteTimeout(timeout)` | 60s | 单独设置写入超时；传入 `0` 关闭，SSE / WebSocket 长连接必需 |
| `UseTrustedProxies(cidrs...)` | 空 | 仅信任这些网段传来的 `X-Forwarded-For` / `X-Real-IP` |
| `DisableStartupLog()` | 关闭 | 不打印启动期的路由注册信息 |

> `UseH2C()` 基于标准库 `http.Server.Protocols`（Go 1.24+），通过 `Run` / `RunTLS` /
> `RunUnix` / `RunFd` / `RunListener` 启动时自动生效。自行创建 `http.Server` 时，把
> `Protocols` 字段设为 `engine.GetHTTPProtocols()`；`GetHTTPHandler()` 只返回引擎本身，
> 不再包装处理器。标准库只实现前导知识方式：客户端需直接发送 HTTP/2 连接序言，
> 依赖 `Upgrade: h2c` 握手的客户端会退化为 HTTP/1.1。

运行时读取与修改：

```go
config := engine.GetConfig()    // 返回深拷贝：修改它不会影响引擎
engine.SetName("order-service") // 等价于替换 config.Name
engine.SetMaxMultipartMemory(8 << 20)
engine.SetTrustedProxies("10.0.0.0/8") // 只有这些网段传来的转发头才被采信

engine.SetErrorHandler(func(ctx *leego.Context, err error) {
	// ctx.Fail(err) 的最后一步会走到这里；未设置时输出默认 JSON 错误。
	// 处理器负责写出响应，返回后框架不会再补响应体。
	_ = ctx.ResponseJSON(http.StatusInternalServerError, leego.JSON{"error": err.Error()})
})
```

> `GetClientIP()` 只在直连对端属于可信代理网段时才解析转发头，避免客户端伪造
> `X-Forwarded-For` 绕过基于 IP 的限流或审计。

---

## 路由

### 注册

```go
// 处理器统一是 func(*leego.Context)；末尾的 tags 可选：
// 第一个元素作为路由名称，其余作为业务标签，进入指标与 OpenAPI。
engine.GET("/users", listUsers, "查询用户列表", "user")
engine.POST("/users", createUser)
engine.PUT("/users/:id", updateUser)
engine.PATCH("/users/:id", patchUser)
engine.DELETE("/users/:id", deleteUser)

// HEAD / OPTIONS 一般不必手动注册：缺省时分别回退到 GET、自动返回 204 + Allow，
// 这里演示显式注册的写法。
engine.HEAD("/health", health)
engine.OPTIONS("/users", options)
```

`tags` 为可选参数：第一个元素作为路由名称，其余作为业务标签，用于指标归类与
后台展示。

### 参数与通配符

```go
// :param 只匹配单个路径段，取值不含 "/"：/users/42 命中，/users/42/x 不命中。
engine.GET("/users/:id", func(ctx *leego.Context) {
	_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": ctx.GetPathValue("id")})
})

// *wildcard 匹配剩余全部路径（不含前导 "/"），未命名时默认参数名为 filepath：
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
api.UseMiddlewares(authMiddleware) // 只作用于该组及其子组

admin := api.NewGroup("/admin")
admin.UseMiddlewares(requireRole("admin")) // 排在 auth 之后：先认证，再判角色
admin.GET("/stats", showStats)             // 实际路径：GET /api/v1/admin/stats
```

父组的前缀与中间件会被子组继承；中间件按「父 → 子」顺序执行。

### 静态文件

```go
// 目录必须存在，否则注册期直接 panic，把配置错误挡在启动阶段。
// 路径遍历（..、反斜杠、NUL、绝对路径）由框架统一拦截。
engine.SetStatic("/static", "./public")
engine.SetStaticFS("/assets", http.Dir("./assets"))
```

框架内置路径遍历防护：`..`、反斜杠、`NUL`、绝对路径等非法请求直接拒绝。

### 黑名单与路由开关

```go
// 按请求路径精确匹配，命中即返回 403，不进入路由匹配，也不经过业务中间件。
engine.AddURLBlacklist("/admin", "/internal")

if handler, ok := engine.GetHandler(http.MethodGet, "/maintenance"); ok {
	// Disable 只切换运行时开关（之后访问返回 423）；处理器与指标都还在，可再 Enable。
	handler.Disable()
}
```

### 405、HEAD 与 OPTIONS

- 路径存在但方法不匹配：返回 `405`，并携带 `Allow` 头；
- 未注册 `HEAD` 的路径：自动回退到 `GET`（`net/http` 会丢弃消息体）；
- 未注册 `OPTIONS` 的路径：自动返回 `204` 并携带 `Allow`。

`Allow` 由实际注册的方法推导，始终包含 `OPTIONS`，存在 `GET` 时同时包含 `HEAD`。

### 路由表

```go
// 返回按方法 + 模式排序的只读快照，不含处理器，可直接序列化给运维接口。
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
// 六个取值入口共享同一份解析结果，重复调用不会重复解析请求。
ctx.GetQueryValue("id")           // 仅查询串：?id=xxx
ctx.GetFormValue("name")          // 查询串 + form-urlencoded
ctx.GetPostFormValue("name")      // 仅请求体中的 form-urlencoded
ctx.GetMultipartFormValue("name") // multipart 表单字段
ctx.GetPathValue("id")            // 路由参数
ctx.GetPathValues()               // 全部路由参数
```

### 请求体与绑定

```go
// ReadBody 读满并缓存：多次调用返回同一份字节，日志、审计与绑定都能复用；
// 超限时返回 http.MaxBytesError，Request.Body 仍由 net/http 负责关闭。
raw, err := ctx.ReadBody()
if err != nil {
	ctx.ResponseError(http.StatusRequestEntityTooLarge, err.Error())
	return
}

// 拿到原始字节后可以先做签名校验、审计落库，再交给下面的绑定。
verifySignature(raw)

var payload User
// 绑定复用上面的缓存，不会二次读取请求体；失败通常是 JSON 语法或类型不匹配。
if err := ctx.BindJSON(&payload); err != nil {
	ctx.ResponseError(http.StatusBadRequest, "JSON 无效: "+err.Error())
	return
}

// GetRequestBody 只返回已缓存内容，不触发读取，适合中间件收尾时记录请求体。
_ = ctx.GetRequestBody()

// Bind 按 Content-Type 分派到下面两个具体实现；也可直接调用其中一个。
_ = ctx.Bind(&payload)
_ = ctx.BindXML(&payload)
_ = ctx.BindForm(&payload)
```

绑定目标必须是非 nil 指针，否则返回错误；`UseStrictJSON(true)` 时未知字段报错。

### 文件上传

```go
// GetFormFile 返回文件句柄与元信息（文件名、大小、类型）；返回的
// multipart.File 由调用方关闭，框架不代管。
file, header, err := ctx.GetFormFile("avatar")
if err != nil {
	ctx.ResponseError(http.StatusBadRequest, "缺少文件字段 avatar")
	return
}
defer file.Close()

// 落盘时按给定权限创建父目录，默认 0o750；target 完全由业务决定，
// 若来自用户输入必须先做白名单或 filepath.IsLocal 校验。
_ = ctx.SaveUploadedFile(header, "./uploads/avatar.png", 0o600)
```

### Cookie

```go
// 参数顺序：名称、值、maxAge（秒）、path、domain、secure、httpOnly。
// 值会做 url.PathEscape，与 GetCookieValue 对称，空格与 "+" 都能往返。
ctx.SetCookieData("sid", "abc123", 3600, "/", "", false, true)
ctx.SetSameSite(http.SameSiteLaxMode) // 只影响本响应后续写出的 Cookie

// 非法转义时原样返回，不会静默变成空串。
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
// 成功响应统一走 Response* 家族：状态码与 Content-Type 一次写完。
_ = ctx.ResponseJSON(http.StatusCreated, leego.JSON{"id": 1, "name": "leego"})
// 状态码必须落在 300–308，否则 panic，避免发出浏览器无法识别的重定向。
ctx.ResponseRedirect(http.StatusFound, "/login")
```

### Header、状态码与元信息

```go
ctx.SetStatusCode(http.StatusAccepted) // 只设置状态码，响应体仍由业务写入
status := ctx.GetStatusCode()          // 尚未设置时返回 0

// 请求头改的是内存中的 Request.Header，不影响已接收的原始报文。
ctx.SetRequestHeader("X-Trace-Id", "123")
ctx.GetRequestHeader("Authorization")
ctx.SetResponseHeader("X-Powered-By", "Leego")
ctx.DeleteResponseHeader("X-Powered-By")

ctx.GetClientIP()    // 只在直连对端属于可信网段时才解析转发头
ctx.GetContentType() // 去掉 charset 等参数后的媒体类型
ctx.IsWebsocket()    // 依据 Connection / Upgrade 头判断
ctx.GetStartTime()   // 上下文创建时间，用于自定义耗时统计
ctx.GetServiceName() // 引擎名称
ctx.GetEngine()      // 所属引擎，可能为 nil
```

### 错误链与派生上下文

```go
// 错误链按追加顺序保存，nil 会被忽略；GetLastError 取最后一个，无错误时返回 nil。
ctx.AddError(errors.New("下游超时"))
last := ctx.GetLastError()

// 派生上下文共享响应包装器，但有独立的参数表与错误链副本，且不进入对象池；
// 用于给下游调用挂上超时或取消信号。
derived := ctx.DeriveContext(context.WithValue(ctx.Request.Context(), key, value))
value, _ := derived.GetContextValue(key)
```

### JWT 声明

```go
ctx.SetJWTClaims(claims)         // 认证中间件校验通过后写入
claims, ok := ctx.GetJWTClaims() // 业务处理器读取；未认证时 ok 为 false
```

---

## 中间件

中间件签名为 `func(next leego.HandleFunc) leego.HandleFunc`：

```go
// 中间件就是包装器：拿到 next 后决定是否调用、调用前后做什么。
func authMiddleware(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		if ctx.GetRequestHeader("Authorization") == "" {
			// 不调用 next 即终止链路；ResponseError 写纯文本并记录错误链。
			ctx.ResponseError(http.StatusUnauthorized, "unauthorized")
			return
		}
		next(ctx) // 调用 next：继续后续中间件与最终处理器
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
corsInstance.SetAllowOrigins("https://example.com") // 白名单来源；凭证需另行开启
corsInstance.SetMaxAge(3600) // 预检结果缓存 3600 秒

// 令牌桶：每秒 100 个令牌、突发 200；排队等待上限 3 秒，超时返回 429。
rateLimiter := limiter.NewTokenBucketLimiter(100, 200, limiter.Config{WaitTimeout: 3 * time.Second})

// 顺序即执行顺序：日志 → 恢复 → CORS → 限流，越靠前越接近请求入口。
engine.UseMiddlewares(
	middlewares.NewLogger().AdaptToMiddleware(),
	middlewares.NewRecovery().AdaptToMiddleware(),
	corsInstance.AdaptToMiddleware(),
	rateLimiter.AdaptToMiddleware(),
)

// ExecuteWithContext 会把 5xx 或返回 error 记为失败样本；窗口内失败率超阈值后
// 直接返回错误，不再打到下游。
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
`golang.org/x/crypto`、`golang.org/x/time`），按需引入；
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
| `openapi` | 从已注册路由快照生成 OpenAPI 3.1 文档与浏览页，支持分组、安全方案、命名模式、请求体与多响应 | `NewSpec`、`UseOperation`、`RegisterOpenAPI` |

### 会话与分片上传

```go
import (
	"gitee.com/leminewx/leego/session"
	"gitee.com/leminewx/leego/upload"
)

// 内存存储只适合单机；多实例部署需要实现 session.Store 换成共享存储。
store := session.NewMemoryStore()
defer store.Close() // 停掉后台清理协程

// 中间件负责读取 Cookie、恢复会话，并在请求结束时写回。
// UseSecure(true) 给 Cookie 打上 Secure 标记（仅 HTTPS 下发送），本地 http 调试传 false。
engine.UseMiddlewares(session.New(
	store,
	session.UseTTL(30*time.Minute),
	session.UseSecure(true),
))

engine.POST("/login", func(ctx *leego.Context) {
	// 会话由中间件放进 Request 的 context，因此这里总能取到。
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

	// 写回发生在请求结束时，这里只返回会话 ID 供客户端确认。
	_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": current.GetID()})
})
```

分片上传由同一个处理器按 `action` 分发，分片体直接读原始字节，
不经过 multipart 解析：

```go
// 本地存储把分片与 JSONL 索引写在 root 下，重启后可按索引恢复进度。
storage, err := upload.NewLocalStorage("./data")
if err != nil {
	log.Fatal(err)
}

// 上限 1 GiB、默认分片 4 MiB、最多 1024 片；分片体直接读原始字节。
handler := upload.NewHandler(storage,
	upload.UseMaxSize(1<<30),
	upload.UseDefaultPartSize(4<<20),
	upload.UseMaxParts(1024),
)

// 同一个处理器按 action 分发，下面四个方法都指向它：
// POST ?action=init | PUT ?action=part&id=..&index=..
// POST ?action=complete&id=.. | GET ?action=status&id=.. | DELETE ?action=abort&id=..
engine.POST("/upload", handler)
engine.PUT("/upload", handler)
engine.GET("/upload", handler)
engine.DELETE("/upload", handler)
```

### 任务调度、网关与链路追踪

```go
// Workers 是并发槽位上限；Start 之后才接受任务，Stop 只停止取任务，
// 不会主动取消下面这些任务收到的 ctx。
scheduler := job.NewScheduler(job.UseWorkers(4))
scheduler.Start(context.Background())
defer scheduler.Stop(context.Background())

// 周期任务返回取消函数；单个任务 panic 会被隔离，不影响调度器。
cancel := scheduler.ScheduleEvery(time.Minute, job.JobFunc(func(ctx context.Context) error {
	return refreshCache(ctx)
}))
defer cancel() // 取消该周期任务

// 一次性任务：延迟触发，执行完自动出队。
scheduler.ScheduleAfter(5*time.Second, job.JobFunc(func(context.Context) error {
	return warmUp()
}))

// GetStats 是快照：Runs 总执行数、Dropped 因槽位不足被丢弃、Pending 排队数。
stats := scheduler.GetStats()
```

```go
// 权重决定平滑加权轮询的比例：9:1 意味着大约十个请求里有一个走 canary。
stable, _ := url.Parse("http://127.0.0.1:9001")
canary, _ := url.Parse("http://127.0.0.1:9002")

balancer, err := gateway.NewBalancer(
	gateway.Upstream{URL: stable, Weight: 9, Tag: "stable"},
	gateway.Upstream{URL: canary, Weight: 1, Tag: "v2"},
)
if err != nil {
	log.Fatal(err)
}

// 超时是单次转发的上游预算；失败回调返回后若响应仍未写出，
// 框架会补一个 502 JSON（不回显上游地址，避免泄露内网拓扑）。
proxy := gateway.NewProxy(balancer,
	gateway.UseTimeout(3*time.Second),
	gateway.UseErrorHandler(func(ctx *leego.Context, target *url.URL, err error) {
		ctx.ResponseError(http.StatusBadGateway, "upstream unavailable")
	}),
)

// 挂到通配符上，该前缀下的任意子路径都交给代理处理。
engine.GET("/proxy/*path", proxy)
engine.POST("/proxy/*path", proxy)
// 请求头带 X-Leego-Canary: v2 时优先路由到标签为 v2 的上游。
```

```go
// 内存导出器只保留最近 1024 条，适合本地体验与调试。
exporter := trace.NewMemoryExporter(1024)
// 未采样的请求不生成 Span；FlushInterval 控制批量导出的节奏。
tracer := trace.NewTracer(exporter, trace.WithFlushInterval(time.Second))
engine.UseMiddlewares(tracer.AdaptToMiddleware())
// 关闭前刷新剩余 Span，否则最后一批数据会随进程退出丢失。
tracer.Shutdown(context.Background())
```

### 配置、校验与统一错误

```go
// 来源按顺序合并，后者覆盖前者：JSON 提供默认值，环境变量做部署期覆盖。
// 嵌套对象被展平为点号分隔的键，因此下面的 http.addr 才能取到。
settings, err := config.Load(
	config.NewJSONSource("config.json", config.UseWatchInterval(time.Second)),
	config.NewEnvSource("APP_"),
)
if err != nil {
	log.Fatal(err)
}
defer settings.Close() // 停止监听协程并取消全部订阅

addr := settings.GetString("http.addr", "127.0.0.1:8080")
// 回调在独立协程执行，只在值真正变化时触发，不阻塞配置加载。
settings.SubscribeChange("http.addr", func(value any) {
	log.Printf("监听地址变更: %v", value)
})

// 0 表示沿用来源自己的轮询间隔（默认 1 秒）；可重复调用但只有第一次生效。
if err := settings.Watch(0); err != nil {
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
	// 绑定失败属于请求格式问题，用 400 + RFC 7807 结构返回。
	if err := ctx.BindJSON(&request); err != nil {
		problem.New(http.StatusBadRequest, "invalid_json", err.Error()).Write(ctx)
		return
	}

	// 校验失败返回 422，problem 会把字段名与规则展开成 details。
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
	openapi.UseServer("https://api.example.com", "生产环境"), // 影响浏览页 cURL 的基地址
	openapi.UseTag("订单", "下单、查询与取消"), // 分组名 + 描述
	openapi.UseSchema("Order", Order{}), // 写入 components.schemas
	openapi.UseOperation("GET /orders/:id", openapi.Operation{
		Summary: "查询订单",
		Tags:    []string{"订单"},
		Responses: map[string]openapi.Response{
			"200": openapi.NewJSONRefResponse("订单详情", "Order"), // 引用上面的 UseSchema
			"404": openapi.NewJSONRefResponse("订单不存在", "Error"),
		},
	}),
)
```

| 端点 | 说明 |
| --- | --- |
| `GET <prefix>` | 文档浏览页（`go:embed` 内嵌，离线可用，无外部 CDN） |
| `GET <prefix>/openapi.json` | OpenAPI 3.1 文档 JSON |

路由模式会转成 OpenAPI 路径模板（`:id` → `{id}`、`*path` → `{path}`），
路径参数统一声明为 `string`；前缀为空时默认 `/openapi`。

- 路由的第二个标签（`engine.GET(pattern, handle, name, tag)`）会写入 `operation.tags`；
- `UseOperation` 的键兼容 `GET /a/:id`、`get /a/{id}` 与省略方法的 `/a/:id`，
  未匹配到路由的声明会被忽略，避免文档里出现不存在的接口；
- 大对象用 `UseSchema` + `NewJSONRefResponse` 生成 `$ref` 复用；
- 浏览页左侧按分组列出接口（支持方法筛选与搜索），右侧展示参数表、请求体、
  可展开的响应体、cURL 与原始 JSON。

---

## 指标与分析后台

### 采集

引擎为**每条路由**内置采集器，无需额外中间件。指标维度为「方法 + 模式 + 状态码」：

```go
// 报告是某一时刻的快照，可直接序列化给后台；Routes 已按方法 + 模式排序。
report := engine.CollectReport()
fmt.Printf("路由 %d 条，累计请求 %d 次\n", report.Totals.Routes, report.Totals.Requests)

// 也可以单独取快照，做自定义聚合。
for _, snapshot := range engine.GetAllHandlerSnapshots() {
	// Name / Method / Pattern / Tags / Enabled：路由标识与开关状态。
	// Flights 在途数、Total 累计响应、Failed 为 5xx 次数。
	// Metrics 按状态码升序，每项含 Code / Total / MaxCostMicro / MinCostMicro /
	// TotalCostMicro；成本单位均为微秒。
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
	dashboard.UseRefreshInterval(5*time.Second), // 前端自动刷新间隔
	dashboard.UseMaxRoutes(200), // 单次返回的最大路由数
	dashboard.EnableExport(true), // 开放 export.csv
	dashboard.UseAuthorizer(func(req *http.Request) bool {
		// 返回 false 时统一回 401；不配置 Authorizer 则端点完全开放，
		// 生产环境必须补上，或把端点限制在内网。
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
// window 为保留时长、buckets 为分桶数：1 小时 / 60 桶，每桶 1 分钟。
store := metrics.NewMemoryStore(time.Hour, 60)
defer store.Close()

// 每 5 秒采集一次引擎快照写入存储；同样必须在 Prepare() 之前注册。
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
	// 回调返回只读通道；ResponseSSE 会阻塞到通道关闭或客户端断开，
	// 通道关闭后自动向客户端补发一条 close 事件。
	_ = ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
		events := make(chan *sse.Message, 1)
		go func() {
			defer close(events) // 关闭通道即结束本次事件流
			select {
			case <-requestCtx.Done(): // 客户端断开或服务关闭，直接退出
			case events <- sse.NewMessageWithEvent("tick", []byte(`{"ok":true}`)):
			}
		}()
		return events
	})
})
```

### SSE：管理器托管多连接

```go
// 管理器统一托管多连接：心跳、广播、单发与优雅关闭都在这里。
manager := sse.NewManager()
manager.SetHeartbeatPeriod(10 * time.Second) // 默认 30 秒，调小便于观察
defer manager.Close()

// 错误通道必须有人消费，否则投递失败的错误会被丢弃。
go func() {
	for err := range manager.GetErrorChan() {
		log.Printf("SSE: %v", err)
	}
}()

engine.GET("/sse/:client", func(ctx *leego.Context) {
	// Serve 写响应头与 connect 事件后阻塞到客户端断开，因此不能放到后台协程。
	_ = manager.Serve(ctx.Response, ctx.Request, ctx.GetPathValue("client"))
})

// 广播给全部连接；按 ID 单发适合定向通知。
manager.Broadcast(sse.NewHeartbeatMessage())
manager.SendTo("client-1", sse.NewMessageWithEvent("notice", payload))
```

### WebSocket

```go
import (
	"gitee.com/leminewx/leego/response/websocket"
	"gitee.com/leminewx/leego/response/websocket/upgrader"
)

// 默认配置：读超时 60 秒、写超时 10 秒、单条消息上限 16 MiB，且只允许同源。
manager := websocket.NewManager(upgrader.NewDefaultConfig())
// Start 可选：只用于把管理器生命周期绑定到 ctx，连接本身独立运行。
go manager.Start(context.Background())

engine.GET("/ws", func(ctx *leego.Context) {
	// IsWebsocket 依据 Connection / Upgrade 头判断，普通 GET 请求直接跳过。
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

// 配置随被测地址一起生成，其余字段按需覆盖：并发 50、共 5000 次请求。
config := stress.NewDefaultConfig("http://127.0.0.1:8080/ping")
config.Concurrent = 50
config.TotalRequests = 5000
config.Timeout = 5 * time.Second
config.KeepAlive = true // 复用连接，避免把建链开销算进延迟

client, err := stress.NewClient(config)
if err != nil {
	log.Fatal(err)
}
defer client.Close()

// Run 阻塞到全部请求完成；请求按并发数均分，保证总数与配置一致。
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

打开 `http://127.0.0.1:8080/` 即为体验页面：左侧菜单按能力分组，右侧为对应功能区，
面板位置记录在 URL hash 中，窄屏下侧栏收起为抽屉。

| 入口 | 说明 |
| --- | --- |
| `/` | 体验前端（左侧菜单 + 右侧功能区，纯原生 JS，无外部 CDN） |
| `/dashboard` | 指标分析后台 |
| `/openapi` | OpenAPI 文档与浏览页 |
| `/metrics/prometheus` | Prometheus 文本指标 |
| `/api/traces` | 最近链路（内存导出器） |

作为库使用时，`harness.NewService` 返回可嵌入的 `*Service`：

```go
// 端口传 0 由内核分配，数据目录用临时目录：适合写集成测试。
service, err := harness.NewService(
	harness.UseAddr("127.0.0.1:0"),
	harness.UseDataDir(t.TempDir()),
	harness.UseLogging(false),
)
if err != nil {
	t.Fatal(err)
}
defer service.Shutdown(context.Background())

// GetHandler 返回已装配好的 http.Handler，可直接交给 httptest 或标准库服务。
server := httptest.NewServer(service.GetHandler())
defer server.Close()
```

`Service.Run(ctx)` 负责监听、提供服务，并在 `ctx` 取消后优雅关闭；
`GetEngine()` 返回底层引擎，可在其基础上继续注册业务路由。

---

## 启动与优雅关闭

```go
engine := leego.NewEngine()
// ... 注册路由与中间件（全部注册必须在首个请求前完成）

// 四种监听方式任选其一，都是阻塞调用，正常关闭时返回 nil：
//
//	engine.Run(":8080")
//	engine.RunTLS(":8443", "cert.pem", "key.pem")
//	engine.RunUnix("/tmp/leego.sock") / RunFd(3) / RunListener(listener)
//	http.ListenAndServe(":8080", engine.GetHTTPHandler())
//
// 注意：WaitSignalAndShutdown 只负责等信号，不负责启动服务；
// 需要同时监听时应把上面的调用放到单独协程里。

// 阻塞等待 SIGINT / SIGTERM；收到信号后先执行回调，再按 timeout 优雅关闭。
// 回调返回错误会直接返回，服务器不会被关闭。
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
| [docs/performance.md](docs/performance.md) | 性能基线：测试环境与方法、微基准与端到端结果、优化建议与局限 |
| [docs/harness.md](docs/harness.md) | 体验服务、前端页面、测试客户端与压测入口 |
| [docs/conventions.md](docs/conventions.md) | 命名、注释、错误处理与 API 命名迁移对照 |
| [docs/extension-roadmap.md](docs/extension-roadmap.md) | 基于现有架构的拓展方向：功能、方案、场景与可行性分析 |
| [docs/extension-design.md](docs/extension-design.md) | 拓展模块的实现方式与代码结构：包布局、接口签名、难点与 PR 切片 |
| [RELEASE.md](RELEASE.md) | 当前版本的行为变更、缺陷修复与升级步骤 |
| [ISSUES.md](ISSUES.md) | 能力边界、使用陷阱与后续规划 |

---

## 许可

本项目基于仓库中 `LICENSE` 文件所列条款发布。
