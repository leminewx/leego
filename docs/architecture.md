# 架构设计

本文说明 Leego 的分层结构、请求生命周期、并发模型与关键设计取舍。阅读源码前
建议先读本文，可以快速建立「谁在做什么」的整体认知。

---

## 分层结构

```
                     ┌──────────────────────────────┐
                     │        net/http (标准库)      │
                     └──────────────┬───────────────┘
                                    │ http.Handler
                     ┌──────────────▼───────────────┐
                     │            Engine            │
                     │  配置 / 生命周期 / 全局链     │
                     └───┬───────────┬───────────┬──┘
             ┌───────────▼──┐   ┌────▼─────┐  ┌──▼───────────┐
             │    Router    │   │  Group   │  │  Dashboard   │
             │ 方法 → Trie  │   │ 前缀/中间件│  │ 内嵌单页/API │
             └───────┬──────┘   └────┬─────┘  └──────────────┘
                     │               │
             ┌───────▼───────────────▼──────┐
             │           Handler            │
             │ 冻结后的链 / 启停 / 采集器    │
             └───────────────┬──────────────┘
                             │
             ┌───────────────▼──────────────┐
             │       Context + response     │
             │ 参数 / 绑定 / 响应 / 错误链   │
             └──────────────────────────────┘
```

| 层 | 职责 | 关键约束 |
| --- | --- | --- |
| `Engine` | 配置合并、生命周期、全局中间件链、请求入口 | 同时是根 `Group`，可直接注册路由 |
| `Router` | 「方法 + 路径 → Handler」映射、`Allow` 收集、黑名单 | 注册期加锁，冻结后无锁 |
| `Group` | 前缀拼接、中间件继承、路由注册 | 只在注册期使用 |
| `Handler` | 折叠后的处理链、启停状态、指标采集器 | 链只折叠一次 |
| `Context` | 单请求状态与全部请求/响应能力 | 池化对象，禁止跨请求持有 |
| `response.Writer` | 状态码、字节数、`Flush`/`Hijack` 透传 | 池化对象，随请求归还 |

---

## 请求生命周期

一次请求会依次经过以下阶段：

1. **进入 `Engine.ServeHTTP`**：确保 `Prepare()` 已执行（幂等，`sync.Once`）。
2. **取出上下文**：从 `sync.Pool` 取 `Context`，从 `response.WriterPool` 取写入器，
   并绑定 `*http.Request` 与 `http.ResponseWriter`。
3. **执行全局中间件链**：链在 `Prepare` 阶段折叠为单个 `HandleFunc`，运行期
   只做一次原子指针加载。
4. **黑名单检查**：命中直接 `403`。
5. **路由匹配**：`Router.Search` 按「静态 > 参数 > 通配」查找；`HEAD` 未注册时
   回退到 `GET`。
6. **未命中处理**：存在其他方法则 `405 + Allow`；`OPTIONS` 直接 `204 + Allow`；
   否则 `404`。
7. **命中处理**：写入路由参数 → 采集器 `Increase()` → 执行处理器 → 采集器
   `Decrease(status, cost)`（其中包含禁用路由的 `423` 分支）。
8. **归还资源**：释放响应写入器并清空上下文后放回池中。

> 第 3 步在路由匹配之前，意味着 CORS、日志、恢复、请求 ID 等中间件对 `404`、
> `405`、`403` 同样生效。这是刻意的设计：错误响应也必须有一致的头与日志。

---

## 冻结模型

注册期与运行期的边界由 `Prepare()` 划出：

```
注册期                          运行期
─────────────────────────────┼──────────────────────────────
Group.GET/POST/...           │ ServeHTTP
Router.Insert（加锁）         │ Router.Search（无锁）
Handler.Prepare（折叠链）     │ 原子读取链指针
Engine.UseMiddlewares        │ 不再接受中间件变更
                             │ Handler.Enable/Disable（原子）
                             │ AddURLBlacklist（sync.Map）
```

冻结带来三个收益：

- 查询路径没有互斥锁，也没有 RCU 读屏障；
- 每条路由的中间件链只折叠一次，不存在的「首次请求竞争」；
- 全局链只构建一次，错误处理路径与正常路径一致。

代价是**启动后不能动态注册路由**。运行期需要开关能力时使用
`Handler.Enable()` / `Handler.Disable()`；它们的开销仅是一次原子写。

---

## 并发模型

| 数据 | 保护方式 | 说明 |
| --- | --- | --- |
| 路由表 | 注册期 `sync.RWMutex` + 冻结后只读 | 冻结后不再加锁 |
| 路由中间件链 | `atomic.Pointer` + `sync.Once` | 折叠一次，读取无锁 |
| 全局中间件链 | `atomic.Pointer` + `sync.Once` | 同上 |
| 路由启停 | `atomic.Bool` | 运行期可切换 |
| 黑名单 | `sync.Map` | 运行期可增删 |
| 指标计数 | `atomic.Int64` | 请求数、耗时、状态码分桶 |
| 上下文 | `sync.Pool` | 每请求复用 |
| 响应写入器 | `sync.Pool` | 每请求复用 |
| 模板 | `sync.RWMutex` | 允许运行期替换模板函数 |
| 错误处理器 | `sync.RWMutex` | 允许运行期替换 |

**池化约束**：`Context` 与 `response.Writer` 在请求结束后会被复用，因此：

- 不要把 `*Context` 存进全局变量、协程或长连接结构；
- 需要在协程中使用请求数据时，先复制需要的值，或使用 `DeriveContext` 派生一个
  绑定到 `context.Context` 的新上下文；
- 长连接（SSE / WebSocket）必须在处理器内**同步阻塞**，提前返回会导致响应
  写入器被归还，连接被意外关闭。

---

## 指标模型

指标按「路由 × 状态码」聚合：

```
Snapshot（一条路由）
├── Method / Pattern / Name / Tags / Enabled
├── Flights   进行中的请求数
├── Total     总请求数
├── Failed    非 2xx/3xx 数量
├── TotalCostMicro 总耗时（微秒）
└── Metrics[]（按状态码）
    ├── Code
    ├── Total
    ├── MaxCostMicro / MinCostMicro
    └── TotalCostMicro
```

选择「按状态码分桶」而非「只记成功/失败」，是为了同时满足三类需求：

- 成功率与错误率：`Snapshot.GetFailureRate()`；
- 状态码分布图：直接遍历 `Metrics`；
- 延迟分析：`Metric.GetAverageCostMicro()` 与最大值。

成本是每个路由多几个原子计数器；状态码数量有限（通常个位数），内存可控。

---

## 错误处理

错误有三条路径，职责不同：

| 入口 | 用途 | 输出 |
| --- | --- | --- |
| `ResponseError(code, message)` | 明确的业务错误 | 纯文本 + 状态码，同时写入错误链 |
| `Fail(err)` | 统一错误出口 | 自定义 `ErrorHandlerFunc` → 默认 JSON |
| `middleware.Recover()` | 捕获 panic | 500 + 堆栈日志，可自定义回调 |

`ResponseError` 在状态码已写入时只记录错误，不重复写响应，避免「头已发送」导致
的无效写入。`Fail` 依次尝试引擎级错误处理器与默认格式，便于把领域错误映射为
HTTP 语义。

---

## 启动与关闭

```
NewEngine → 注册路由/中间件/后台 → Run / RunTLS / RunUnix / RunFd / RunListener
                                              │
                                              ├─ newServer（应用超时与头大小）
                                              ├─ logStartup（可关闭）
                                              └─ ListenAndServe
WaitSignalAndShutdown(timeout, before)
        │
        ├─ 等待 SIGINT / SIGTERM
        ├─ 调用 before()（失败不阻止关闭）
        └─ Shutdown(ctx)（超时强制返回错误）
```

`normalizeServeError` 会把 `http.ErrServerClosed` 归一化为 `nil`，因此正常关闭
不会被误判为启动失败。

---

## 设计取舍速查

| 决策 | 收益 | 代价 |
| --- | --- | --- |
| 标准库优先 | 无外部 Web 依赖、行为可预期 | 需要自行实现路由与长连接 |
| 分段 Trie | 匹配稳定、支持优先级与通配 | 注册期构建成本略高 |
| 注册期冻结 | 运行期零锁 | 不能热注册路由 |
| 上下文池化 | 热路径无堆分配 | 不能跨请求持有 |
| 指标内建 | 开箱即用，无需额外中间件 | 每个路由少量原子开销 |
| 后台内嵌 | 离线可用、部署简单 | 修改 UI 需重新编译 |
| 命名以动词开头 | API 语义统一、可检索 | 与旧版 API 不兼容（见迁移对照） |
