# Leego

Leego 是一个基于 Go 标准库 `net/http` 构建的轻量级 Web 框架，提供路由分组、中间件、上下文封装、
参数绑定、静态文件服务、优雅关闭等常用能力，并保持与 `http.Handler` 完全兼容。

> 版本：v0.10.23.0（要求 Go 1.25+）

---

## 目录

- [快速开始](#快速开始)
- [核心概念](#核心概念)
- [路由](#路由)
  - [路由注册](#路由注册)
  - [路由参数](#路由参数)
  - [通配符路由](#通配符路由)
  - [路由分组](#路由分组)
  - [静态文件服务](#静态文件服务)
  - [路由黑名单](#路由黑名单)
  - [启用 / 禁用路由](#启用--禁用路由)
- [Context 上下文](#context-上下文)
  - [请求参数](#请求参数)
  - [请求体绑定](#请求体绑定)
  - [Cookie](#cookie)
  - [响应方法](#响应方法)
  - [Header 与状态码](#header-与状态码)
- [中间件](#中间件)
  - [编写中间件](#编写中间件)
  - [与 http.Handler 互操作](#与-httphandler-互操作)
  - [内置中间件](#内置中间件)
- [启动与优雅关闭](#启动与优雅关闭)
- [可观测性](#可观测性)
- [完整示例](#完整示例)

---

## 快速开始

```go
package main

import (
	"net/http"

	"gitee.com/leminewx/leego"
)

func main() {
	engine := leego.NewEngine()

	engine.GET("/ping", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"message": "pong"})
	})

	// 监听并服务 HTTP 请求（默认不会产生竞态，标准库 net/http 驱动）
	if err := engine.Run(":8080"); err != nil {
		panic(err)
	}
}
```

访问 `http://localhost:8080/ping` 将得到：

```json
{"message":"pong"}
```

---

## 核心概念

| 类型 | 说明 |
| --- | --- |
| `leego.Engine` | 框架引擎，本身即一个路由组（内嵌 `*group`），负责路由注册、中间件编排、请求分发与服务生命周期管理。 |
| `leego.Context` | 每个请求对应一个上下文对象，封装了 `*http.Request` 与 `http.ResponseWriter`，并提供参数解析、绑定、响应等便捷方法。 |
| `leego.Group` | 路由组接口，用于在统一前缀和中间件下组织路由。 |
| `leego.HandleFunc` | 处理器函数类型：`func(ctx *leego.Context)`。 |
| `leego.MiddlewareFunc` | 中间件函数类型：`func(next leego.HandleFunc) leego.HandleFunc`。 |
| `leego.Handler` | 已注册路由的抽象，支持 `Enable()` / `Disable()` 与指标采集。 |
| `leego.JSON` | 响应 JSON 的便捷 map 类型：`map[string]any`。 |

`Engine` 实现了 `http.Handler` 接口，因此可直接挂载到任意标准库 HTTP Server，或配合 `GetHTTPHandler()` 在 HTTP/2（h2c）场景下使用。

---

## 路由

### 路由注册

支持全部标准 HTTP 方法：

```go
engine.GET("/users", listUsers)
engine.POST("/users", createUser)
engine.PUT("/users/:id", updateUser)
engine.PATCH("/users/:id", patchUser)
engine.DELETE("/users/:id", deleteUser)
engine.HEAD("/health", health)
engine.OPTIONS("/users", options)
```

每个注册方法都接受可选的 `tags ...string` 参数：第一个元素作为路由名称，后续作为标签（用于可观测性归类）。

### 路由参数

使用 `:name` 定义单段参数，通过 `ctx.GetPathValue("name")` 取值：

```go
engine.GET("/users/:id", func(ctx *leego.Context) {
	_ = ctx.ResponsePlain(http.StatusOK, "用户 ID = "+ctx.GetPathValue("id"))
})
```

### 通配符路由

使用 `*` 匹配剩余多级路径（默认参数名为 `filepath`）：

```go
engine.GET("/files/*", func(ctx *leego.Context) {
	_ = ctx.ResponsePlain(http.StatusOK, "路径 = "+ctx.GetPathValue("filepath"))
})
```

> 访问 `/files/a/b/c.txt` 时 `ctx.GetPathValue("filepath")` 返回 `a/b/c.txt`。

### 路由分组

通过 `Group(prefix)` 创建带统一前缀的路由组，并可为该组单独挂载中间件：

```go
api := engine.Group("/api/v1")
api.UseMiddlewares(authMiddleware)
api.GET("/profile", getProfile)
// 实际路由：GET /api/v1/profile
```

分组支持嵌套，父组的路由前缀与中间件会被子组继承。

### 静态文件服务

```go
// 基于本地目录
engine.SetStatic("/static", "./public")

// 基于 http.FileSystem
engine.SetStaticFS("/assets", http.Dir("./assets"))
```

请求 `/static/foo.css` 会映射到 `./public/foo.css`。框架内置路径遍历防护，自动拦截 `..`、`\\`、`NUL` 及绝对路径等非法访问。

### 路由黑名单

将指定路径加入黑名单后，访问会直接返回 `403 Forbidden`：

```go
engine.AddURLBlacklist("/admin", "/internal")
```

### 启用 / 禁用路由

通过 `GetHandler` 取出已注册处理器，调用 `Disable()` / `Enable()` 动态控制其可用性：

```go
if h, ok := engine.GetHandler(http.MethodGet, "/maintenance"); ok {
	h.Disable() // 禁用后访问返回 423 Locked
}
```

---

## Context 上下文

### 请求参数

```go
ctx.GetQueryValue("id")          // URL 查询参数 ?id=xxx
ctx.GetFormValue("name")         // URL 参数 + x-www-form-urlencoded
ctx.GetPostFormValue("name")     // 仅 x-www-form-urlencoded 表单
ctx.GetMultipartFormValue("name")// multipart/form-data 表单字段
ctx.GetPathValue("id")           // 路由参数 / 通配符
```

### 请求体绑定

```go
var payload User
if err := ctx.BindJSON(&payload); err != nil {
	ctx.ResponseError(http.StatusBadRequest, "invalid json")
	return
}

var doc Config
_ = ctx.BindXML(&doc)

raw, _ := ctx.GetRequestBody()
```

文件上传：

```go
file, header, err := ctx.GetFormFile("avatar")
// 或直接保存到磁盘
_ = ctx.SaveUploadedFile(header, "./uploads/avatar.png")
```

### Cookie

```go
ctx.SetCookieData("sid", "abc123", 3600, "/", "", false, true)
ctx.SetSameSite(http.SameSiteLaxMode)

value, err := ctx.GetCookieValue("sid")
```

### 响应方法

| 方法 | 说明 |
| --- | --- |
| `ResponseJSON(code, data)` | 写入 `application/json`，返回 error |
| `ResponsePlain(code, text)` | 写入 `text/plain` |
| `ResponseXML(code, data)` | 写入 `text/xml` |
| `ResponseHTML(file, tmplName, data)` | 渲染 HTML 模板文件 |
| `ResponseBytes(code, data)` | 写入原始字节 |
| `ResponseError(code, msg)` | 使用 `http.Error` 返回错误 |
| `ResponseRedirect(code, location)` | 重定向（code 需为 3xx 或 201） |
| `ResponseFileContent(path)` | 在浏览器中展示文件 |
| `ResponseFileAttachment(path)` | 作为附件下载 |
| `ResponseSSEvent(event)` | 发起 SSE 事件流（见 SSE 章节） |

示例：

```go
_ = ctx.ResponseJSON(http.StatusCreated, leego.JSON{
	"id":   1,
	"name": "leego",
})

ctx.ResponseRedirect(http.StatusFound, "/login")
```

### Header 与状态码

```go
ctx.SetStatusCode(http.StatusAccepted)
ctx.GetStatusCode()

ctx.SetRequestHeader("X-Trace-Id", "123")
ctx.GetRequestHeader("Authorization")
ctx.SetResponseHeader("X-Powered-By", "Leego")
ctx.GetResponseHeader("Content-Type")

ctx.GetClientIP()        // 自动识别 X-Forwarded-For / X-Real-IP / RemoteAddr
ctx.GetContentType()
ctx.IsWebsocket()
```

---

## 中间件

### 编写中间件

中间件为签名 `func(next leego.HandleFunc) leego.HandleFunc` 的函数，通过 `UseMiddlewares` 挂载到引擎或路由组：

```go
func authMiddleware(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		if ctx.GetRequestHeader("Authorization") == "" {
			ctx.ResponseError(http.StatusUnauthorized, "unauthorized")
			return // 不调用 next，请求在此中断
		}
		next(ctx) // 调用 next 以继续执行后续处理器
	}
}

engine.UseMiddlewares(authMiddleware)
```

中间件按注册顺序形成责任链：引擎级中间件 → 父组中间件 → 子组中间件 → 路由处理器。

### 与 http.Handler 互操作

```go
import "gitee.com/leminewx/leego/middleware"

// 将 http.Handler 包装为 Leego 中间件
engine.UseMiddlewares(middleware.WarpFromHTTPHandler(myHTTPHandler))

// 将 Leego 中间件解包为标准库 http.Handler
stdHandler := middleware.UnwarpFromMiddlewareFuncToHTTPHandler(myMiddleware)
```

### 内置中间件

| 包 | 用途 | 主要入口 |
| --- | --- | --- |
| `middleware/recovery` | 捕获 panic 并返回 500，避免进程崩溃 | `recovery.Recover()`、`recovery.RecoverWithConfig(cfg)` |
| `middleware/logger` | 彩色请求日志（方法 / 状态码 / 延迟） | `logger.Log()`、`logger.LogWithConfig(cfg)` |
| `middleware/cors` | 跨域资源共享控制 | `cors.NewDefaultCORS()` |
| `middleware/breaker` | 熔断器（失败率熔断 + 半开探测） | `breaker.NewCircuitBreaker(failure, success, timeout)` |
| `middleware/limiter` | 限流（固定窗口 / 滑动窗口 / 令牌桶） | `limiter.NewFixedWindowLimiter`、`NewSlidingWindowLimiter`、`NewTokenBucketLimiter` |
| `middleware/monitor` | 处理器指标采集（QPS / 延迟 / 错误） | `monitor.Collect(collector)` |
| `middleware/jwt` | JWT 签发与解析 | `jwt.NewHMACSigner`、`jwt.NewParser` |

示例：

```go
import (
	"time"

	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/breaker"
	"gitee.com/leminewx/leego/middleware/cors"
	"gitee.com/leminewx/leego/middleware/limiter"
	"gitee.com/leminewx/leego/middleware/recovery"
)

engine.UseMiddlewares(
	recovery.Recover(),
	middleware.WarpFromHTTPHandler(cors.NewDefaultCORS().GetMiddleware()),
	limiter.NewTokenBucketLimiter(100, 200).GetMiddleware(),
)

cb := breaker.NewCircuitBreaker(5, 3, 10*time.Second)
engine.GET("/risky", func(ctx *leego.Context) {
	_ = cb.ExecuteWithContext(ctx, func(c *leego.Context) error {
		// 业务调用
		return nil
	})
})
```

> CORS 实例需调用 `Enable()` 后才生效；`NewDefaultCORS()` 已内置启用。

---

## 启动与优雅关闭

Leego 提供多种监听方式，并支持基于系统信号的优雅关闭：

```go
engine := leego.NewEngine()
// ... 注册路由与中间件

// 方式一：普通 HTTP
go func() {
	if err := engine.Run(":8080"); err != nil {
		panic(err)
	}
}()

// 方式二：HTTPS
// _ = engine.RunTLS(":8443", "cert.pem", "key.pem")

// 方式三：Unix Socket / 文件描述符 / 自定义 Listener
// _ = engine.RunUnix("/tmp/leego.sock")
// _ = engine.RunListener(myListener)

// 等待 SIGINT / SIGTERM，超时 10s 内优雅关闭
if err := engine.WaitSignalAndShutdown(10*time.Second, func() error {
	// 关闭前回调，例如释放数据库连接
	return nil
}); err != nil {
	panic(err)
}
```

如需在 HTTP/2（h2c，明文 HTTP/2）场景使用：

```go
engine.UseH2C = true
http.ListenAndServe(":8080", engine.GetHTTPHandler())
```

---

## 可观测性

框架内建处理器级指标采集（`collector`），可配合 `monitor.Collect` 中间件使用：

```go
import (
	"gitee.com/leminewx/leego/middleware/collector"
	"gitee.com/leminewx/leego/middleware/monitor"
)

engine.UseMiddlewares(monitor.Collect(collector.NewCollector()))

// 运行时获取所有处理器的指标快照
collectors := engine.GetAllHandlerCollectors()
for _, c := range collectors {
	// c.GetRequestCount() / c.GetSuccessCount() / c.GetErrorCount()
	// c.GetAvgCost() / c.GetMaxCost() / c.GetMinCost()
}
```

启动时会打印路由构建信息（创建 / 重复），便于核对路由表。

---

## 完整示例

```go
package main

import (
	"net/http"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middleware"
	"gitee.com/leminewx/leego/middleware/recovery"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	engine := leego.NewEngine()
	engine.MaxMultipartMemory = 16 << 20 // 限制 multipart 表单内存占用

	// 全局中间件
	engine.UseMiddlewares(recovery.Recover())

	// 路由组：/api/v1
	v1 := engine.Group("/api/v1")
	v1.GET("/users/:id", func(ctx *leego.Context) {
		id := ctx.GetPathValue("id")
		_ = ctx.ResponseJSON(http.StatusOK, User{ID: 1, Name: id})
	})

	// 静态资源
	engine.SetStatic("/static", "./public")

	// 黑名单
	engine.AddURLBlacklist("/secret")

	go func() {
		if err := engine.Run(":8080"); err != nil {
			panic(err)
		}
	}()

	_ = engine.WaitSignalAndShutdown(10*time.Second, nil)
}
```

---

## 许可

本项目基于 LICENSE 文件中的条款发布。
