# 路由

Leego 的路由由「分段 Trie」实现：注册期构建与冻结，运行期只读查询。本文说明
路由语法、匹配优先级、自动回退行为与相关限制。

---

## 注册接口

`Engine` 与 `Group` 提供同一组方法：

```go
GET(pattern string, handle HandleFunc, tags ...string)
PUT / POST / HEAD / PATCH / DELETE / OPTIONS
SetStatic(relativePath, rootPath string, tags ...string)
SetStaticFS(relativePath string, fs http.FileSystem, tags ...string)
```

`tags` 为可选参数：**第一个元素作为路由名称**，其余作为业务标签（最多保留
`collector` 包允许的数量）。名称与标签会出现在指标报告与分析后台中：

```go
engine.GET("/users/:id", handler, "查询用户详情", "user", "v1")
// Name = "查询用户详情"；Tags = ["user", "v1"]
```

### 重复注册

同一「方法 + 规范化路径」重复注册会 panic：

```
leego: duplicate route: GET /users/:id
```

把重复注册当作启动期错误处理，可以避免「后者覆盖前者」这类难以排查的问题。

---

## 路由语法

| 语法 | 含义 | 示例 |
| --- | --- | --- |
| 静态段 | 精确匹配 | `/users/list` |
| `:name` | 匹配单段，写入参数表 | `/users/:id` |
| `*name` | 匹配剩余全部路径（可跨 `/`） | `/files/*filepath` |
| `*` | 通配，参数名默认 `filepath` | `/static/*` |

取值：

```go
id := ctx.GetPathValue("id")
path := ctx.GetPathValue("filepath")
```

匹配优先级：**静态段 > `:param` > `*wildcard`**。因此 `/users/list` 与
`/users/:id` 可以共存，访问 `/users/list` 时命中静态路由。

两个容易踩的约束：

1. **同一位置不能使用不同参数名**。先注册 `/users/:id`，再注册
   `/users/:name` 会 panic：`tree: conflicting route parameter`。参数名影响
   `GetPathValue` 的键，因此必须全局一致。
2. **通配段必须位于模式末尾**。`/files/*path/edit` 虽然能注册成功，但通配节点
   自身没有挂载路由，永远不会被命中（匹配器只在通配节点已注册路由时才走通配
   分支）。把通配段放在中间属于无效路由。

---

## 路径规范化

组前缀与模式通过 `path.Join` 拼接，自动消除 `//`、`.`、`..`：

```go
api := engine.NewGroup("/api/")
api.GET("users", handler)     // 实际注册：GET /api/users
```

根模式 `/` 会被单独保留，不会被归一化为 `.`。

### 路径遍历防护

静态处理器在 `path.Clean` **之前**检查原始请求路径，拒绝包含以下内容的请求：

- `..` 段（向上穿越）；
- 反斜杠 `\`（Windows 路径分隔符）；
- `NUL` 字符；
- 绝对路径或 Windows 盘符路径。

命中后返回 `403`，并且不会触达文件系统。响应还会补充
`X-Content-Type-Options: nosniff` 与 `X-Frame-Options: DENY`。

---

## 分组

```go
api := engine.NewGroup("/api/v1")
api.UseMiddlewares(auth)

admin := api.NewGroup("/admin")
admin.UseMiddlewares(requireRole("admin"))
admin.GET("/stats", showStats) // GET /api/v1/admin/stats
```

- 前缀与中间件沿父链继承；
- 中间件执行顺序为「父组 → 子组」，同一组内按注册顺序；
- 分组不需要显式销毁，注册期结束后不再使用；
- 引擎本身是根分组，`engine.UseMiddlewares(...)` 等价于在根组挂载。

---

## 405、HEAD 与 OPTIONS

未命中的请求由 `Engine.handleUnmatched` 统一处理：

| 场景 | 响应 | 头 |
| --- | --- | --- |
| 路径不存在于任何方法 | `404` | — |
| 路径存在、方法不匹配 | `405` | `Allow: GET, HEAD, OPTIONS` |
| 方法为 `OPTIONS` 且路径存在 | `204` | `Allow: ...` |
| 方法为 `HEAD` 且仅注册了 `GET` | 执行 `GET` 处理器 | 由 `net/http` 丢弃消息体 |

`Allow` 的推导规则：

1. 逐个方法在对应 Trie 中查询该路径；
2. 若 `GET` 可用，自动加入 `HEAD`；
3. 始终加入 `OPTIONS`；
4. 按字母序排序后以 `", "` 连接。

因此显示注册 `engine.GET("/x", h)` 时，客户端收到的 `Allow` 是
`GET, HEAD, OPTIONS`。

---

## 黑名单

```go
engine.AddURLBlacklist("/admin", "/internal")
engine.IsBlacklisted("/admin") // true
```

黑名单使用 `sync.Map`，**运行期可增删**（当前仅提供添加与查询）。命中黑名单的
请求在路由匹配之前被拦截，返回 `403`；全局中间件链仍然执行，因此日志与 CORS
头保持完整。

黑名单按精确路径匹配，不做前缀或通配匹配；需要前缀语义时在中间件中自行判断。

---

## 路由开关

```go
handler, ok := engine.GetHandler(http.MethodGet, "/maintenance")
if ok {
	handler.Disable()
}
```

禁用后的行为：

- 路由仍参与匹配（`Allow` 与 404/405 判定不变）；
- 不执行任何中间件与业务处理器；
- 由引擎返回 `423 Locked`；
- 指标采集继续记录（可观测到被禁用的访问量）。

恢复使用 `handler.Enable()`。

---

## 冻结与注册时机

引擎在以下时机自动调用 `Prepare()`：

- 第一次 `ServeHTTP`；
- `Run` / `RunTLS` / `RunUnix` / `RunFd` / `RunListener` 启动前。

冻结后的行为：

| 操作 | 结果 |
| --- | --- |
| 注册新路由 | panic：`cannot register route after the engine has been prepared` |
| 追加中间件 | 不生效（全局链已折叠） |
| `Handler.Enable/Disable` | 允许（原子写） |
| `AddURLBlacklist` | 允许（`sync.Map`） |
| `SetErrorHandler` / `SetTemplateFuncs` | 允许（互斥锁保护） |

若需要在测试中提前验证路由表，可显式调用 `engine.Prepare()` 后用
`engine.IsPrepared()` 断言。

---

## 路由表快照

```go
for _, route := range engine.GetRoutes() {
	fmt.Println(route.Method, route.Pattern, route.Name, route.Tags, route.Enabled)
}
```

`GetRoutes()` 返回按「方法、模式」排序的 `[]RouteInfo`，字段均可 JSON 序列化，
适合直接暴露给运维接口或前端表格。它不包含处理器引用，避免泄漏内部状态。

---

## 静态文件服务

```go
engine.SetStatic("/static", "./public")
engine.SetStaticFS("/assets", http.Dir("./assets"))
```

实现要点：

- `SetStatic` 会先用 `filepath.Abs` + `os.Stat` 校验目录，目录不存在或不是目录
  时直接 panic，把配置错误暴露在启动期；
- 挂载点内部使用 `http.StripPrefix` + `http.FileServer`，因此 `Range`、
  `If-Modified-Since`、`ETag` 等语义与标准库一致；
- 每个挂载点注册 `GET` 与 `HEAD` 两条 `*filepath` 通配路由；
- 同一分组内同一挂载点重复挂载会 panic，避免静默覆盖。

> 由于只注册了 `*filepath` 通配，访问不带后续路径的挂载点（如 `/static`）会命中
> 通配但参数为空，处理器返回 `404`。需要目录索引时请请求 `/static/`。

---

## 常见问题

**Q：为什么注册路由后立刻访问是 404？**
路由注册必须在启动前完成；如果先启动了服务再注册，注册会 panic。若没有 panic
却访问不到，检查前缀是否被 `path.Join` 规范化（例如多余的 `/`）。

**Q：`/users/:id` 与 `/users/*rest` 能共存吗？**
可以，`:` 优先于 `*`。但同一层级同时存在静态、参数与通配时，匹配结果依次为
静态 → 参数 → 通配，不会出现歧义。

**Q：为什么 `Allow` 里没有我注册的 `PATCH`？**
`Allow` 由「该路径在各方法 Trie 中的实际命中」推导。若 `PATCH` 路由注册在别的
路径模式上（例如 `/users/:id` 而请求 `/users/1/`），则不会出现在 `Allow` 中。
