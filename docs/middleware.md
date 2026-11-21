# 中间件

中间件负责把「横切关注点」从业务处理器中剥离：日志、恢复、跨域、限流、熔断、
鉴权等。Leego 的中间件模型与 `net/http` 的 `func(http.Handler) http.Handler`
同构，只是操作对象换成了 `*leego.Context`。

---

## 模型

```go
type HandleFunc func(ctx *Context)
type MiddlewareFunc func(next HandleFunc) HandleFunc
```

一个中间件接收「后续处理链」，返回「新的处理链」：

```go
func timing(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		start := time.Now()
		next(ctx)                                    // 调用后续链路
		log.Printf("cost=%s", time.Since(start))     // 后置逻辑
	}
}
```

### 顺序

注册顺序即执行顺序（洋葱模型）：

```
engine.UseMiddlewares(A, B)
api := engine.NewGroup("/api")
api.UseMiddlewares(C)
api.GET("/x", handler)

请求 /api/x 时：A → B → C → handler
```

- 引擎级中间件包裹**所有**请求，包括 404 / 405 / 403 / 423；
- 组级中间件只作用于该组注册的路由；
- 同一路由的组级中间件在全局链之后执行；
- 中间件链在 `Prepare()` 阶段折叠一次，之后不再重新组合。

### 中断与放行

- 不调用 `next(ctx)`：请求在此终止，后续链路不执行；
- 调用 `next(ctx)`：继续执行；
- 调用 `next(ctx)` 之后仍可写响应（例如统一改写响应头），但状态码与响应体若已
  写出则无法回滚。

```go
func auth(next leego.HandleFunc) leego.HandleFunc {
	return func(ctx *leego.Context) {
		if ctx.GetRequestHeader("Authorization") == "" {
			ctx.ResponseError(http.StatusUnauthorized, "缺少凭据")
			return
		}
		next(ctx)
	}
}
```

---

## 挂载位置

```go
engine.UseMiddlewares(globalMiddleware)   // 全局
api := engine.NewGroup("/api")
api.UseMiddlewares(groupMiddleware)       // 组级
```

路由级中间件通过「单条路由的处理器包装」实现：

```go
engine.GET("/admin", auth(adminHandler))
```

> 路由级包装必须在注册前完成；注册后再追加全局中间件不会生效（链已冻结）。

---

## 与 http.Handler 互操作

```go
import "gitee.com/leminewx/leego/middlewares"

// 标准库处理器 → 中间件（先执行标准处理器，再进入后续链）
engine.UseMiddlewares(middlewares.WrapFromHTTPHandler(pprofHandler))
engine.UseMiddlewares(middlewares.WrapFromHTTPHandlerFunc(myHandlerFunc))

// 中间件 → 标准库处理器（只关心中间件副作用时使用）
stdHandler := middlewares.UnwrapFromMiddlewareFuncToHTTPHandler(myMiddleware)
stdHandlerFunc := middlewares.UnwrapFromMiddlewareFuncToHTTPHandleFunc(myMiddleware)
```

注意两种解包的语义差异：

- `WrapFromHTTPHandler` 会**继续**调用 `next`，适合「旁路处理」；
- `UnwrapFromMiddlewareFuncToHTTPHandler` 使用空处理器作为链尾，适合只做指标
  采集、访问日志等副作用；它无法感知业务响应。

---

## 内置中间件

| 包 | 构造器 | 说明 |
| --- | --- | --- |
| `middlewares` | `NewLogger()` / `NewLoggerWithConfig(cfg)` | 访问日志 |
| `middlewares` | `NewRecovery()` / `NewRecoveryWithConfig(cfg)` | panic 恢复 |
| `middlewares` | `NewMonitor(collector)` | 为手工挂载的处理器补充指标 |
| `middlewares/cors` | `New()` | 跨域 |
| `middlewares/jwt` | `NewHMACSigner` / `NewParser` | JWT 签发与解析工具（本身不是中间件，见 [jwt.md](jwt.md)） |
| `middlewares/limiter` | 三种限流构造器 | 限流（见 [limiter-breaker.md](limiter-breaker.md)） |
| `middlewares/breaker` | `NewCircuitBreaker(config)` | 熔断（同上） |
| `middlewares/cache` | `New(cache.Config{...})` | 响应缓存，默认旁路 Cookie 请求（见 README「拓展能力」） |
| `middlewares/compress` | `New(compress.Config{...})` | gzip 压缩，按内容类型与阈值判定；`q=0`（含 `*;q=0`）视为客户端拒绝 |
| `middlewares/trace` | `NewTracer(exporter, ...)` | W3C `traceparent` 传播与异步导出（同上） |
| `middlewares/session` | `New(store, options...)` | 服务端会话：签名 Cookie + 可替换存储，支持轮换与销毁；panic 路径同样持久化，Hijack 前同步 Set-Cookie |

所有构造器都只创建实例（结构体或接口），实例通过 `AdaptToMiddleware()` 适配为
`leego.MiddlewareFunc` 后才能挂载；包装与解包工具函数除外。这样实例可以在多处复用
（同一限流器挂多个路由组、同一熔断器同时用于 `Execute` 与中间件）。

### 目录约定

- 中间件能力一律放在 `middlewares/`：根包为日志、恢复、指标采集，子包为
  `cors`、`limiter`、`breaker`、`cache`、`compress`、`trace`、`session`；
  新增中间件不得放在 `middlewares/` 之外的包。
- 对外可挂载的中间件必须由实例提供 `AdaptToMiddleware()`，不直接导出 `MiddlewareFunc`
  常量或函数；`WrapFromHTTPHandler` / `UnwrapFromMiddlewareFuncToHTTPHandler` 属于与
  `http.Handler` 互操作的工具，不返回中间件实例，因此不受此约束。
- 只为中间件服务但不是中间件的包：`middlewares/collector`（指标快照与报告聚合）、
  `middlewares/jwt`（令牌签发与解析）。它们不提供 `AdaptToMiddleware`，文档与示例不得
  把它们当作处理器或中间件直接挂载。

### 访问日志

```go
engine.UseMiddlewares(middlewares.NewLogger().AdaptToMiddleware())

engine.UseMiddlewares(middlewares.NewLoggerWithConfig(&middlewares.LoggerConfig{
	Output:          os.Stdout,
	WithColor:       true,
	WithQuery:       true,
	SkipAbsolutePaths: map[string]struct{}{
		"/metrics":  {},
		"/healthz":  {},
		"/favicon.ico": {},
	},
}).AdaptToMiddleware())
```

`LoggerConfig` 字段：

| 字段 | 说明 |
| --- | --- |
| `Output` | 输出目标，默认 `os.Stdout` |
| `WithColor` | 是否使用 ANSI 颜色；输出到文件时建议关闭 |
| `WithQuery` | 是否输出查询串 |
| `SkipAbsolutePaths` | 跳过的绝对路径集合（健康检查、指标、图标等） |

日志包含：方法、路径、状态码、耗时、客户端 IP。状态码直接取自
`ctx.GetStatusCode()`，如果处理器未写响应且未显式设置状态码，日志中的状态码为
`0`（此时 `net/http` 会补 `200`，但日志不会代为修正）。
颜色开关为「显式配置优先」：`WithColor` 为 `true` 时总是上色；为 `false` 时会
根据输出目标是否为终端自动判定。重定向到文件时应确认已关闭颜色。

### 恢复

```go
engine.UseMiddlewares(middlewares.NewRecovery().AdaptToMiddleware())

engine.UseMiddlewares(middlewares.NewRecoveryWithConfig(&middlewares.RecoveryConfig{
	Output:          os.Stderr,
	DumpRequest:     true,
	DumpRequestBody: false,
	HandleFunc: func(ctx *leego.Context, recovered any) {
		log.Printf("panic: %v", recovered)
		_ = ctx.ResponseJSON(http.StatusInternalServerError, leego.JSON{"error": "internal"})
	},
}).AdaptToMiddleware())
```

| 字段 | 说明 |
| --- | --- |
| `Output` | 堆栈输出目标，默认 `os.Stdout` |
| `DumpRequest` | 是否转储请求行与头；`Authorization`、`Cookie`、`Set-Cookie`、`X-Api-Key` 等敏感头自动替换为 `[REDACTED]` |
| `DumpRequestBody` | 是否转储请求体（可能包含敏感信息，默认关闭） |
| `HandleFunc` | 自定义收尾逻辑；为 `nil` 时返回 `500` 纯文本 |

恢复中间件只捕获其后链路上的 panic。引擎在请求入口还有一道兜底：即使没有挂
`NewRecovery()`，panic 也会记录堆栈并在响应尚未开始时返回 500（`http.ErrAbortHandler`
仍按标准库语义上抛）。若需要覆盖「路由匹配阶段」的 panic、输出请求转储或自定义
收尾，仍应把 `NewRecovery()` 注册为全局中间件（推荐做法）。

请求转储在克隆出的 Header 上脱敏，不会改动原始请求头；脱敏后的占位符为
`[REDACTED]`，可安全地把日志交给集中式平台。

### 指标采集补充

```go
thirdParty := collector.New(http.MethodGet, "/debug/vars")
engine.UseMiddlewares(middlewares.NewMonitor(thirdParty).AdaptToMiddleware())
```

引擎已为每条注册路由内建采集器，**不要**再叠加 `NewMonitor`，否则同一请求会被
计数两次。它只用于框架外挂载的处理器。

### CORS

```go
corsInstance := cors.New()
corsInstance.SetAllowOrigins("https://app.example.com")
corsInstance.SetAllowMethods(http.MethodGet, http.MethodPost)
corsInstance.SetAllowHeaders("Content-Type", "Authorization")
corsInstance.SetExposeHeaders("X-Request-Id")
corsInstance.SetAllowCredentials(true)
corsInstance.SetMaxAge(3600)

engine.UseMiddlewares(corsInstance.AdaptToMiddleware())
```

默认值：

| 项 | 默认 |
| --- | --- |
| 启用 | 是 |
| 来源 | `*`（任意来源） |
| 方法 | 常用方法集合 |
| 凭证 | 关闭 |
| `Max-Age` | 86400 秒 |

安全规则：

- `SetAllowOrigins()` 无参数、包含 `*` 或全部为空 → 视为允许任意来源，并**强制
  关闭凭证**（浏览器禁止 `*` 与凭证共存）；
- 配置具体来源时**不会**自动开启凭证，需要显式
  `SetAllowCredentials(true)`；
- 具体来源会写入 `Vary: Origin`，避免缓存串味；
- 预检请求（`OPTIONS` + `Origin` + `Access-Control-Request-Method`）由中间件直接
  响应：来源不允许 → `403`；方法不允许 → `405`；通过 → `204`；
- 普通请求遇到不允许的来源时**不拒绝**，只是不下发 CORS 头，由浏览器拦截。这样
  同源请求与命令行调用不会受到误伤。

---

## 组合示例

```go
rateLimiter := limiter.NewTokenBucketLimiter(100, 200, limiter.Config{WaitTimeout: 3 * time.Second})

engine.UseMiddlewares(
	middlewares.NewLogger().AdaptToMiddleware(),
	middlewares.NewRecovery().AdaptToMiddleware(),
	cors.New().AdaptToMiddleware(),
	rateLimiter.AdaptToMiddleware(),
)
```

推荐顺序与理由：

1. `NewLogger` 放在最外层，能记录后续中间件产生的错误响应；
2. `NewRecovery` 紧随其后，覆盖其余中间件与业务处理器；
3. `CORS` 在限流之前，保证 429 响应也带跨域头；
4. 限流放在业务之前、日志与恢复之后，429 依然可观测。

---

## 编写建议

- **只做一件事**：把鉴权、限流、日志分离，避免单个中间件分支过多；
- **不要吞掉错误**：用 `ctx.AddError` / `ctx.Fail` 记录，方便日志与错误链排查；
- **避免持有 `*Context`**：请求结束后上下文会被复用；
- **注意响应已发送**：调用 `next` 后若要写响应，先用 `GetStatusCode()` 判断；
- **长连接中间件要谨慎**：SSE / WebSocket 会长时间占用协程，超时类中间件需显式
  放行（例如按路径跳过）。

---

## 测试中间件

中间件可以脱离引擎单独测试：

```go
func TestAuthRejectsMissingToken(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx := leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	called := false
	auth(func(*leego.Context) { called = true })(ctx)

	if called {
		t.Fatal("缺少凭据时不得进入业务处理器")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, want 401", recorder.Code)
	}
}
```

需要并发断言时，请使用带互斥锁的响应记录器，避免 `-race` 报告数据竞争。
