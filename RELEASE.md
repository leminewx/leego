# 发布说明

本文件记录各版本的能力变化、破坏性变更与升级步骤。历史版本以 Git 标签为准，
这里只保留当前版本与仍然影响升级决策的条目。

---

## v0.19.0.0 — 构造函数与选项函数命名对齐

> 发布日期：2026-11-20
> 要求：Go 1.27.4+

本版本按 `docs/conventions.md` 收紧两处命名，均为**不兼容变更**，不保留旧名别名。

### 一、包内唯一目标对象直接 `New()`

包名即被构造对象、且包内只有这一个可构造类型时，构造函数不再重复类型名：

| 旧名称 | 新名称 |
| --- | --- |
| `dashboard.NewDashboard(...)` | `dashboard.New(...)` |
| `cache.NewCache(...)` | `cache.New(...)` |
| `compress.NewCompress(...)` | `compress.New(...)` |
| `cors.NewDefaultCORS()` | `cors.New()` |
| `collector.NewCollector(...)` | `collector.New(...)` |
| `upgrader.NewUpgrader(...)` | `upgrader.New(...)` |

`cors.New()` 与原来的 `NewDefaultCORS()` 等价：返回带默认配置且已启用的实例。
包内有多个可构造类型的（`openapi.NewSpec`、`response.NewWriter`、`job.NewScheduler` 等）
仍保留 `New<类型>`。

### 二、选项函数改为 `With*` / `Without*`

返回 `Option` 类型的配置函数统一使用 `With<属性>` / `Without<属性>` 前缀，不再使用 `Use<属性>`：

| 旧名称 | 新名称 |
| --- | --- |
| `leego.UseEngineName(...)` 等 `EngineOption` | `leego.WithEngineName(...)` 等 |
| `leego.UseH2C()` | `leego.WithH2C()` |
| `leego.UseMetricsInterval(...)` / `UseMetricsStep(...)` / `UseMetricsAuthorizer(...)` | `leego.WithMetricsInterval(...)` 等 |
| `session.UseTTL(...)` / `UseSecure(...)` / `UseSecret(...)` 等同族 | `session.WithTTL(...)` 等 |
| `openapi.UseServer(...)` / `UseTag(...)` / `UseSchema(...)` / `UseOperation(...)` 等同族 | `openapi.WithServer(...)` 等 |
| `dashboard.UseTitle(...)` / `UseProvider(...)` 等同族 | `dashboard.WithTitle(...)` 等 |
| `upload.UseMaxSize(...)` 等同族 | `upload.WithMaxSize(...)` 等 |
| `harness.UseAddr(...)` 等同族 | `harness.WithAddr(...)` 等 |
| `gateway.UseTransport(...)` 等同族 | `gateway.WithTransport(...)` 等 |
| `job.UseWorkers(...)` / `UseErrorHandler(...)` | `job.WithWorkers(...)` / `WithErrorHandler(...)` |
| `config.UseWatchInterval(...)` | `config.WithWatchInterval(...)` |
| `(*testutil.Client).UseHeader(...)` / `UseCookie(...)` | `(*testutil.Client).WithHeader(...)` / `WithCookie(...)` |

`UseMiddlewares` 属于注册语义，不在此列，保持原名。

### 三、测试函数名

受影响的测试函数同步去掉类型后缀，例如 `TestNewCollector` → `TestNew`、
`TestNewDefaultCORS` → `TestNew`、`TestNewUpgraderCopiesConfig` → `TestNewCopiesConfig`。

### 四、升级步骤

1. 全局替换构造调用：`dashboard.NewDashboard` → `dashboard.New`、`cache.NewCache` → `cache.New`、
   `compress.NewCompress` → `compress.New`、`cors.NewDefaultCORS` → `cors.New`、
   `collector.NewCollector` → `collector.New`、`upgrader.NewUpgrader` → `upgrader.New`；
2. 把所有选项函数的 `Use` 前缀改为 `With`（`UseMiddlewares` 除外）；
3. `gofmt -l .`、`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全部通过。

---

## v0.17.0.0 — 中间件构造器与包名统一

> 发布日期：2026-11-20
> 要求：Go 1.27.4+

本版本把中间件统一为「构造器创建实例 + 实例适配为中间件」两段式，并把
`middleware` 包重命名为 `middlewares`。旧包路径与旧函数名全部删除，不保留别名。

### 一、包重命名

| 旧路径 | 新路径 |
| --- | --- |
| `gitee.com/leminewx/leego/middleware` | `gitee.com/leminewx/leego/middlewares` |
| `.../middleware/{breaker,cache,collector,compress,cors,jwt,limiter,trace}` | `.../middlewares/...` |

包声明同步改为 `package middlewares`，导入后的默认标识也是 `middlewares`。

### 二、构造器与适配器分离

所有中间件都改为「`New*` 只创建结构体或接口实例，实例的 `AdaptToMiddleware()`
返回 `leego.MiddlewareFunc`」：

| 旧用法 | 新用法 |
| --- | --- |
| `middlewares.Log()` / `LogWithConfig(cfg)` | `middlewares.NewLogger()` / `NewLoggerWithConfig(cfg)` |
| `middlewares.Recover()` | `middlewares.NewRecovery()` / `NewRecoveryWithConfig(cfg)` |
| `middlewares.Collect(target)` | `middlewares.NewMonitor(target)` |
| `cache.New(cfg)` | `cache.NewCache(cfg)` |
| `compress.New(cfg)` | `compress.NewCompress(cfg)` |
| `trace.New(exporter, opts...)` | `trace.NewTracer(exporter, opts...)` |
| `(*trace.Tracer).BuildMiddleware()` | `(*trace.Tracer).AdaptToMiddleware()` |
| `(*CORS).GetMiddleware()` | `(*CORS).AdaptToMiddleware()` |
| `(*CircuitBreaker).GetMiddleware()` | `(*CircuitBreaker).AdaptToMiddleware()` |
| `limiter.Limiter.GetMiddleware(timeout)` | `limiter.Limiter.AdaptToMiddleware()` |

上表全部需要尾部再调用 `.AdaptToMiddleware()` 才能传入 `UseMiddlewares`。
其中 `cache.NewCache` / `compress.NewCompress` 已在 v0.19.0.0 中收敛为 `New`，以文首版本说明为准。
`NewLoggerWithConfig` 与 `NewRecoveryWithConfig` 在构造时复制一份配置快照，
运行期修改原始配置对象不再影响已创建的实例。

### 三、限流等待语义下沉

`GetMiddleware(waitTimeout)` 的参数被移除，等待上限改由构造时写入的
`limiter.Config.WaitTimeout` 决定：

| `WaitTimeout` | 行为 |
| --- | --- |
| `0`（默认） | 不等待，配额不足立即返回 `429` |
| 正数 | 在超时内以 50ms 轮询等待 |
| 负数 | 归一化为 `0` |

**行为变化**：旧版本默认等待 3 秒，本版本默认立即拒绝。需要排队语义的调用方
必须显式设置 `limiter.Config{WaitTimeout: ...}`。

### 四、Logo 样式

- 删除极简卡片样式常量 `LOGO_STYLE_CARD` 及对应模板，内置样式由 5 种减少为 4 种；
- `SetLogoStyle` 的合法上界改为 `LOGO_STYLE_ASCII`，越界值仍被忽略。

### 五、OpenAPI 结构化

- 顶层新增 `servers`、`tags`、`components.securitySchemes`，`info` 新增
  `termsOfService` / `contact` / `license`；
- 操作新增 `summary` / `description` / `deprecated` / `security` / `requestBody`
  与多状态码 `responses`；
- 新增声明式选项：`UseServer`、`UseTag`、`UseSecurityScheme`、`UseSchema`、
  `UseOperation`、`UseOperations` 以及 `UseTermsOfService` / `UseContact` / `UseLicense`；
- `Response.Content` 由 `map[string]any` 改为 `map[string]MediaType`：直接构造
  响应体的代码需要补一层 `openapi.MediaType{Schema: ..., Example: ...}`；
- 新增 `NewParameter`、`NewJSONResponse`、`NewJSONRefResponse`、
  `NewJSONRequestBody` 与 `SchemaRef`；
- 浏览页重做：左侧按 `tags` 分组并提供方法筛选与搜索，右侧展示参数表、
  请求体、可展开的响应体、cURL 与原始 JSON；cURL 基地址取自 `servers`。

`UseOperation` 的键兼容 `GET /a/:id`、`get /a/{id}` 与省略方法的 `/a/:id`；
未匹配到路由的声明会被忽略，不会凭空生成接口。

### 六、开发者体验界面

`harness` 前端由「顶栏 + 卡片网格」改为「左侧菜单 + 右侧功能区」：

- 10 个能力面板与平台入口，切换状态写入 URL hash，刷新后保持；
- 宽度小于 900px 时侧栏收起为抽屉，点击遮罩关闭；
- 修复分片上传读取会话 ID 时误用大写 `ID` 的问题（服务端 JSON 键为 `id`）；
- 全部路由补充业务标签，并通过 `harness/openapi.go` 声明中文摘要、参数说明、
  请求体与多状态码响应；`harness/openapi_internal_test.go` 校验声明表与真实路由、
  分组、命名模式三者一致。

### 七、h2c 改用标准库

`UseH2C()` 不再依赖 `golang.org/x/net/http2/h2c`，改由 `http.Server.Protocols`
（Go 1.24+）开启：

| 旧行为 | 新行为 |
| --- | --- |
| 启用 h2c 时 `GetHTTPHandler()` 返回 `h2c.NewHandler` 包装 | 始终返回引擎本身，h2c 由 `Protocols` 决定 |
| 同时支持 `Upgrade: h2c` 握手与前导知识 | 只支持前导知识（直接发送 HTTP/2 连接序言） |
| 需自行引入 `golang.org/x/net` | 标准库自带，`go.mod` 移除该直接依赖 |

新增 `GetHTTPProtocols()`：启用时返回 `{HTTP1, HTTP2, UnencryptedHTTP2}`，未启用时
返回 `nil`（沿用标准库默认）。保留 `HTTP2` 是为了 `RunTLS` 仍能通过 ALPN 协商到 h2。

### 八、升级步骤

1. 全局把 `leego/middleware` 替换为 `leego/middlewares`（含子包路径）；
2. 按第二节表格把旧函数改为 `New*` 构造器，并在挂载处补 `.AdaptToMiddleware()`；
3. 原 `GetMiddleware(3 * time.Second)` 一类调用改为
   `limiter.NewXxxLimiter(..., limiter.Config{WaitTimeout: 3 * time.Second})`；
4. 直接构造 `openapi.Response` 的代码把 `Content` 改为 `map[string]MediaType`；
5. 检查文档、示例、脚本中的旧包路径与 `LOGO_STYLE_CARD` 引用；
6. 若代码依赖 `GetHTTPHandler()` 的 h2c 包装，自行创建的 `http.Server` 需设置
   `Protocols: engine.GetHTTPProtocols()`；依赖 `Upgrade: h2c` 握手的客户端应改为
   直接发送 HTTP/2 连接序言，或继续使用 HTTP/1.1。

验证：`gofmt -l .` 无输出，`go build ./...`、`go vet ./...`、
`go test ./... -count=1` 全部通过（38 个包）。

---

## v0.16.0.0 — 导出命名一致性补全

> 发布日期：2026-11-20
> 要求：Go 1.27.4+

本版本把上一版漏改的导出标识符一次性对齐 `docs/conventions.md` §1.1：
首词不得为介词、名词或形容词。**旧名称全部删除，不保留 `Deprecated` 别名**，
升级时需要同步修改调用点与自定义实现。

### 一、破坏性变更：重新命名

| 旧名 | 新名 | 词性原因 |
| --- | --- | --- |
| `config.Config.OnChange` | `SubscribeChange` | `On` 是介词 |
| `config.Config.Snapshot` | `GetSnapshot` | `Snapshot` 是名词 |
| `session.From` | `GetFromContext` | `From` 是介词 |
| `session.FromRequest` | `GetFromRequest` | 同上 |
| `session.Session.ID` | `GetID` | 名词缩写 |
| `session.Session.Keys` | `GetKeys` | 名词 |
| `session.Session.Values` | `GetValues` | 名词 |
| `session.MemoryStore.Len` | `Count` | 名词缩写 |
| `job.Scheduler.Every` | `ScheduleEvery` | 限定词开头 |
| `job.Scheduler.After` | `ScheduleAfter` | `After` 是介词 |
| `gateway.Balancer.Len` | `CountUpstreams` | 名词缩写 |
| `gateway.Balancer.Next` | `PickNext` | 名词 |
| `gateway.Balancer.NextFor` | `PickNextFor` | 名词 |
| `upload.Storage.Status` | `GetStatus` | 名词 |
| `upload.LocalStorage.Status` | `GetStatus` | 同上 |
| `upload.LocalStorage.Root` | `GetRoot` | 名词 |
| `trace.Span.Duration` | `GetDuration` | 名词 |
| `trace.MemoryExporter.Spans` | `GetSpans` | 名词 |
| `trace.Tracer.Middleware` | `BuildMiddleware` | 名词 |
| `openapi.SchemaOf` | `BuildSchema` | 名词 |
| `harness.Service.Handler` | `GetHandler` | 名词 |
| `testutil.Client.JSON` | `SendJSON` | 名词 |
| `testutil.Response.JSONMap` | `GetJSONMap` | 名词 |
| `sse.Manager.Done` | `GetDoneChan` | 分词 |
| `websocket.Manager.Done` | `GetDoneChan` | 分词 |

### 二、保持不变（已写入规范）

- 函数式选项：`trace.With*` / `trace.WithoutPropagation` / `testutil.With*`
  属于 `Option` 白名单，保留原前缀；
- 标准库契约：`Len`/`Less`/`Swap`/`Push`/`Pop`（`container/heap.Interface`）、
  `Header`/`Write`/`WriteHeader`/`Flush`/`Hijack`/`ReadFrom`（`http.ResponseWriter` 族）、
  `Unwrap`/`Error`/`String`/`MarshalJSON`/`ServeHTTP`；
- `testutil.Clock.Now` 对齐 `time.Now`，不改名。

### 三、升级步骤

1. 全局替换上表旧名（IDE 重构或 `gofmt -r` 均可）；
2. 自定义 `upload.Storage` 实现需把 `Status` 方法改名为 `GetStatus`；
3. 断言旧名称是否还在字符串/反射中（如日志、配置键、模板），这些不受编译器检查。

验证：`gofmt -l .` 无输出，`go build ./...`、`go vet ./...`、
`go test ./... -count=1` 全部通过（33 个包）。

---

## v0.15.0.0 — 工程化拓展与体验服务

> 发布日期：2026-11-19
> 要求：Go 1.27.4+

本版本在 v0.14.0.0 的架构基础上完成四项工作：语言与标准库升级、热路径开销收敛、
工程化拓展包落地、开发者工具整合。除包路径下移外，核心 API 保持兼容。

### 一、语言与标准库

- `go.mod` 升级到 Go 1.27.4，构建与 CI 最低版本同步提高；
- 用 `slices`、`maps`、`strings.Cut` 等标准库能力替换手写循环（如会话密钥拷贝、
  配置键规范化、路由段拆分），减少分配与边界错误。

### 二、热路径开销

- 请求分发、限流等待、熔断统计与 SSE 投递移除可避免的 `defer`：
  只在 panic 安全、锁释放或必须保证执行的冷路径保留，并补注释说明原因；
- 限流器默认参数收敛到 `limiter.Config`（`WaitTimeout`、`PollInterval`），
  三个算法共享同一份配置，避免参数各自漂移；
- `NewTokenBucketLimiter(limit, burst, config...)` 支持可选配置，旧调用方式不变。

### 三、工程化拓展包

新增以下零第三方依赖的包（仅标准库与 `golang.org/x/*`）：

| 包 | 能力 |
| --- | --- |
| `config` | 多源配置合并、热更新与键级订阅 |
| `validate` | 结构体标签校验，规则表可扩展 |
| `problem` | RFC 7807 风格统一错误响应 |
| `session` | 签名 Cookie 会话，存储可替换 |
| `upload` | 分片上传、断点续传与 SHA-256 秒传 |
| `job` | 最小堆任务调度 |
| `gateway` | 反向代理、平滑加权轮询与标签灰度 |
| `middlewares/cache` | 响应缓存与 stale-while-revalidate |
| `middlewares/compress` | gzip 压缩中间件 |
| `middlewares/trace` | W3C 链路追踪与异步导出 |
| `metrics` | 指标存储抽象、内存实现与 Prometheus 输出 |
| `openapi` | OpenAPI 快照与浏览页 |

根包新增 `AddShutdownHook`、`Validatable` 钩子与统一的生命周期编排，
使拓展包可以幂等地挂入引擎关闭流程。

### 四、开发者工具整合（破坏性）

- `stresstest/` → `harness/stresstest/`；
- `testutil/` → `harness/testutil/`；
- `examples/` → `harness/examples/`；
- 新增 `harness` 体验服务（`harness.NewService`）与入口 `harness/cmd/harness`，
  内置单页前端、两个模拟上游、会话/缓存/限流/上传/SSE/网关/任务/链路演示；
- 新增 `harness/stresstest/test/base` 基准服务。

升级步骤：将上述三个路径的 import 前缀改为 `gitee.com/leminewx/leego/harness/`，
并确认 Go 版本不低于 1.27.4。

### 五、缺陷修复

- **会话 Cookie 在真实 HTTP 下丢失**：中间件原先在处理器写响应后才设置 Cookie，
  而 `net/http` 在 `WriteHeader` 后忽略响应头修改。现改为包装 `response.Writer`，
  在首次写响应/刷新前同步 Cookie，新建与轮换会话均能正确下发；
  补充基于 `httptest.Server` + `cookiejar` 的回归测试。

### 六、安全审计与稳定性收敛（23 项）

本版本还对根包、中间件、长连接、网关与可观测性做了一轮完整审计，修复 23 项
安全/并发/性能缺陷，并为每项补充回归测试。核心 API 不变。

**安全**

- `GetClientIP` 仅在直连对端属于 `TrustedProxies` 时解析转发头，且从右向左跳过
  可信代理，防止伪造 `X-Forwarded-For` 绕过限流与审计；
- 响应缓存键纳入 Host，默认旁路携带 Cookie 的请求，且不缓存含 `Set-Cookie` 的
  响应，避免跨身份/跨租户串味；`forbidsCache` 支持参数化指令；
- WebSocket 默认同源校验（Origin 缺失放行，跨站拒绝）；
- `metrics` / `dashboard` 管理端点支持鉴权函数；上传错误只返回白名单文案；
- 网关 502 不再回显上游地址；`Rewrite` 模式剥离客户端转发头后重新生成。

**稳定性**

- 引擎请求入口增加 panic 兜底：记录堆栈、返回 500，并保证 Context/Writer 归还；
- `job` 单任务、metrics 采集、SSE 管理、upload 后台协程均 `recover` 隔离；
- 熔断器半开探测在 panic 后回收令牌；metrics FileStore 轮转失败可自愈重开；
- 上传会话锁按引用计数回收；秒传索引改为 `index.jsonl` 追加写，重启可恢复；
- `config.OnChange` 按订阅 ID 精确取消；`validate` 阻断匿名指针环。

**性能**

- `context` 的 body 缓存超过 64 KiB 时不再驻留对象池，`BindJSON`/`BindXML`
  直接读 `bytes.Reader`，省去一次字符串拷贝；
- SSE 广播/关闭的逐连接通知限制 64 并发；`trace` 未采样且不传播时零分配；
- openapi handler 在构造期序列化文档并只读一次 UI 页面；gateway 按 `*url.URL`
  缓存 ReverseProxy；`Collector.Decrease` 原子夹零。

**升级注意**

1. WebSocket 默认拒绝跨站 Origin，独立域名部署需配置 `OriginCheckFunc` 白名单。
2. 上传秒传索引由 `index.json` 变为 `index.jsonl`，旧索引不会自动迁移，
   升级后重建一次即可。
3. `config.OnChange` 返回的 `cancel` 只取消本次订阅，不再按函数指针批量删除。
4. 长连接服务需 `leego.UseWriteTimeout(0)`；`SetCookieData` 改用 `url.PathEscape`，
   与 `GetCookieValue` 的 `url.PathUnescape` 对称，空格与 `+` 可原样往返。

验证：`gofmt -l .` 无输出，`go build ./...`、`go vet ./...`、
`go test -race -count=1 ./...` 全部通过（35 个包）。

---

## v0.14.0.0 — 全框架重构与命名收敛

> 发布日期：2026-11-19
> 要求：Go 1.25+

本次发布是对路由、上下文、响应、中间件、长连接、指标与测试体系的整体重构。
目标有三个：**热路径零锁**、**对外契约可预测**、**并发安全可被证明**。
由于函数命名与部分行为语义发生变化，这是一次破坏性升级。

### 一、破坏性变更

#### 1. 函数与方法命名（最终口径）

全部导出函数/方法统一为「动词 / be 动词 / 情态动词」开头，名词与形容词开头
的旧命名被移除；构造函数与上下文响应各形成独立命名族。主要对照（完整对照
见 [docs/conventions.md](docs/conventions.md)）：

| 旧名称 | 新名称 |
| --- | --- |
| `NewEngine()` | `NewEngine()`（保持） |
| `CreateEngine()`（重构中间版本） | `NewEngine()` |
| `Group()` | `NewGroup()` |
| `ResponseJSON` | `ResponseJSON`（保持） |
| `ResponseHTML` | `ResponseHTML`（字符串）/ `ResponseTemplate`（模板） |
| `ResponseString` | `ResponseText` |
| `ResponseSSE` | `ResponseSSE`（保持） |
| `ResponseFile` | `ResponseFile` / `ResponseAttachment` |
| `WriteJSON` / `RenderHTML` / `ServeFileContent`（重构中间版本） | `ResponseJSON` / `ResponseTemplate` / `ResponseFile` |
| `Warp`（拼写错误） | `WrapFromHTTPHandler` / `WrapFromHTTPHandlerFunc` |
| `INFO` / `WARN` | `LogInfo` / `LogWarn` |

命名口径的四条规则：

1. **构造函数**统一 `New*` / `new*`；`Create*` 仅保留给「创建持久化或外部资源」的函数。
2. **上下文响应**统一 `Response*` 前缀（`ResponseJSON`、`ResponseText`、`ResponseSSE`、
   `ResponseFile`、`ResponseAttachment` 等），不再分散为 `Write*` / `Render*` / `Send*` / `Serve*` / `Stream*`。
3. **常量**：导出常量全大写 + 下划线连接；包内私有常量 `_` 开头 + 全大写 + 下划线连接。
4. **词性**：以词典词性为准；`Group`、`Stream`、`Broadcast`、`Cache` 等兼类词可作动词首词，
   因此 `NewGroup` 不属于名词开头违规。

#### 2. 行为语义变化

- **路由注册期冻结**：`Prepare()` 之后禁止再注册路由或追加中间件，违规会 panic，
  目的是让匹配热路径完全无锁。运行期开关请使用 `Handler.Enable/Disable`。
- **405 与自动 OPTIONS**：命中路径但方法不匹配时返回 405 并带 `Allow` 头；
  未显式注册 OPTIONS 时由框架自动应答。
- **上下文池化**：`*Context` 会随请求归还池中，**不得跨请求保存**；
  需要异步使用时用 `DeriveContext` 派生。
- **WebSocket 管理模型**：改为「每连接一个读协程 + 一个写协程」，
  `Start` 变为可选的生命周期绑定方法，`Serve` 自身即为阻塞读循环。
- **管理器中不存在中央调度协程**：`Broadcast` / `SendToClient` 均为非阻塞投递，
  慢客户端的消息会被丢弃而不是拖住其它连接。

### 二、新增能力

- **指标体系**：`middlewares/collector` 提供 `Snapshot` / `Metric` / `Totals` /
  `Report`，按路由与状态码分桶，`CollectReport()` 直接产出可序列化报告。
- **指标分析后台**：`dashboard` 包内置单页后台（`go:embed`），支持过滤、排序、
  JSON / CSV 导出、健康检查，并通过 `UseProvider` / `UseAuthorizer` 扩展数据源与鉴权。
- **限流三算法**：固定窗口、滑动窗口、令牌桶，统一 `Limiter` 接口与 429 响应。
- **熔断器**：closed / open / half-open 状态机，失败率模型 + 半开探测 + 指标快照。
- **SSE**：单连接回调式与 `Manager` 托管多连接两种用法，支持心跳、关闭通知与优雅下线。
- **WebSocket**：自实现 RFC 6455 握手与帧编解码（文本/二进制/PING/PONG/CLOSE），
  校验掩码方向、控制帧长度、UTF-8 与消息大小上限。
- **JWT**：HMAC-SHA256/384/512 签发与校验，`sync.Pool` 复用解析器，
  支持时钟偏差、受众与签发方校验，并对不支持算法快速失败。
- **压测工具**：`stresstest` 提供 worker 池并发、延迟分位数与报告格式化。
- **优雅关闭**：`WaitSignalAndShutdown` 支持关闭前回调，并暴露 `GetServer` 做细粒度控制。

### 三、修复的缺陷（按模块归类）

| 模块 | 修复内容 |
| --- | --- |
| 路由 | 静态/参数/通配优先级错乱；HEAD 回退到 GET；405 缺少 `Allow`；黑名单与禁用路由的判定顺序 |
| 引擎 | 静态目录路径穿越；全局中间件链重复构建；关闭时连接与监听器泄漏；启动日志与配置并发写 |
| 上下文 | 池化对象未重置导致跨请求脏数据；绑定失败后仍写入响应；错误响应与状态码不一致；Windows 路径下附件文件名未截取 |
| 响应 | 首次 `Write` 未回填 200；`WriteHeader` 重复调用；大文件 `int64` 溢出 |
| 日志/恢复 | 颜色探测在非终端下输出控制符；panic 栈被截断；日志字段并发写 |
| JWT | 时钟偏差被丢弃；`clockSkew` 数据竞争；不支持算法被静默降级 |
| 限流/熔断 | 窗口推进竞态；令牌桶参数越界；半开状态重复放行；指标计数丢失 |
| SSE/WebSocket | 多协程写导致帧交织；关闭通道后继续发送导致 panic；首帧被缓冲读取器吞掉 |
| 指标/后台 | 失败数把 4xx 计入；聚合顺序不稳定；CSV 导出未转义；后台缺少安全响应头 |
| 压测 | 请求数分配不均；分位数插值偏乐观；错误分类不完整 |

### 四、测试与质量

- 全量测试通过 `go test ./... -count=1`，并发正确性通过 `go test -race ./...`。
- 补齐 WebSocket 升级器与连接管理测试：握手、掩码方向、分片拒绝、控制帧、
  关闭码、读写并发、心跳与连接清理。
- 覆盖率：根包 89.3%、`dashboard` 99.4%、`middlewares/collector` 99.0%、
  `response/websocket/upgrader` 93.4%、`response/websocket` 89.8%、`response/sse` 94.7%、
  `middlewares/jwt` 91.6%、`middlewares/limiter` 93.5%、`tree` 96.6%；未覆盖部分仅
  `examples/*` 与压测靶机。
- 基准覆盖路由匹配、参数解析、上下文获取、响应写入、指标聚合、帧编解码等热路径。
- 仓库统一为 `gofmt` 干净状态（LF 换行）。

### 五、文档

新增/重写：`README.md`、`docs/architecture.md`、`docs/routing.md`、`docs/context.md`、
`docs/middlewares.md`、`docs/metrics-dashboard.md`、`docs/sse-websocket.md`、
`docs/jwt.md`、`docs/limiter-breaker.md`、`docs/stresstest.md`、
`docs/testing.md`、`docs/conventions.md`、`docs/extension-roadmap.md`。

### 六、升级步骤

1. 按 [docs/conventions.md](docs/conventions.md) 的对照表全局替换旧函数名；
2. 在启动流程中显式调用 `Prepare()`（或依赖 `Run` 内部的自动冻结），
   并把运行期注册改为 `Enable/Disable`；
3. 检查是否存在跨请求保存 `*Context` 的代码，改为 `DeriveContext`；
4. WebSocket 侧确认不再依赖中央调度协程，广播失败按「丢弃慢客户端」语义处理；
5. 运行 `go build ./...`、`go vet ./...`、`go test -race ./...` 验证。

### 七、已知限制

见 [ISSUES.md](ISSUES.md)。
