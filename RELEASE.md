# 发布说明

本文件记录各版本的能力变化、破坏性变更与升级步骤。历史版本以 Git 标签为准，
这里只保留当前版本与仍然影响升级决策的条目。

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
| `middleware/cache` | 响应缓存与 stale-while-revalidate |
| `middleware/compress` | gzip 压缩中间件 |
| `middleware/trace` | W3C 链路追踪与异步导出 |
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

- **指标体系**：`middleware/collector` 提供 `Snapshot` / `Metric` / `Totals` /
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
- 覆盖率：根包 89.3%、`dashboard` 99.4%、`middleware/collector` 99.0%、
  `response/websocket/upgrader` 93.4%、`response/websocket` 89.8%、`response/sse` 94.7%、
  `middleware/jwt` 91.6%、`middleware/limiter` 93.5%、`tree` 96.6%；未覆盖部分仅
  `examples/*` 与压测靶机。
- 基准覆盖路由匹配、参数解析、上下文获取、响应写入、指标聚合、帧编解码等热路径。
- 仓库统一为 `gofmt` 干净状态（LF 换行）。

### 五、文档

新增/重写：`README.md`、`docs/architecture.md`、`docs/routing.md`、`docs/context.md`、
`docs/middleware.md`、`docs/metrics-dashboard.md`、`docs/sse-websocket.md`、
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
