# 拓展路线：基于现有架构的子模块与功能点分析

本文只做**设计与可行性分析**，不含实现代码。目标是在不破坏现有架构
（自研路由树 + 池化 Context + 注册期冻结的中间件链 + 进程内指标）的前提下，
给出可分期交付的拓展方向。

具体到包布局、接口签名、关键难点与 PR 切片的落地设计见
[extension-design.md](extension-design.md)。

> **依赖硬约束**：核心与本文所有方案均不引入任何第三方库，只使用 Go 标准库
> 与 `golang.org/x/*` 官方扩展库（当前仅 `x/crypto`、`x/time`）。
> 需要外部系统的能力（Redis、对象存储、OTel SDK、SQLite、YAML、Brotli 等）
> 一律**只定义接口**，由使用方在独立模块中实现并注入；不得为了某个可选特性
> 而把依赖塞进核心的 `require` 列表。可行性矩阵见第七节。

---

## 一、现状盘点

| 能力域 | 现有实现 | 可复用的关键点 |
| --- | --- | --- |
| 路由 | `tree` 前缀树、参数段/通配段、注册期冻结、405 与 Allow 头 | `Router.Search`、`Engine.GetRoutes`、`RouteInfo` |
| 上下文 | 池化 `Context`、`Response*` 响应族、表单/JSON/XML 绑定 | `Context` 生命周期钩子、`response.Writer` 池 |
| 中间件 | 链式 `MiddlewareFunc`、注册期组装、按 tag 归属 | `Handler`、`collector` 采集点 |
| 指标 | `collector`（延迟分位、状态码分布、失败率）+ `dashboard` 后台 | `Snapshot`、`Report`、`CollectReport` |
| 实时通信 | SSE（单写者）、WebSocket（收写双协程 + Manager 广播） | `sse.Manager`、`websocket.Manager` |
| 安全 | JWT（HMAC）、CORS、限流（固定/滑动/令牌桶）、熔断 | `middlewares/jwt`、`limiter`、`breaker` |
| 工程化 | `stresstest` 压测、`logging`、`headers`、`response` | 压测客户端、日志前缀约定 |

对照 gin / echo / fiber / chi / beego 的常见能力面，缺口集中在：
**配置管理、参数校验、OpenAPI、可观测性外接、分布式协调、静态资源工程化、测试工具**。

---

## 二、P0：高价值、低风险、强复用

### 2.1 配置中心与热更新（`config` 包）

**功能描述**
统一的配置加载与监听：内置 JSON 文件、环境变量、命令行参数三种来源，
按「命令行 > 环境变量 > 文件 > 默认值」优先级合并；支持字段级热更新回调。
YAML/TOML 不做内建解析（标准库不提供），通过 `Source` 接口由使用方注入。

**方案**
- 新增 `config` 包：`Source` 接口（`Load() (map[string]any, error)`、`Watch(func(map[string]any))`），
  内置 `FileSource`、`EnvSource`、`FlagSource`；`Loader` 负责合并与类型转换。
- 与 `Engine` 的衔接点：`UseEngineName`、`UseServerTimeouts` 等 `EngineOption` 已是纯函数，
  热更新时只需重新求值并写入 `EngineConfig` 的可变副本；服务端超时属于启动期参数，
  明确标记为「只读配置」，避免运行期改超时引发的连接状态不一致。
- 结构体标签 `config:"server.read_timeout"`，复用 `Context.BindForm` 已有的反射工具链。
- 热更新监听不引入 fsnotify：用 `os.Stat` 轮询修改时间（默认 1s，可配置），
  内容变更后重建不可变快照并原子替换；轮询协程随 `Engine` 生命周期退出。

**应用场景**
多环境部署（dev/staging/prod）同一份二进制；K8s ConfigMap 挂载后热加载日志级别、
限流阈值、熔断阈值等运行期参数。

**可行性**
工作量约 3～5 人日；纯标准库（`encoding/json`、`flag`、`os`、`sync/atomic`），
零新增依赖；风险在于「热更新与并发读取的一致性」——采用不可变快照 +
`atomic.Pointer[Config]` 替换，禁止原地修改。优先级：**高**。

### 2.2 参数校验与统一错误码（`binding` + `problem` 包）

**功能描述**
在现有 `Bind` 之上补齐校验：`validate:"required,min=1,max=64,email"` 标签、
自定义校验器注册、错误消息本地化；把校验失败统一转换为结构化错误响应。

**方案**
- 新增 `validate` 子包：`Validator` 接口 + 默认反射实现，`RegisterRule(name, func)` 扩展；
  与 `bindFormValues` / `BindJSON` 共享同一套字段元信息缓存（`sync.Map[reflect.Type]`）。
- 错误码体系：`ErrorCode` 结构（`Code`、`Message`、`Status`、`Details`），
  提供 `NewError(code, msg)` 与 `Context.Fail(err)` 的桥接；输出两种格式：
  项目默认 `{code, message, details}` 与 RFC 9457 `application/problem+json`。
- 与 `HTTPError` 合并：`NewHTTPError` 已存在，扩展 `Details []FieldError` 即可，
  避免引入第二套错误模型。

**应用场景**
表单/接口入参校验；开放平台按错误码对接；前端按字段级错误渲染表单。

**可行性**
工作量 4～6 人日；反射元信息可复用 `Context` 绑定逻辑，风险低；
需注意「错误消息不能包含内部字段名或 SQL」以免信息泄露。优先级：**高**。

### 2.3 OpenAPI 3.1 自动生成（`openapi` 包）

**功能描述**
扫描已注册路由，自动产出 `openapi.json` 与 Swagger UI 页面；从 `Handler` 的
tags、参数名、绑定结构体标签推导 schema。

**方案**
- 复用 `Engine.GetRoutes()` 的 `RouteInfo`（method、pattern、tags、description）
  与 `Handler.GetParamNames()`；从 `tree` 的 `paramName` 推导路径参数。
- 新增 `openapi` 包：`Spec`、`PathItem`、`Operation` 结构体 + `NewSpec(engine)`；
  `RegisterOpenAPI(engine, prefix)` 注册 `/openapi.json` 与静态 UI（复用 `dashboard` 的
  静态资源嵌入方式）。
- 请求体 schema：扫描 handler 签名中的绑定目标类型（需为每个 handler 显式声明
  `WithRequest(UserDTO{})` 之类的元信息，避免全量反射猜测）。

**应用场景**
前后端契约先行；SDK 自动生成；接口文档与代码同源，避免文档腐化。

**可行性**
工作量 5～8 人日；生成器本身零外部依赖，UI 用 `embed.FS` 内嵌自写的
浏览页面（不引 CDN，保证离线与内网可用）；风险是「类型到 schema 的映射」复杂度
（泛型、嵌套、指针），建议先支持基础类型 + 结构体 + 切片 + map，复杂类型回退为
`object`。优先级：**高**。

### 2.4 指标持久化与历史分析（`collector` 扩展）

**功能描述**
把进程内指标落成时序数据，支持按分钟/小时聚合的历史趋势、Top N 慢接口、
状态码分布变化；dashboard 增加趋势图与时间范围筛选。

**方案**
- 新增环形缓冲 `TimeSeries`（固定窗口，默认 1 小时 / 60 桶），复用
  `stresstest.Latencies` 的分位数算法（已有 HDR 风格实现）。
- 存储层抽象 `MetricStore` 接口：`Append(Snapshot)`、`Query(from, to, step)`；
  内置 `MemoryStore`（默认）与 `FileStore`（JSONL 追加 + 启动回放，标准库实现）。
  需要 SQL 查询能力时由使用方实现 `MetricStore` 注入，核心不引入数据库驱动。
- 导出侧：`/metrics` 输出 Prometheus 文本格式（`TextEncoder` 手写即可，无需依赖），
  同时保留 JSON 给 dashboard。
- 后台页面：在现有静态页上增加「趋势 / Top N / 时间筛选」，数据走新增的
  `/api/metrics/history` 接口。

**应用场景**
容量规划；上线后回归对比；故障复盘定位「何时开始变慢」。

**可行性**
工作量 4～6 人日；内存占用可控（60 桶 × 路由数）；`FileStore` 只依赖
`os`/`bufio`/`encoding/json`，无 CGO、无数据库驱动，交叉编译无额外成本。优先级：**高**。

### 2.5 静态资源工程化与 SPA 支持

**功能描述**
`embed.FS` 内嵌前端产物、SPA history 路由回退、静态资源压缩（gzip）、
内容指纹与长缓存头。

**方案**
- 扩展 `Group.SetStaticFS`：新增 `SetStaticEmbed(fs embed.FS, root string)`；
  复用现有 `newStaticHandler` 的路径穿越防护（`containsPathTraversal`）。
- SPA 回退：`SetSpaFallback(prefix, index string)`，未命中文件时返回 index.html，
  但**仅对 GET/HEAD 且无扩展名**的路径生效，避免把 API 404 吞掉。
- 压缩：新增 `middlewares.Compress`，基于 `sync.Pool` 复用 `gzip.Writer`，
  按 `Accept-Encoding` 协商；对 `text/event-stream` 与 WebSocket 升级请求跳过。
- Brotli 在标准库中没有编码器，因此不做内建；压缩层抽象为 `Compressor` 接口
  （`gzip` 为默认实现），使用方可注入 br 实现，核心不新增依赖。
- 指纹与缓存：不改文件名，改用 `ETag`（文件大小+修改时间）与
  `Cache-Control: public, max-age=31536000, immutable` 仅对带 hash 的产物目录生效。

**应用场景**
单二进制交付前后端；内网离线部署；移动端 H5 静态资源加速。

**可行性**
工作量 3～4 人日；全部基于标准库（`embed`、`compress/gzip`、`crypto/sha256`）；
风险是压缩中间件与 `Flush`/SSE 的兼容性，必须在 `ResponseSSE` 路径上显式旁路。
优先级：**高**。

### 2.6 测试工具包（`testutil` 包，已落地于 `harness/testutil`）

**功能描述**
为使用方提供断言友好的测试辅助：`testutil.NewClient(engine)` 返回可直接请求的
客户端；`Response.BindJSON(t, &target)`；不可变冻结时钟 `NewFrozenClock` 以稳定
TTL、限流窗口与 JWT 过期断言（限流/熔断/JWT 仍用各自内部时钟，冻结时钟不注入它们）。

**方案**
- 新增 `harness/testutil` 包（不进入 `go test` 主链路，仅作为库）：基于
  `httptest.ResponseRecorder` 进程内调用，`UseHeader` / `UseCookie` 固定头，
  `WithHeader` / `WithCookie` / `WithQuery` / `WithRawQuery` 为单请求选项。
- 时钟注入：目前只提供 `FrozenClock`（`Advance` / `Set`）供业务测试使用；
  `limiter`、`breaker`、`jwt.Parser` 仍各自读系统时间，未收敛到 `Clock`（规划中）。
- `FuzzHandler` 与 `testdata` 约定尚未实现；现有压测靶机为
  `harness/stresstest/test/base`。

**应用场景**
业务方写 handler 单测；框架自身的回归测试减少 `time.Sleep`；
CI 中稳定的限流/熔断时间相关测试。

**可行性**
工作量 3 人日；`Clock` 改造涉及 3 个包，需回归 `-race`；风险低。优先级：**高**。

---

## 三、P1：中价值、中风险

### 3.1 可观测性外接（OpenTelemetry / Prometheus）

**功能描述**
自动为每个请求生成 trace span（含路由、状态码、耗时），指标按 OTel 语义约定导出；
日志携带 trace_id。

**方案**
- 不引入 OTel SDK：新增 `middlewares/trace`，自实现 W3C `traceparent` 解析与生成
  （`encoding/hex` 即可），`SpanName` 优先用路由 pattern（`GET /users/:id`）而非
  原始路径，避免高基数；span 数据通过 `Exporter` 接口交由使用方上报。
- `logging` 增加 `WithFields`，把 `trace_id`/`span_id` 注入日志前缀。
- 指标不必重复造轮子：`collector` 的 `Snapshot` 已含计数与分位，
  内建 Prometheus 文本导出（手写 `TextEncoder`），OTel 上报由 `Exporter` 实现注入。

**应用场景**
微服务链路追踪；SLO 看板；跨服务故障定位。

**可行性**
工作量 5～7 人日；核心零依赖：`traceparent` 传播 + `Exporter` 接口，
真正对接 OTel Collector 由使用方在独立模块实现。优先级：**中高**。

### 3.2 分布式限流与熔断（`limiter` / `breaker` 后端抽象）

**功能描述**
把进程内令牌桶扩展为「本地 + 外部配额」双层：本地承接突发，外部（如 Redis）
做全局配额；外部不可用时自动降级为纯本地限流。框架只定义后端接口，不内建客户端。

**方案**
- 抽象 `Limiter` 接口（`Allow(ctx) bool`、`AllowWithTimeout(ctx, d) (bool, error)`），
  现有三个实现保持不变；新增 `Backend` 后端接口
  （`Take(ctx, key, n) (bool, time.Duration, error)`），使用方用任意客户端实现
  Redis/Lua 原子扣减。
- `breaker` 的滑动窗口可选用 Redis 共享统计，但建议**保持本地熔断**：
  分布式熔断容易产生「雪崩式误判」，更适合交给服务网格。
- 降级策略：`FallbackLimiter` 包装器，外部后端超时超过阈值时切到本地实现并打点。

**应用场景**
多副本部署下的全局 QPS 限制；开放平台按租户配额；秒杀场景的入口保护。

**可行性**
工作量 6～8 人日；框架侧零依赖（只新增接口与降级包装器），Redis 实现由使用方
在独立模块提供；风险是时钟漂移与网络分区下的配额不准。优先级：**中**。

### 3.3 缓存中间件（`middlewares/cache`）

**功能描述**
HTTP 响应缓存：`ETag`/`Last-Modified` 协商、`stale-while-revalidate` 后台刷新、
按路由声明 TTL 与 vary 维度（如 `Authorization` 指纹）。

**方案**
- 需要可回放的响应：现有 `response.Writer` 只统计状态码与大小，需新增
  `Recorder`（镜像 body 到内存，超过阈值则不缓存并告警）。
- 缓存键 = method + host + 原始路径 + query + vary 头摘要；存储用 `lru`（可复用
  `godis` 工作区中的 LRU 实现思路，但需在本仓库内独立实现以免跨模块依赖）。
- Cookie 默认参与旁路：携带 Cookie 的请求不缓存，含 `Set-Cookie` 的响应也不缓存；
  与身份无关的路由可显式开启。
- 与 `Context` 池化兼容：缓存命中时直接写 body，不再执行后续 handler。

**应用场景**
高频只读接口（配置下发、字典、首页）；CDN 前置回源保护。

**可行性**
工作量 5～6 人日；难点在「响应回放 + 池化 Writer 生命周期」，需要严格 `Reset`；
风险是缓存穿透与内存膨胀，需默认关闭并按路由显式开启。优先级：**中**。

### 3.4 会话与认证增强

**功能描述**
在 JWT 之外补充：服务端会话（Cookie + 存储后端）、OAuth2/OIDC 登录、RSA/ECDSA 签名。

**方案**
- `jwt`：把 `Signer` 接口从 HMAC 扩展到 `RS256/ES256`（`crypto/rsa`、`crypto/ecdsa`），
  `Parser` 已按 `Algorithm` 分发，只需新增实现与 `kid` 支持。
- `session` 新包：`Store` 接口（`Get/Save/Destroy`），内置内存实现；
  跨进程存储由使用方实现接口注入；Cookie 复用 `Context.SetCookieData` 的编码与 `SameSite` 处理。
- OIDC：只做「授权码 + ID Token 校验」最小闭环；discovery 与 JWKS 用
  `net/http` + `encoding/json` 手写，ID Token 复用本仓库 `jwt`，不引入 IdP SDK。

**应用场景**
需要即时踢下线的后台系统（JWT 无状态做不到）；第三方登录；密钥轮换。

**可行性**
工作量 6～10 人日；`jwt` 扩展风险低，`session` 需处理并发与过期清理；
优先级：**中**。

---

## 四、P2：高价值、高复杂度

### 4.1 WebSocket 能力增强

**功能描述**
分片消息、`permessage-deflate` 压缩、房间/频道、消息 ACK 与重连令牌、
背压可视化（队列水位指标）。

**方案**
- 分片：`upgrader.Conn` 的 `Read` 已拒绝分片（1003），扩展为「累积 + 上限校验」，
  上限复用 `MaxReadLimit`；控制帧仍要求 FIN=1 且 ≤125 字节。
- 压缩：握手阶段协商 `Sec-WebSocket-Extensions`，在 `Conn` 内挂标准库
  `compress/flate` 读写器；需谨慎处理 RSV1 位与上下文接管。
- 房间：`Manager` 增加 `Rooms map[string]map[string]*Client`，复用现有 `clients sync.Map`；
  广播按房间过滤，保持非阻塞投递语义。
- ACK：新增 `MSG_TYPE_ACK`，客户端对 `seq` 确认；服务端维护有限窗口重发。
- 背压指标：把 `len(sendChan)` 与丢弃计数导出到 `collector`，dashboard 展示。

**应用场景**
IM/协作编辑（房间 + ACK）；物联网长连接（压缩 + 分片）；大屏推送。

**可行性**
工作量 12～18 人日；协议正确性风险最高，必须补 fuzz 与 RFC 6455 用例；
建议拆成「分片 → 压缩 → 房间 → ACK」四个独立里程碑。优先级：**中（分阶段）**。

### 4.2 SSE 增强

**功能描述**
`Last-Event-ID` 断线回放、跨副本广播（后端可插拔）、按 topic 订阅、
慢客户端背压策略（丢弃最旧事件）。

**方案**
- 现有 `sse.Manager` 是单进程内存模型，`Client` 有 `messageCacheNumber` 缓存；
  在其上增加 `eventId` 单调序号与 `Replay(fromID)`。
- 跨副本：`Broadcaster` 接口（`Publish(topic, msg)`、`Subscribe(topic)`），
  内置进程内实现；Redis Streams 等后端由使用方实现接口注入（需支持最近 N 条回放）。
- 背压：通道满时按「丢弃最旧 + 计数」而非阻塞，与 WebSocket 的快速失败语义对齐。

**应用场景**
订单状态推送、任务进度、告警流；多副本部署下的事件一致性。

**可行性**
工作量 8～12 人日；单机回放容易且框架侧零依赖，跨副本回放需存储支持（由使用方注入）；
风险是事件重复与顺序保证。优先级：**中**。

### 4.3 文件上传增强

**功能描述**
分片上传、断点续传、秒传（内容哈希去重）、对象存储适配（S3/OSS/MinIO）、
上传进度与病毒扫描钩子。

**方案**
- 复用 `Context.GetMultipartFormValue` 与 `SaveUploadedFile` 的目录创建逻辑；
  新增 `upload` 包：`Session`（uploadId + 分片位图）、`Storage` 接口
  （`PutPart`、`Complete`、`Abort`）。
- 秒传：SHA-256 内容寻址，索引表可先用本地文件 + 内存 map。
- 对象存储：只定义 `Storage` 接口（本地磁盘为默认实现），S3/OSS/MinIO
  适配器由使用方在独立模块实现，核心不引入云 SDK。

**应用场景**
大文件（视频、模型、日志包）上传；弱网续传；多副本共享存储。

**可行性**
工作量 10～15 人日；难点在分片一致性与并发合并；
建议先实现本地存储版本，云存储作为适配器。优先级：**中**。

### 4.4 任务调度与后台作业

**功能描述**
进程内 cron 调度、一次性延迟任务、Worker Pool 限并发、任务幂等与重试。

**方案**
- 新增 `job` 包：`Scheduler`（最小堆 + 单协程驱动）、`WorkerPool`（有界队列）、
  `Job` 接口（`Run(ctx) error` + `RetryPolicy`）。
- 与 `Engine` 生命周期绑定：`Engine.Start(ctx)` 启动，`Shutdown` 时等待在途任务并
  拒绝新任务；复用 `Engine.WaitSignalAndShutdown` 的优雅关闭模式。
- 分布式协调（防重复执行）留给外部，不内建选主。

**应用场景**
定时清理、报表生成、缓存预热、异步通知。

**可行性**
工作量 5～7 人日；纯标准库可完成；风险是「关机时任务丢失」，
需明确 at-most-once 语义并写入文档。优先级：**中低**。

### 4.5 命令行脚手架（`leego-cli`）

**功能描述**
`leego new <project>` 生成标准工程骨架（含 dashboard 注册、配置模板、Dockerfile）；
`leego gen handler <name>` 生成 handler + 测试 + 路由注册片段；`leego routes` 打印路由表。

**方案**
- 独立 `cmd/leego` 模块，只用标准库 `flag` + `embed.FS`（不引入 cobra）；
  模板用 `embed.FS` 内嵌。
- `leego routes` 可直接复用 `Engine.GetRoutes()` 与 `sortRoutes` 的输出格式。

**应用场景**
新项目初始化；团队内代码风格统一；CI 中检查路由变更。

**可行性**
工作量 4～6 人日；无核心侵入；优先级：**中低**。

### 4.6 网关与灰度能力

**功能描述**
反向代理（`httputil.ReverseProxy` 封装）、按 header/权重灰度、超时预算传递、
上游熔断与重试。

**方案**
- 新增 `gateway` 包：`Upstream`（目标 + 权重 + 健康检查）、`Balancer`（轮询/加权）、
  `ProxyHandler`；复用 `breaker` 做上游熔断，`limiter` 做上游配额。
- 超时预算：从 `Context.Request.Context()` 读取 deadline，向下游传递
  `X-Request-Timeout`，并在剩余预算不足时快速失败。

**应用场景**
BFF 层；遗留系统前置网关；灰度发布。

**可行性**
工作量 10～15 人日；纯标准库可实现；风险在连接复用与流式响应（SSE/WS 透传）。
优先级：**低（与框架定位偏离）**。

---

## 五、候选清单（暂不展开）

| 方向 | 一句话说明 | 依赖档位 | 结论 |
| --- | --- | --- | --- |
| ORM / 数据库层 | 工作区已有 `worm`，可做可选集成而非内建 | C（使用方引入驱动） | 不建议内建 |
| 模板引擎重写 | 标准库 `html/template` 已足够 | A | 不做 |
| HTTP/2 服务端推送 | 已被主流浏览器废弃 | A | 不做 |
| HTTP/3 / QUIC | 需引入 `quic-go`，与标准库 `http.Server` 模型差异大 | C | 不做 |
| gRPC-Gateway | 需 `grpc` 生态；适合独立网关进程，不宜塞进核心 | C | 不做（独立进程） |
| 国际化（i18n） | 消息表 + `encoding/json` 即可，无需 `x/text` | A | 并入 P0-2.2 |
| 审计日志 | 结构化日志 + 中间件即可 | A | 低优先 |
| 请求签名与防重放 | `crypto/hmac` + 时间窗 + nonce | A | 并入 P1-3.4 |
| 多租户隔离 | 需要配置/路由/指标三层贯通，改动面大 | A（纯逻辑） | 观望 |
| 消息队列集成 | Kafka/NATS 属于应用层选型 | C | 不做 |

---

## 六、优先级、前置依赖与依赖档位

档位定义：**A** = 纯标准库；**B** = 仅需 `golang.org/x/*`（已具备）；
**C** = 需非官方第三方（核心不引入，只留接口）。

| 优先级 | 模块 | 前置依赖 | 档位 | 预估 |
| --- | --- | --- | --- | --- |
| P0-1 | 配置中心与热更新 | 无 | A | 3～5 人日 |
| P0-2 | 参数校验与错误码 | 2.1（可选） | A | 4～6 人日 |
| P0-3 | OpenAPI 生成 | 2.2（schema） | A | 5～8 人日 |
| P0-4 | 指标持久化 | 无（复用 collector） | A | 4～6 人日 |
| P0-5 | 静态资源工程化 | 无 | A | 3～4 人日 |
| P0-6 | 测试工具包 | 时钟注入改造 | A | 3 人日 |
| P1-1 | 可观测性外接 | 2.4（指标导出） | A（+C 对接） | 5～7 人日 |
| P1-2 | 分布式限流熔断 | 2.1（配置） | A（+C 后端） | 6～8 人日 |
| P1-3 | 缓存中间件 | 3.3（Recorder） | A | 5～6 人日 |
| P1-4 | 会话与认证增强 | 2.1 | A | 6～10 人日 |
| P2-1 | WebSocket 增强 | 2.4（背压指标） | A | 12～18 人日 |
| P2-2 | SSE 增强 | P1-2（可选） | A（+C 跨副本） | 8～12 人日 |

建议节奏：**先做 P0 六项（约 1 个迭代）**，把「配置—校验—文档—指标—静态资源—测试」
这条工程闭环补齐；再做 P1 的外接能力；P2 按业务真实需求单项启动，
避免同时铺开导致协议正确性验证不足。

---

## 七、零第三方依赖可行性矩阵

结论先行：**上面 12 个重点模块全部可在「核心零第三方」前提下交付**。
区别只在能力深度：涉及外部系统的那部分被拆成「接口（A 档）+ 使用方实现（C 档）」，
接口一旦确定，后续接 Redis / OTel / S3 只需新增适配器，核心不改、`go.mod` 不变。

### 7.1 A 档：纯标准库即可交付

| 模块 | 关键标准库 | 说明 |
| --- | --- | --- |
| 配置中心（2.1） | `encoding/json`、`flag`、`os`、`sync/atomic`、`time` | JSON + env + flag；轮询 `os.Stat` 做热更新 |
| 参数校验（2.2） | `reflect`、`regexp`、`strings`、`encoding/json` | 标签解析、字段缓存、problem+json 手写 |
| OpenAPI（2.3） | `encoding/json`、`net/http`、`html/template`、`embed` | 自写浏览页，不引 Swagger UI / CDN |
| 指标持久化（2.4） | `container/ring`、`os`、`bufio`、`encoding/json`、`sort`、`fmt` | `FileStore` + 手写 Prometheus TextEncoder |
| 静态资源（2.5） | `embed`、`compress/gzip`、`crypto/sha256`、`net/http` | 无 Brotli，只做 gzip |
| 测试工具包（2.6） | `net/http/httptest`、`testing`、`context`、`sync` | 时钟抽象为接口，测试中注入假时钟 |
| 缓存中间件（3.3） | `container/list`、`crypto/sha256`、`sync`、`strconv` | LRU + ETag + swr 全部自研 |
| JWT RS256/ES256（3.4） | `crypto/rsa`、`crypto/ecdsa`、`crypto/x509`、`encoding/pem` | 密钥解析、签名、验签均在标准库内 |
| 会话 Session（3.4） | `crypto/hmac`、`crypto/rand`、`net/http`、`sync`、`time` | 内存 Store + 签名 Cookie |
| OIDC 最小闭环（3.4） | `net/http`、`encoding/json`、`crypto/*` | discovery + JWKS + ID Token 校验 |
| WebSocket 增强（4.1） | `compress/flate`、`encoding/binary`、`crypto/rand` | 分片、permessage-deflate、房间、ACK |
| SSE 增强（4.2） | `net/http`、`sync`、`time`、`strconv` | Last-Event-ID 回放、背压丢弃策略 |
| 分片上传（4.3） | `mime/multipart`、`crypto/sha256`、`os`、`io` | 本地 `Storage` 实现 + 秒传 |
| 任务调度（4.4） | `container/heap`、`time`、`context`、`sync` | 最小堆驱动 + Worker Pool |
| CLI 脚手架（4.5） | `flag`、`embed`、`text/template` | 不引 cobra |
| 网关与灰度（4.6） | `net/http/httputil`、`net/url`、`net` | ReverseProxy 封装 + 加权轮询 |
| 明文 HTTP/2（h2c） | `net/http`（`Server.Protocols`，Go 1.24+） | 同端口 HTTP/1.1 + h2c，无需扩展库 |
| 请求签名与防重放 | `crypto/hmac`、`crypto/sha256`、`strconv` | 时间窗 + nonce 去重 |
| 国际化 i18n | `encoding/json`、`fmt` | 消息表 + 占位符替换 |
| Prometheus 导出 | `fmt`、`sort`、`strings` | 文本格式手写，无需 client_golang |
| 审计日志 | `encoding/json`、`os`、`io` | 结构化行日志 + 中间件 |

### 7.2 B 档：仅需官方扩展库（`go.mod` 已具备）

| 能力 | 依赖 | 现状 |
| --- | --- | --- |
| 密码哈希 `bcrypt` | `golang.org/x/crypto/bcrypt` | 已使用 |
| 令牌桶限流 | `golang.org/x/time/rate` | 已使用 |
| 文本编码转换（非必需） | `golang.org/x/text` | 未引入，需要时单独添加 |

> 注意：B 档仅限「已经在 `require` 里的官方库」。新增任何 `golang.org/x/*`
> 模块都需要单独评估（属官方，但仍是新增依赖），原则上优先用标准库。

### 7.3 C 档：必须真第三方 → 一律只留接口

| 能力 | 常见第三方 | 核心侧处理方式 | 能力损失 |
| --- | --- | --- | --- |
| YAML / TOML 配置 | `gopkg.in/yaml.v3`、`BurntSushi/toml` | 只用 JSON，`Source` 接口注入 | 配置文件需改 JSON 或自写适配器 |
| SQL 存储 | `mattn/go-sqlite3`、`modernc.org/sqlite` | `FileStore`（JSONL）；`MetricStore` 注入 | 无 SQL 查询，只有时间范围过滤 |
| Brotli 压缩 | `andybalholm/brotli` | 只做 gzip，`Compressor` 接口注入 | 首字节体积略大（文本约差 10%～15%） |
| OpenTelemetry SDK | `go.opentelemetry.io/otel` | 自研 `traceparent` + `Exporter` 接口 | 无自动埋点生态，需使用方适配 |
| Redis | `redis/go-redis` | `Backend` / `Broadcaster` / `Store` 接口 | 跨副本能力由使用方实现 |
| 对象存储 | `aws-sdk-go-v2`、`minio-go` | `Storage` 接口（本地实现为默认） | 需自写云存储适配器 |
| gRPC | `google.golang.org/grpc` | 独立网关进程，不在核心内 | 无内建 gRPC 服务端 |
| QUIC / HTTP3 | `quic-go` | 不做 | 无 HTTP/3 |
| 消息队列 | `kafka-go`、`nats.go` | 不内建，应用层选型 | 无内建 MQ 集成 |
| ORM | `gorm.io/gorm` | 只保证与 `database/sql` 友好，不内建 | 无 ORM 语法糖 |

### 7.4 如何在 CI 中守住这条线

用 `go list` 打印全量依赖，过滤掉标准库与 `golang.org/x/` 后必须为空：

```text
# 预期输出为空
# PowerShell
powershell> go list -deps ./... | Select-String '^[A-Za-z0-9._-]+\.[A-Za-z]{2,}/' |
              Select-String -NotMatch '^golang\.org/x/'
# bash
$ go list -deps ./... | grep -E '^[A-Za-z0-9._-]+\.[A-Za-z]{2,}/' | grep -v '^golang.org/x/'
```

同时把「不得新增 `require` 条目」写进 PR 检查清单；确需新增时，
只允许放在独立子模块（例如 `contrib/redis`），通过 `go.work` 或独立 `go.mod`
隔离，不进入根模块的依赖图。