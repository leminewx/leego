# Context 上下文

`Context` 是每个请求的状态容器，封装了请求、响应、路由参数、错误链、模板与
JWT 声明等能力。本文按使用场景说明各组方法的行为与约束。

> `Context` 与 `response.Writer` 均为池化对象，**禁止跨请求持有**。请求结束后
> 它们会被清空并放回池中。

---

## 结构

```go
type Context struct {
	Request  *http.Request     // 原始请求
	Response response.Writer   // 响应包装器（记录状态码与字节数）
	// 其余字段均为私有，通过方法访问
}
```

`NewContext(resp, req)` 用于在框架之外构造上下文（例如把中间件解包成
`http.Handler` 时）；它不会绑定引擎，因此依赖引擎的能力（模板、错误处理器、
请求体上限）会退化为默认行为。

---

## 元信息

```go
ctx.GetStartTime()    // 上下文创建时间
ctx.GetServiceName()  // 引擎名称
ctx.GetEngine()       // 所属引擎，可能为 nil
ctx.GetRemoteIP()     // TCP 对端地址（Host 部分）
ctx.GetClientIP()     // 可信代理感知的客户端 IP
ctx.GetURL()          // 缓存的 *url.URL
```

`GetClientIP()` 的解析顺序：

1. 直连对端地址（`RemoteAddr`）；对端不在 `TrustedProxies` 内时直接返回它；
2. 仅当对端可信时，读取 `X-Forwarded-For`（按逗号拆分）与 `X-Real-IP`，
   合并后**从右向左**遍历，跳过可信代理，返回第一个不可信地址；
3. 全部地址都可信或解析失败时，回退到对端地址。

> 可信代理默认为空，因此默认**不会**信任任何转发头。部署在 Nginx / 网关之后时
> 必须显式配置 `WithTrustedProxies` 或 `SetTrustedProxies`，否则基于 IP 的审计与
> 限流可以被伪造头绕过。

---

## 请求参数

| 方法 | 数据来源 |
| --- | --- |
| `GetPathValue(name)` | 路由参数（`:name` / `*name`） |
| `GetPathValues()` | 全部路由参数 |
| `GetQueryValue(key)` | URL 查询串 |
| `GetFormValue(key)` | 查询串 + `x-www-form-urlencoded` 请求体 |
| `GetPostFormValue(key)` | 仅请求体中的表单字段 |
| `GetMultipartFormValue(key)` | `multipart/form-data` 字段 |
| `GetFormFile(field)` | `multipart/form-data` 文件 |

```go
id := ctx.GetPathValue("id")
keyword := ctx.GetQueryValue("q")
avatar, header, err := ctx.GetFormFile("avatar")
if err == nil {
	defer avatar.Close() // 调用方负责关闭
}
```

解析表单会惰性触发 `ParseForm` / `ParseMultipartForm`；multipart 的内存上限来自
引擎配置（默认 32 MiB）。

---

## 请求体

```go
raw, err := ctx.ReadBody()      // 读取并缓存
cached, _ := ctx.GetRequestBody() // 只读取缓存
```

行为细节：

- 首次调用 `ReadBody` 才会真正读取 `Request.Body`；
- 结果（含错误）被缓存，后续调用返回同一份数据或错误；
- 引擎设置 `MaxBodyBytes` 时使用 `http.MaxBytesReader` 限制读取量，超限返回
  `*http.MaxBytesError`；交给 `ctx.Fail` 时会自动映射为 `413` +
  `request_entity_too_large`，无需调用方手动判断错误类型；
- 不会关闭 `Request.Body`，由 `net/http` 负责关闭；
- 缓存缓冲在上下文复位时保留容量，但超过 64 KiB 时直接丢弃，避免一次大请求体
  长期占用对象池。

因此可以「先绑定，再读取原始字节」：

```go
var payload Order
if err := ctx.BindJSON(&payload); err != nil { /* ... */ }
raw, _ := ctx.ReadBody() // 仍然可用
```

---

## 绑定

```go
ctx.Bind(&payload)      // 按 Content-Type 自动分派
ctx.BindJSON(&payload)  // json（含 +json 子类型）
ctx.BindXML(&payload)   // application/xml、text/xml
ctx.BindForm(&payload)  // urlencoded 与 multipart
```

`Bind` 的分派规则：

| Content-Type 包含 | 使用 |
| --- | --- |
| `json` | `BindJSON` |
| `xml` | `BindXML` |
| `x-www-form-urlencoded` | `BindForm` |
| `multipart/form-data` | `BindForm` |
| 其他 | 返回 `ErrUnsupportedMediaType` |

约束：

- 目标必须是**非 nil 指针**，否则返回错误（不会 panic）；
- `WithStrictJSON(true)` 时 JSON 解码使用 `DisallowUnknownFields`；
- `BindForm` 支持 `string`、`bool`、整数、浮点数及其切片，字段通过 `form`
  标签匹配，未声明标签时使用字段名；
- 绑定失败不会写入响应，错误处理由调用方决定。

---

## 响应

### 状态码与 Header

```go
ctx.SetStatusCode(http.StatusAccepted)
status := ctx.GetStatusCode() // 未写入时为 0

ctx.SetResponseHeader("X-Trace-Id", "abc")
ctx.GetResponseHeader("X-Trace-Id")
ctx.DeleteResponseHeader("X-Trace-Id")

ctx.SetRequestHeader("X-Tenant", "t-1")
ctx.GetRequestHeader("X-Tenant")
ctx.DeleteRequestHeader("X-Tenant")
```

### 文本类响应

```go
ctx.ResponseJSON(http.StatusOK, payload)
ctx.ResponseXML(http.StatusOK, payload)
ctx.ResponseText(http.StatusOK, "ok")
ctx.ResponseHTML(http.StatusOK, "<h1>hi</h1>")
ctx.ResponseBytes(http.StatusOK, raw)
```

| 方法 | Content-Type |
| --- | --- |
| `ResponseJSON` | `application/json; charset=utf-8` |
| `ResponseXML` | `application/xml; charset=utf-8` |
| `ResponseText` | `text/plain; charset=utf-8` |
| `ResponseHTML` | `text/html; charset=utf-8` |
| `ResponseBytes` | 由调用方自行设置的 Header 决定 |

`ResponseJSON` / `ResponseXML` / `ResponseHTML` / `ResponseBytes` 返回 `error`，便于记录
序列化失败；`ResponseText` 同样返回 `error`。

### 错误响应

```go
ctx.ResponseError(http.StatusBadRequest, "参数缺失")   // 纯文本 + 状态码
ctx.Fail(errors.New("下游不可用"))                  // 交给统一错误处理器
```

`ResponseError` 的行为：

1. `message` 为空时使用 `http.StatusText(code)`；
2. 总是把错误写入错误链（`GetErrors()` 可查）；
3. 若状态码已写入（`GetStatusCode() != 0`）则不再写响应，避免「头已发送」的
   无效写入；
4. 否则调用 `http.Error` 输出纯文本。

`Fail` 的行为：写入错误链 → 引擎自定义 `ErrorHandlerFunc` → 默认 JSON
`{"error":{"code":"internal_error","message":"Internal Server Error"}}`。

- 默认实现**不回显 `err.Error()`**，避免把数据库地址、文件路径等内部细节返回给
  客户端；原始错误仍保留在错误链与日志中；
- `*HTTPError` 使用其 `Status` / `Code` / `Message`；`Message` 为空时回落到
  `http.StatusText(status)`；
- `*http.MaxBytesError` 自动映射为 `413` + `request_entity_too_large`；
- 响应已写入时只记录错误，不再改变响应内容。

### 重定向与文件

```go
ctx.ResponseRedirect(http.StatusFound, "/login") // code 必须在 300–308，否则 panic

ctx.ResponseFile("./public/index.html")       // 浏览器内展示
ctx.ResponseAttachment("./export.csv")            // 附件下载
ctx.ResponseFileFromFS("index.html", http.Dir("./public"))
ctx.ResponseAttachmentFromFS("export.csv", http.Dir("./export"))
```

附件下载的 `Content-Disposition` 会对非 ASCII 文件名使用 RFC 5987 编码，避免
中文文件名乱码；文件名校验只取路径最后一段（兼容 Windows 反斜杠），
并转义双引号与反斜杠，防止响应头注入。

---

## 模板

```go
engine.LoadHTMLGlob("templates/*.html")
engine.LoadHTMLFiles("templates/base.html", "templates/home.html")
engine.SetTemplateFuncs(template.FuncMap{"upper": strings.ToUpper})

engine.GET("/home", func(ctx *leego.Context) {
	if err := ctx.ResponseTemplate("home.html", data); err != nil {
		// 模板错误可能包含文件路径，只写日志，对客户端返回固定文案。
		ctx.ResponseError(http.StatusInternalServerError, "模板渲染失败")
	}
})

engine.GET("/debug", func(ctx *leego.Context) {
	_ = ctx.ResponseTemplateFile("templates/debug.html", "", data) // 不缓存，适合调试
})
```

- `LoadHTMLGlob` / `LoadHTMLFiles` 是**追加**语义，可多次调用；
- `ResponseTemplate` 使用已加载的模板集合，未加载时返回错误；
- `ResponseTemplateFile` 每次解析文件，不参与缓存，高频场景应使用 `ResponseTemplate`；
- 所有模板渲染统一 `Content-Type: text/html; charset=utf-8`。

---

## Cookie

```go
ctx.SetCookieData("sid", "abc123", 3600, "/", "", true, true)
ctx.SetSameSite(http.SameSiteLaxMode) // 作用于后续 SetCookieData / SetCookie

value, err := ctx.GetCookieValue("sid")
```

- `cookiePath` 为空时默认 `/`；
- Cookie 值写入时经过 `url.PathEscape`，读取时由 `url.PathUnescape` 解码，两者对称：
  空格与 `+` 都能原样往返，`+` 始终是字面量而不是空格；
- `GetCookieValue` 遇到非法转义时原样返回 Cookie 值，不会静默变成空串；
- `SetCookie(cookie)` 可直接写入完整的 `*http.Cookie`（SameSite 由调用方设置）。

---

## 错误链

```go
ctx.AddError(errors.New("校验失败")) // nil 会被忽略
errs := ctx.GetErrors()              // 按追加顺序
last := ctx.GetLastError()           // 无错误时为 nil
```

`GetErrors()` 返回内部切片引用，调用方**不得修改**。错误链适合在中间件中收集
多段错误（如「签名无效 + 过期」）后统一输出。

---

## 派生上下文

```go
derived := ctx.DeriveContext(context.WithValue(ctx.Request.Context(), key, value))
if v, ok := derived.GetContextValue(key); ok {
	// 使用 v
}
```

`DeriveContext` 基于 `context.Context` 创建新的 `Context`：请求被 `WithContext`
包装，错误链与路由参数被深拷贝，**响应写入器与引擎与原上下文共享**。它适合在
协程中继续读取请求元信息；不要用它向同一个客户端并发写响应，也不要把新上下文
长期持有（它不参与对象池）。

---

## JWT 声明

```go
ctx.SetJWTClaims(claims)         // 认证中间件写入
claims, ok := ctx.GetJWTClaims() // 业务处理器读取
```

声明以 `any` 保存，读取时需自行断言类型：

```go
if claims, ok := ctx.GetJWTClaims().(*jwt.StandardClaims); ok {
	log.Println(claims.Subject)
}
```

---

## 长连接

```go
err := ctx.ResponseSSE(func(requestCtx context.Context) <-chan *sse.Message {
	// 返回事件通道；通道关闭后连接结束
})
```

`ResponseSSE` 会同步阻塞直到连接结束。处理器必须等待它返回，**不能提前返回**，
否则响应写入器会被归还池中，连接被意外关闭。

引擎默认 `WriteTimeout` 为 60 秒，到时 `net/http` 会切断长连接；启用 SSE /
WebSocket 的服务应使用 `leego.WithWriteTimeout(0)` 关闭写入超时。详见
[docs/sse-websocket.md](sse-websocket.md)。

---

## 检测辅助

```go
ctx.GetContentType() // 去除参数后的媒体类型，如 application/json
ctx.IsWebsocket()    // Upgrade: websocket 且 Connection 包含 upgrade
```

`IsWebsocket()` 只做协议头判断，不做握手；实际升级见 WebSocket 章节。

---

## 常见问题

**Q：为什么 `GetStatusCode()` 返回 0？**
状态码只在显式调用 `SetStatusCode` 或写入器实际写出响应时更新。若处理器什么
都没写，`net/http` 会补 `200`，但上下文中仍是 0。

**Q：`ResponseError` 之后再 `ResponseJSON` 会怎样？**
第二次写入会尝试设置 Header，但状态码与头已发送，客户端只会收到第一次的响应。
业务上应在分支中提前 `return`。

**Q：能在协程里直接使用 `ctx` 吗？**
不能。请求结束后 `ctx` 会被复用；需要在协程中访问时，先把必要字段复制出来，
或使用 `DeriveContext`。
