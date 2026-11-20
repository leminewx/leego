# 拓展功能实现方式与代码结构（零第三方依赖）

本文是 [extension-roadmap.md](extension-roadmap.md) 的落地篇：把 12 个重点模块
与 4 个候选能力拆成**可独立提交的包、接口签名与实现步骤**，约束是只使用 Go 标准库与
`golang.org/x/{crypto,net,time}`（已在 `go.mod`）。

本文只给结构与签名，不含完整实现；实现时按第五节的切片顺序逐个 PR 落地。

> **落地状态**：4.1～4.14 已按实际实现回填；测试工具包（4.15）落在
> `harness/testutil`，压测与示例服务同在 `harness/` 下；4.16 脚手架尚未实现。
> 签名以代码为准，本文保留设计意图以便对照。

---

## 一、设计约束

| 约束 | 具体要求 | 违反后果 |
| --- | --- | --- |
| 依赖方向 | 新包可以导入根包 `leego`；根包只能导入**不反向依赖根包**的新包（当前仅 `openapi`、`metrics`），其余一律由 `main` 组装 | 循环导入，编译失败 |
| 注册期/运行期 | 所有路由与中间件在 `Prepare()` 前注册；运行期只读 | panic 或数据竞态 |
| 池化兼容 | 中间件不得在 `handler` 返回后持有 `Context` | 内存复用后数据串台 |
| 并发原语 | 配置/缓存/指标用不可变快照 + `atomic.Pointer`；通道永不关闭发送端 | `-race` 失败 |
| 崩溃边界 | 中间件不得 panic；第三方语义（压缩、缓存、上传）失败必须降级为「不处理」 | 拖垮整个请求 |

根包 `leego` 现有可复用的接线点：

- `Engine.GetRoutes() []RouteInfo`、`RouteInfo{Method, Pattern, Name, Tags, Enabled}`；
- `Context.Response response.Writer`（**导出字段，可直接替换**，缓存与压缩依赖它）；
- `Context.Fail(error)` + `HTTPError{Status, Code, Message, Err}` + `errors.As`；
- `MiddlewareFunc func(next HandleFunc) HandleFunc`、`Group.UseMiddlewares`；
- `collector.Report` / `collector.Snapshot` / `dashboard.Provider`；
- `response.Writer`（`Flush`/`Hijack`/`GetStatusCode`/`GetSize`/`Unwrap`）；
- `Engine.RegisterDashboard(prefix, options...)`。

---

## 二、目录结构（落地后）

下面是仓库当前的实际布局；括号内「A 档」表示只用标准库。设计稿把文件拆得更细，
实现时按真实耦合合并，差异见本节末尾两张对照表。

```
leego/
├── config/                     # 配置加载与热更新（A 档）
│   ├── config.go               # Config + SubscribeChange + Watch（os.Stat 轮询）
│   ├── source.go               # Source 接口 + JSON / Env / Flag 源
├── validate/                   # 标签校验（A 档）
│   ├── validate.go             # Check / CheckVar + structCache（sync.Map）
│   ├── rule.go                 # 内置规则表
├── problem/                    # RFC 7807 错误模型（A 档）
│   └── problem.go              # Problem / FieldError / NewFromValidation / Write
├── openapi/                    # OpenAPI 3.1 + 内嵌浏览页（不导入 leego）
│   ├── openapi.go              # Spec / Options / 声明式覆盖与多响应
│   ├── schema.go               # reflect → JSON Schema
│   ├── handler.go              # 文档 JSON 与 UI 两个 http.Handler
│   └── assets/index.html       # go:embed 自写页面（分组导航 + 结构化详情）
├── metrics/                    # 指标持久化与导出（A 档）
│   ├── store.go                # Store 接口 + MemoryStore / FileStore（JSONL 回放）
│   ├── series.go               # 环形序列与时间范围聚合
│   └── prometheus.go           # 文本格式编码器
├── middlewares/
│   ├── compress/               # gzip（compress.go + pool.go）
│   ├── cache/                  # 响应缓存（cache.go + store.go + writer.go）
│   └── trace/                  # traceparent（trace.go + traceparent.go + exporter.go）
├── session/                    # 会话（cookie.go + session.go + store.go）
├── upload/                     # 分片上传（upload.go + local.go + handler.go）
├── job/                        # 进程内调度（job.go）
├── gateway/                    # 反向代理与灰度（gateway.go + proxy.go）
└── harness/
    ├── harness.go              # 体验服务装配与生命周期
    ├── cmd/harness/main.go     # 可执行入口
    ├── testutil/               # 测试客户端、断言与冻结时钟
    ├── stresstest/             # 压测客户端，test/base 为靶机
    ├── examples/               # base / file / sse / websocket / stress
    └── statics/index.html      # 体验前端
```

根包新增的适配文件共 4 个（另有同名测试）：`validatable.go`、`openapi.go`、
`metrics.go`、`lifecycle.go`。

### 设计稿与实现的文件对照

| 设计稿 | 实现 | 原因 |
| --- | --- | --- |
| `config/watch.go` | 拆到 `source.go`（`JSONSource.Watch` 轮询）与 `config.go`（`Watch` 汇总停止函数） | 轮询只是 `os.Stat` + FNV 哈希，不足百行 |
| `validate/cache.go` | 并入 `validate.go` 的 `structCache` | 一个 `sync.Map` 加一个解析函数 |
| `problem/writer.go`、`i18n.go` | 并入 `problem.go`；未做 i18n | 写响应约 20 行；消息表未纳入范围 |
| `openapi/spec.go`、`builder.go` | 并入 `openapi.go` | Spec 构建与路由适配是同一逻辑单元 |
| `metrics/provider.go` | 未单独实现 | 趋势数据由根包 `RegisterMetrics` 直接适配 |
| `metrics/metrics.go` | 仅 `package` 声明 | 预留占位 |

### 尚未落地的规划项

| 规划 | 状态 |
| --- | --- |
| `middlewares/secure`（请求签名与防重放） | 未实现 |
| `cmd/leego` 脚手架 | 未实现；仓库只有 `harness/cmd/harness` |
| `contrib/{redis,otel,s3}` | 未实现，保留独立子模块约定 |
| WebSocket 分片 / `permessage-deflate` / 房间 / ACK | 未实现；分片帧先发 1003 关闭帧 |
| SSE `Last-Event-ID` 回放 / `Broadcaster` | 未实现；仅保留 `Message.Id` 字段 |

---

## 三、包依赖图

```
                         ┌───────────────────┐
                         │  harness/cmd/main │
                         └────────┬──────────┘
                                  │
   config  validate  problem  session  upload  job  gateway  harness/testutil
        │       │        │        │       │      │       │        │
        └───────┴────────┴────────┴───────┴──────┴───────┴────────┘
                                   │ 导入
                          ┌────────▼────────┐
                          │   leego（根）    │
                          └────────┬────────┘
                                   │ 导入
        ┌──────────────┬───────────┼─────────────┬──────────────┐
        │              │           │             │              │
     tree/         response/    headers/    collector/      dashboard/
                                   │
                     middlewares/{compress,cache,trace}
                                   │
                         metrics ──┴──> collector + dashboard.Provider

   openapi：只依赖标准库；metrics：只依赖 collector + dashboard
                    （二者不导入 leego，因此可由根包提供适配方法）
```

规则：**箭头只能向上**。`openapi` 与 `metrics` 是仅有的两个「根包向下调用」的包，
因此它们必须不导入 `leego`：openapi 用自定义 `Route` 结构承接，
metrics 只用 `collector`/`dashboard` 的公开结构。

---

## 四、逐模块设计

### 4.1 `config`：配置加载与热更新

**文件职责**

| 文件 | 内容 |
| --- | --- |
| `config.go` | `Config`、`Load`、`Bind`、`SubscribeChange`、`Watch`（汇总各源的停止函数）、快照替换 |
| `source.go` | `Source` 接口；`JSONSource`（内含 `os.Stat` 轮询与 FNV 哈希比较）、`EnvSource`、`FlagSource`、`MapSource` |

**核心签名**

```go
// Source 定义配置来源。Watch 在数据变化时回调 notify，返回停止函数。
type Source interface {
	Load() (map[string]any, error)
	Watch(notify func(map[string]any)) (stop func(), err error)
}

// subscription 是订阅实例：取消时必须按 id 精确删除。
type subscription struct {
	id     uint64
	notify func(any)
}

// Config 是不可变快照的持有者，可被并发读取。
type Config struct {
	values    atomic.Pointer[map[string]any] // 只读快照，整体替换
	subsMu    sync.Mutex
	subs      map[string][]subscription      // key → 订阅列表
	nextSubID uint64
}

func Load(sources ...Source) (*Config, error)
func (own *Config) Get(key string) (any, bool)
func (own *Config) GetString(key, fallback string) string
func (own *Config) GetDuration(key string, fallback time.Duration) time.Duration
func (own *Config) Bind(target any) error          // 标签 config:"server.read_timeout"
func (own *Config) SubscribeChange(key string, fn func(any)) (cancel func())
func (own *Config) GetSnapshot() map[string]any     // 返回浅拷贝，供诊断页展示
```

**实现步骤**

1. 合并顺序：默认值 → 文件 → 环境变量 → 命令行；键名统一转小写并用 `.` 分层。
2. `Bind` 复用根包已有的反射经验：`reflect` 遍历字段，支持 `config:"a.b"`、
   `default:"..."`、`time.Duration`/`int64`/`bool`/`[]string` 的转换。
3. 热更新：`watcher` 每 1s `os.Stat`，`ModTime + Size` 变化时重读，
   算出 `hash/fnv` 摘要与旧值比较；不同则构造新 map 并 `Store`，
   再按 key 差异派发 `SubscribeChange`。
4. 订阅标识：`SubscribeChange` 为每次调用分配自增 ID；`cancel` 只删除自己的 ID，
   即使同一函数字面量被注册两次，也不会误删兄弟订阅。
5. 生命周期：`Watch` 的协程由 `stop()` 停止；`stop` 幂等（`sync.Once`）。

**接线点**：`main` 中加载后手动应用启动期参数
（`leego.WithEngineName(cfg.GetString(...))`）；运行期参数通过 `SubscribeChange`
回调写进 `limiter`/`breaker` 的可变配置，不触碰 `EngineConfig`。

**测试点**：优先级合并、类型转换失败、并发 `Get` 与替换（`-race`）、
`SubscribeChange` 只对变化 key 触发、兄弟订阅取消互不影响、`stop` 幂等。

---

### 4.2 `validate` + `problem`：校验与统一错误码

**核心签名**

```go
// validate
type FieldError struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
	Param string `json:"param,omitempty"`
	Value any    `json:"value,omitempty"`
}

type Errors []FieldError // 实现 error；空切片视为 nil
func (own Errors) Error() string

// RuleFunc 判断字段是否满足规则；param 为标签中逗号后的参数。
type RuleFunc func(value reflect.Value, param string) bool

func RegisterRule(name string, rule RuleFunc) error
func Check(target any) error            // 返回 Errors 或 nil
func CheckVar(value any, tags string) error   // 校验单个值
```

```go
// problem：RFC 9457
type Problem struct {
	Type     string       `json:"type,omitempty"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Code     string       `json:"code,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

func (own *Problem) Error() string
func (own *Problem) Write(ctx *leego.Context) // Content-Type: application/problem+json
func NewFromValidation(err error) *Problem    // validate.Errors → 422
func NewFromError(err error) *Problem         // 校错/HTTPError/其他 → 422/对应码/500
```

**实现步骤**

1. 元信息缓存：`sync.Map[reflect.Type]*structInfo`，`structInfo` 只存
   字段下标、标签解析结果与嵌套路径，避免每请求解析标签。
2. 标签解析：`validate:"required,min=1,max=64,email"`；`param` 取逗号后的内容；
   未知规则在 `Check` 首次执行时返回错误（不要静默忽略）。
3. 值获取：`reflect.Value` 逐层解引用；指针为 nil 且 `required` 时报错，
   否则跳过（语义与常见校验库一致）。
4. 匿名嵌套：展开匿名结构体时记录 `visited` 类型集合，遇到已访问类型直接跳过，
   防止 `A → *B → *A` 这类互嵌指针导致无限递归。
5. `dive`：切片/数组/map 逐元素递归，路径拼成 `items[0].name`。
6. `problem.Write`：先 `ctx.GetStatusCode() != 0` 判断，已写响应则只记录不覆盖。

**接线点**：根包新增 5 行接口（第六节），`Bind` 成功后自动调用：

```go
// validatable.go（根包）
type Validatable interface{ Validate() error }
```

业务 DTO 实现 `Validate()` 转发到 `validate.Check(own)`，即可零耦合获得校验。

**测试点**：标签全部规则、嵌套与 `dive` 路径、nil 指针、匿名指针环、
并发首次解析、`Problem` JSON 字段顺序与 Content-Type。

---

### 4.3 `openapi`：OpenAPI 3.1 生成

**为什么它不能导入 `leego`**：若 `Engine.RegisterOpenAPI` 存在，则依赖是
`leego → openapi`；若 `openapi` 再导入 `leego` 就成环。因此 `openapi`
只认自己的最小结构。

**核心签名**

```go
type Route struct {
	Method, Pattern, Name string
	Tags                  []string
	Summary, Description  string
	Deprecated            bool
	Parameters            []Parameter
	RequestBody           *RequestBody
	Responses             map[string]Response
}

type RouteSource interface{ GetRoutes() []Route }

// Options 聚合文档级元数据；Operations 是声明式覆盖表，
// 键为 "METHOD 路径模板"，未匹配到路由时声明被忽略。
type Options struct {
	Title, Version, Description, TermsOfService string
	Contact         *Contact
	License         *License
	Servers         []Server
	Tags            []Tag
	SecuritySchemes map[string]SecurityScheme
	Schemas         map[string]any    // 命名模式，供 $ref 引用
	Operations      map[string]Operation
}

type Spec struct { OpenAPI, Info, Paths, Components }
func NewSpec(source RouteSource, options ...Option) *Spec
func (own *Spec) MarshalJSON() ([]byte, error)       // 稳定排序，便于 diff
func (own *Spec) AddOperation(method, pattern string, op Operation)
func (own *Spec) AddSchema(name string, value any)   // reflect → Schema

// 声明式构造函数
func WithServer/WithTag/WithSecurityScheme/WithSchema/WithOperation(...)
func NewParameter(name, in, description string, required bool, schema map[string]any) Parameter
func NewJSONResponse(description string, example any) Response
func NewJSONRefResponse(description, schemaName string) Response
func NewJSONRequestBody(required bool, example any) *RequestBody
func SchemaRef(name string) map[string]any

func NewHandler(spec *Spec) http.Handler  // GET {prefix}/openapi.json
func NewUIHandler(spec *Spec) http.Handler // 自写 HTML + 内嵌 JS
```

**实现步骤**

1. 路径转换：`/users/:id` → `/users/{id}`；`*path` → `/{path}`（标记为 wildcard），
   路径参数自动转为 `in: path` 且 `required: true`。
2. 每个 `Route` 生成 `PathItem` 的一个方法项，`operationId` 用
   `Name` 或 `method + pattern` 的规范化串；未声明响应时写入默认 `200 OK`。
3. 声明式覆盖：`normalizeOperationKey` 兼容 `GET /a/:id`、`get /a/{id}` 与
   省略方法的 `/a/:id`；`mergeOperation` 只替换显式填写的字段，
   因此自动推导的 `operationId` 与路径参数不会丢失。
4. `Schema` 生成：基本类型 → `type/format`；结构体 → `object + properties`；
   切片 → `array + items`；`time.Time` → `string/date-time`；`any` → `{}`；
   深度上限 8 层，超出回退 `{"type":"object"}`。
5. 结构化响应：`Response.Content` 为「媒体类型 → `MediaType`」，
   大对象用 `WithSchema` + `NewJSONRefResponse` 生成 `#/components/schemas/...`，
   多个接口共用同一份模式；SSE 与下载分别用 `text/event-stream`、
   `application/octet-stream`。
6. UI：`embed` 一个自写页面（左侧按 `tags` 分组、支持方法筛选与搜索，
   右侧渲染参数表、请求体、可展开响应体、cURL 与原始 JSON），
   纯 `fetch` + 原生 DOM，不引 CDN；cURL 基地址取自 `servers`。
7. 快照语义：`RegisterOpenAPI` 在 `Prepare()` 前构建 `Spec`，
   运行期只读；路由变更需重新构建（注册期语义）。
8. 构造期缓存：`NewHandler` 在创建时完成一次 `MarshalJSON`，成功则缓存字节、
   失败则缓存错误（每请求直接 500，文案不包含内部细节）；`NewUIHandler` 只
   `ReadFile` 一次并缓存页面，避免每请求读盘。

**接线点**（根包，约 25 行适配器）：

```go
func (own *Engine) RegisterOpenAPI(prefix string, options ...openapi.Option) error {
	routes := own.GetRoutes()
	source := &routeAdapter{routes: routes} // RouteInfo → openapi.Route
	spec := openapi.NewSpec(source, options...)
	// 注册 openapi.json 与 UI 两条路由
}
```

**测试点**：模式转换（含 wildcard）、schema 递归与深度截断、
JSON 输出稳定性（两次序列化字节一致）、声明键的多种写法、
未注册模式的 `$ref` 能被发现、UI 页面可离线加载。

---

### 4.4 `metrics`：指标持久化与导出

**核心签名**

```go
type Point struct {
	At        time.Time
	Total     uint64
	Failed    uint64
	AvgMicro  int64
	P95Micro  int64
}

type TimeSeries struct{ /* 环形桶 + 互斥锁 */ }
func NewTimeSeries(window time.Duration, buckets int) *TimeSeries
func (own *TimeSeries) Append(at time.Time, snapshot collector.Snapshot)
func (own *TimeSeries) Query(from, to time.Time, step time.Duration) []Point

type Store interface {
	Append(snapshot collector.Snapshot) error
	Query(from, to time.Time, step time.Duration) ([]Point, error)
	Close() error
}

func NewMemoryStore(window time.Duration, buckets int) Store
func NewFileStore(path string, window time.Duration, buckets int) (Store, error)

func NewPrometheusHandler(source func() collector.Report) http.Handler
```

**实现步骤**

1. `TimeSeries`：固定桶数（默认 60）+ 桶宽 = window/buckets；
   `Append` 时推进桶指针并累加；`Query` 按 step 聚合（均值/最大值）。
2. `FileStore`：每行一条 JSON（`Snapshot` + 时间戳），`bufio.Writer` 批量刷盘；
   启动时逐行解码回填，**末尾半行直接丢弃**（进程被 kill 的残留）。
3. 轮转：按大小（默认 64 MiB）切分 `metrics-<unix>.jsonl`，保留最近 5 个文件。
4. Prometheus：手写编码器，输出 `# HELP/# TYPE` + 累加计数；
   label 值按规范转义 `\\`、`\n`、`"`；按 `method + pattern` 稳定排序。
5. `dashboard.Provider` 实现：把 `Query` 结果映射为 dashboard 的趋势数据结构。

**接线点**：`main` 中 `engine.RegisterMetrics("/metrics", store)`（根包适配方法，
见第六节），或直接用 `Engine.GetRoutes()` + `collector` 自行组装并输出。

**测试点**：环形推进与边界（window 跨越）、JSONL 尾部损坏恢复、轮转、
Prometheus 文本与官方格式逐字对比、并发写入（`-race`）。

---

### 4.5 `middlewares/compress`：gzip 压缩

**核心签名**

```go
type Config struct {
	Level   int      // 默认 -1（flate.DefaultCompression）
	MinSize int      // 响应体小于该值不压缩，默认 1024
	Types   []string // 默认 text/*, application/json, application/xml, application/javascript
}

func New(config ...Config) leego.MiddlewareFunc
```

**实现步骤**

1. `writerPool sync.Pool` 复用 `gzip.Writer`；请求结束时 `Reset(io.Discard)` 后归还。
2. 包装器实现 `response.Writer`（`Flush` 先 `gzip.Flush` 再底层 `Flush`）。
3. `WriteHeader` 时决定是否压缩：`Content-Type` 命中 + 无 `Content-Encoding` +
   方法非 `HEAD` + 非 `text/event-stream` + 非 `101 Switching Protocols`。
4. `Unwrap()` 返回底层 writer，保证 `http.ResponseController` 与 sendfile 仍可用。
5. 不压缩已 `Hijack` 的连接（WebSocket）：`Hijack` 直接委托底层，
   并标记 `skipCompress`，后续 `Write` 透传。

**注意**：包装器 `GetSize()` 返回**实际写出的压缩后字节**，与 collector 统计口径一致；
若需要原始大小，另设 `GetOriginalSize()`，不改变 `response.Writer` 契约。

---

### 4.6 `middlewares/cache`：响应缓存

**核心签名**

```go
type Config struct {
	TTL      time.Duration // 默认 30s
	MaxBytes int64         // 默认 64 MiB
	MaxBody  int64         // 单条响应上限，超出不缓存，默认 1 MiB
	Vary     []string      // 默认 Authorization / Accept-Encoding
	Methods  []string      // 默认 GET / HEAD
}

func New(config ...Config) leego.MiddlewareFunc
```

**实现步骤**

1. 缓存键：`method + host + ctx.GetURL().Path + query(raw) + vary 头摘要`
   （SHA-256 前 16 字节）；Host 参与计算，避免同一进程多租户/多域名串味。
2. `Recorder` 实现 `response.Writer`：镜像 body 到 `bytes.Buffer`，
   超过 `MaxBody` 标记 `overflow` 并停止镜像（仍透传）。
3. 命中：直接写 `header + status + body`，设置 `X-Cache: HIT`，**不调用 next**。
4. 未命中：`ctx.Response = recorder`（利用导出字段），`next(ctx)`；
   返回后若状态码 ∈ {200}、响应无 `Set-Cookie`、未 `overflow`、未 `Hijack`、
   非 `text/event-stream`，存入 LRU（`container/list` + map，按字节淘汰）。
   默认旁路携带 Cookie 的请求（Cookie 通常代表身份且不在 Vary 中），
   与身份无关的路由可显式开启。
5. `stale-while-revalidate`：过期条目先返回旧值并设置 `X-Cache: STALE`，
   同时用 `singleflight`（仓库内自研，20 行）触发一次后台刷新。

**关键约束**：`Recorder` 必须在 `next` 返回后立刻 `Flush` 并归还；
不得在缓存命中路径上复用 `Context` 之外的状态。

---

### 4.7 `middlewares/trace`：W3C traceparent 传播

**核心签名**

```go
type Span struct {
	TraceID  string
	SpanID   string
	ParentID string
	Name     string // 优先用路由 pattern
	Start    time.Time
	End      time.Time
	Status   int
	Attrs    map[string]string
}

type Exporter interface{ Export(ctx context.Context, spans []Span) error }

func New(exporter Exporter, options ...Option) leego.MiddlewareFunc
func TraceFrom(ctx context.Context) (*Span, bool)
func ParseTraceparent(value string) (traceID, parentID string, sampled bool, ok bool)
func FormatTraceparent(traceID, spanID string, sampled bool) string
```

**实现步骤**

1. 解析：`00-<32hex>-<16hex>-<01>`，长度与十六进制严格校验；非法则新建。
2. 生成 ID：`crypto/rand` 读 16/8 字节后 `hex.Encode`；禁止用时间戳或伪随机。
3. 生命周期：span 存 `ctx.Request.WithContext(...)`，仅在本次请求内有效；
   `End` 在 `next` 返回后设置，交给带缓冲 channel 的批量导出协程。
4. 导出器：接口 + `logExporter`（默认打印）+ `noopExporter`。
5. 零分配快路径：未采样且未开启传播时，不解析/生成 ID、不创建 Span，
   直接调用 next，避免为被丢弃的链路付出内存与 CPU。

---

### 4.8 `session`：服务端会话

**核心签名**

```go
type Values map[string]any

type Store interface {
	Get(ctx context.Context, id string) (Values, error)
	Save(ctx context.Context, id string, values Values, ttl time.Duration) error
	Destroy(ctx context.Context, id string) error
}

func New(store Store, options ...Option) leego.MiddlewareFunc
func GetFromContext(ctx context.Context) (*Session, bool)
func GetFromRequest(request *http.Request) (*Session, bool)
```

**实现步骤**

1. 内存 Store：`map[string]*entry` + `sync.RWMutex`；后台清理协程按 `ttl/2` 扫描。
2. Cookie：`base64url(payload).base64url(hmac)`，HMAC-SHA256；
   校验 `SameSite=Lax`、`HttpOnly`、`Secure`（可配置）。
3. ID 生成：`crypto/rand` 32 字节；**轮换**：登录成功后更换 ID，防会话固定。
4. 请求结束统一 `Save`（`defer`），未修改则不写。

---

### 4.9 `middlewares/jwt` 扩展：RS256 / ES256 / JWKS

扩展现有包，不新建包。

```go
func NewRSASigner(algorithm Algorithm, key *rsa.PrivateKey, kid string) (Signer, error)
func NewECDSASigner(algorithm Algorithm, key *ecdsa.PrivateKey, kid string) (Signer, error)
func ParsePrivateKeyPEM(data []byte) (any, error)   // PKCS#1 / PKCS#8 / EC
func ParsePublicKeyPEM(data []byte) (any, error)

// KeySet 按令牌头部 kid 选择验签器。
type KeySet struct{ /* kid → Signer */ }
func NewKeySet(signers ...Signer) *KeySet
func (own *KeySet) Add(signer Signer) error
```

实现要点：签名算法标识必须来自**配置**而非令牌（防 alg 混淆）；
`none` 与未知算法一律拒绝；ECDSA 签名注意 `r||s` 定长拼接（`asn1` 不用于 JWS）。

---

### 4.10 WebSocket 现状与规划

扩展现有 `response/websocket`。已落地的是 RFC 6455 基础能力，分片、压缩、
房间与 ACK 均未实现：

| 能力 | 文件 | 状态 | 说明 |
| --- | --- | --- | --- |
| 握手与配置 | `upgrader/upgrader.go`、`upgrader/config.go` | 已实现 | `ReadTimeout`/`WriteTimeout`/`MaxReadLimit`/`Subprotocols`/`OriginCheckFunc`，默认同源校验 |
| 帧编解码 | `upgrader/frame.go`、`upgrader/conn.go`、`upgrader/code.go`、`upgrader/close.go` | 已实现 | 掩码必填校验、控制帧 ≤125 字节且不可分片、文本 UTF-8 校验、关闭码回显 |
| 连接管理 | `manager.go`、`client.go`、`message.go` | 已实现 | 每连接单写者 + 读/写协程，非阻塞投递 |
| 分片消息 | — | 未实现 | `FIN=0` 与 continuation 帧先发 1003 关闭帧，再断开连接 |
| permessage-deflate | — | 未实现 | 未协商任何扩展，任一 `RSV` 位非 0 直接断开连接 |
| 房间 | — | 未实现 | 需在 `Manager` 增加 `rooms` 索引与按房间广播 |
| ACK / 重发窗口 | — | 未实现 | 需新增消息类型与定时器 |

---

### 4.11 SSE 现状与规划

| 能力 | 文件 | 状态 | 说明 |
| --- | --- | --- | --- |
| 单写者 + 心跳 | `client.go` | 已实现 | `messageChan` 缓冲（默认 8）、`SetWriteTimeout`、`SetHeartbeatPeriod`；`Close` 只关 `closeChan`，永不关消息通道 |
| 事件 ID 字段 | `message.go` | 已实现 | `Message.Id` 写成 `id:` 行，供客户端重连时回传 `Last-Event-ID` |
| `Last-Event-ID` 回放 | — | 未实现 | Manager 无环形缓冲，重连后不补发历史事件 |
| 跨副本广播 | — | 未实现 | 无 `Broadcaster` 接口，`Broadcast` 仅限进程内 |
| 背压丢弃 | — | 未实现 | 队列满时按 `writeTimeout` 返回错误，不会“丢最旧” |

若后续要做跨副本，建议的接口形态（当前仅设计）：

```go
type Broadcaster interface {
	Publish(ctx context.Context, topic string, msg *Message) error
	Subscribe(ctx context.Context, topic string) (<-chan *Message, func(), error)
}
```

---

### 4.12 `upload`：分片上传

```go
type Part struct {
	Index int   `json:"index"`
	Size  int64 `json:"size"`
	Hash  string `json:"hash,omitempty"`
}

type Session struct {
	ID       string `json:"id"`
	Size     int64  `json:"size"`
	PartSize int64  `json:"part_size"`
	Parts    []bool `json:"parts"`
}

type Storage interface {
	Init(ctx context.Context, request InitRequest) (Session, error)
	GetStatus(ctx context.Context, id string) (Session, error)
	PutPart(ctx context.Context, id string, index int, r io.Reader, size int64) error
	Complete(ctx context.Context, id string, parts []Part) (string, error)
	Abort(ctx context.Context, id string) error
}

func NewLocalStorage(root string) Storage
func NewHandler(storage Storage, options ...Option) leego.HandleFunc
```

实现要点：会话元信息以 JSON 文件落在 `root/.sessions/<id>.json`，
并发用 per-session `sync.Mutex`（`sync.Map` 存锁 + 引用计数，最后一个使用者
释放后回收）；`Complete` 按序拼接并校验 SHA-256；秒传索引是 `root/index.jsonl`
（每行 `{sum,path}` 追加写并 `Sync`，加载时跳过损坏行），旧版 `index.json`
不会自动迁移；对外错误只返回白名单文案，不回显内部路径；`Abort` 清理目录。

---

### 4.13 `job`：进程内调度

```go
type Job interface{ Run(ctx context.Context) error }

type Scheduler struct{ /* 最小堆 + 单协程 */ }
func NewScheduler(options ...Option) *Scheduler
func (own *Scheduler) ScheduleEvery(interval time.Duration, job Job) (cancel func())
func (own *Scheduler) ScheduleAfter(d time.Duration, job Job)
func (own *Scheduler) Start(ctx context.Context)
func (own *Scheduler) Stop(ctx context.Context) error // 等待在途任务
```

实现要点：`container/heap` 按 `nextRun` 排序；单协程 `select` 定时器 + 取消；
Worker 数有界（默认 `GOMAXPROCS`），队满拒绝并计数；每个任务在独立协程中执行
并 `recover`，单个任务 panic 不会杀死调度器；语义明确为 **at-most-once**
（进程崩溃则丢失），写入包注释。

---

### 4.14 `gateway`：反向代理与灰度

```go
type Upstream struct {
	URL    *url.URL
	Weight int
}

func NewBalancer(upstreams ...Upstream) (*Balancer, error) // 加权轮询（平滑加权）
func (own *Balancer) CountUpstreams() int                 // 上游数量
func (own *Balancer) PickNext() *url.URL                 // 全局加权轮询
func (own *Balancer) PickNextFor(tag string) *url.URL    // 优先同标签，未命中回落全局
func NewProxy(balancer *Balancer, options ...Option) leego.HandleFunc
```

实现要点：每个上游一个 `httputil.ReverseProxy` 实例（`Rewrite` 模式，按
`*url.URL` 指针复用）并共享连接池；`Rewrite` 模式下标准库会先剥离客户端自带的
`Forwarded` / `X-Forwarded-*`，再由 `SetXForwarded` 写入真实来源；灰度按 header
命中或权重分流；`FlushInterval` 为负数时支持 SSE 流式透传；`ErrorHandler` 通过
`OnError` 回调快速失败；超时预算通过 `X-Request-Timeout` 向下游传递；502 只返回
固定文案，不泄露上游地址。

---

### 4.15 `harness/testutil`：测试工具包

```go
func NewClient(handler http.Handler) *Client
func (own *Client) WithHeader(key, value string) *Client
func (own *Client) WithCookie(cookie *http.Cookie) *Client
func (own *Client) Get(path string, options ...RequestOption) *Response
func (own *Client) SendJSON(method, path string, body any, options ...RequestOption) *Response

type Response struct {
	Status  int
	Header  http.Header
	Body    []byte
	Cookies []*http.Cookie
	Err     error // 请求构造阶段错误；非 nil 时其余字段无意义
}

func (own *Response) AssertStatus(t *testing.T, want int) *Response
func (own *Response) AssertHeader(t *testing.T, key, want string) *Response
func (own *Response) AssertContains(t *testing.T, want string) *Response
func (own *Response) BindJSON(t *testing.T, target any)
func (own *Response) GetJSONMap(t *testing.T) map[string]any
func (own *Response) GetCookie(name string) (*http.Cookie, bool)

type Clock interface{ Now() time.Time }
func NewFrozenClock(at time.Time) *FrozenClock
func (own *FrozenClock) Advance(delta time.Duration)
func (own *FrozenClock) Set(at time.Time)
```

实现要点：全程 `httptest.ResponseRecorder`，进程内调用，不监听端口；
`RequestOption`（`WithHeader`、`WithCookie`、`WithQuery`、`WithRawQuery`）为函数式选项，
`WithHeader` / `WithCookie` 固定在客户端上对所有请求生效；断言失败直接 `t.Fatal`，
并返回响应本身以支持链式断言；`NewFrozenClock` 用于稳定时间断言（不改根包时钟）。

---

### 4.16 `cmd/leego`：脚手架（未实现）

```go
// 规划：只用 flag.NewFlagSet 实现子命令：new / gen handler / routes
go:embed templates/**   // 工程骨架模板
```

实现要点（若后续启动）：`leego new` 生成目录 + 配置模板 + Dockerfile；
`leego gen handler` 生成 handler/测试/注册片段；
`leego routes` 通过替换 `main` 中的空壳 `Engine` 调用 `GetRoutes()` 输出表格。
当前仓库只提供 `harness/cmd/main.go` 体验服务，不包含脚手架命令。

---

## 五、六个关键实现难点

### 5.1 配置热更新：不可变快照 + 差异派发

```go
func (own *Config) apply(next map[string]any) {
	previous := own.values.Swap(&next)
	if previous == nil { return }

	// 只对发生变化的 key 派发订阅，避免“任何字段变更触发全部回调”；
	// 每个订阅持自增 ID，取消时不会误删同一函数的其它订阅。
	for key, subscriptions := range own.subsSnapshot() {
		if v, ok := next[key]; ok && !equalValue(v, (*previous)[key]) {
			for _, callback := range subscriptions {
				go safelyNotify(callback.notify, v)
			}
		}
	}
}
```

坑：`Swap` 返回的是**旧指针**，不能在回调里读 `own.values`；
否则高频写入时回调看到的是更新后的值，丢失变更顺序。
回调必须异步派发（带缓冲 channel），否则慢回调会阻塞配置监听协程。

### 5.2 压缩不与 SSE / WebSocket 冲突

判定顺序（在 `WriteHeader` 中一次性决定，之后不再变）：

```
1. 已是 101（Switching Protocols） → 不压缩
2. Content-Type 为 text/event-stream → 不压缩
3. 无 Content-Type → 延迟到首次 Write 再判定（默认不压缩）
4. 已有 Content-Encoding → 不压缩
5. 方法为 HEAD / 状态码为 204、304 → 不压缩
6. 否则设置 Content-Encoding: gzip、删除 Content-Length
```

坑：`Flush()` 必须先刷 `gzip.Writer` 再刷底层，否则 SSE 之外的分块响应
会出现“头部到了、数据没到”的假死。WebSocket 升级路径必须 `Hijack` 透传，
压缩包装器不能碰 `net.Conn`。

### 5.3 缓存回放与池化 Context

```go
recorder := newRecorder(ctx.Response, config.MaxBody)
ctx.Response = recorder      // 利用导出字段替换
next(ctx)                    // 业务正常写响应
ctx.Response = recorder.inner // 归还前恢复，避免 Collector 统计错对象

if recorder.cacheable() {
	store.Set(key, recorder.entry())
}
recorder.release()           // 归还 buffer，禁止保留任何 []byte 引用
```

坑：命中路径直接写缓存时，`ctx.Response.GetSize()` 会小于实际字节（没有走过
业务写路径），需要在写完后手动 `AddSize` 或改用 “构造一个计数 writer 包一层”
的方案，保证访问日志与指标一致。

### 5.4 为什么暂不支持 WebSocket 分片与压缩

RFC 6455 的分片与 permessage-deflate 需要连接维护 `inFragment`、`deflateIn`、
`deflateOut` 三个状态位，还要处理“控制帧可插在分片中间”“RSV1 仅在首帧置位”
等边界。当前实现在 `Conn.Read` 里直接拒绝：

| 触发条件 | 结果 |
| --- | --- |
| 数据帧 `FIN=0`，或收到 `OP_CODE_CONTINUE` | 先发 1003（unsupported data）关闭帧，再断开并返回错误 |
| 任一 `RSV` 位非 0（含 RSV1） | 返回协议错误并断开，不发送关闭帧 |
| 控制帧 `FIN=0` 或载荷 > 125 字节 | 同上（协议错误断开） |

代价是单条消息必须一次写完、无法协商压缩；收益是状态机小到可以完整测试，
且不会因半途分片导致内存无上限累积（`MaxReadLimit` 只约束单帧）。
需要传输超大载荷时，调用方应自行拆成多条独立消息。

### 5.5 traceparent 的严格性

必须拒绝：长度不为 55、版本非 `00`、全零 traceID、全零 parentID、
非小写十六进制。拒绝时**新建** trace 并把 `tracestate` 丢弃，
不要尝试“修复”非法输入——那会让上下游 trace 串联出错。

### 5.6 FileStore 的崩溃一致性

- 每条记录以 `\n` 结尾，写入后立即 `Flush`（可靠性优先于吞吐）；
- 启动回放时，最后一行若 `json.Unmarshal` 失败则**截断该行**并继续；
- 轮转用“先建新文件、再切指针、最后关旧文件”，避免丢记录；
- 文件权限 `0o600`，目录 `0o700`（指标可能含路由名等内部信息）。

---

## 六、对根包的最小改动（共 5 处，约 80 行）

| 文件 | 改动 | 行数 | 目的 |
| --- | --- | --- | --- |
| `context.go` | 4 个 `Bind*` 成功后调用 `Validate()`（若实现 `Validatable`） | ~12 | 校验零耦合接入 |
| `validatable.go`（新） | `type Validatable interface{ Validate() error }` | ~5 | 同上 |
| `openapi.go`（新） | `RegisterOpenAPI(prefix, options...)`，内部做 `RouteInfo → openapi.Route` 适配 | ~25 | 文档生成 |
| `metrics.go`（新） | `RegisterMetrics(prefix, store)`，注册 `/metrics` 与历史查询路由 | ~20 | 指标导出 |
| `lifecycle.go`（新） | `AddShutdownHook(func(context.Context) error)`，`Shutdown` 时逆序执行 | ~20 | `job`/`metrics` 优雅关闭 |

`Bind*` 的接入方式（以 `BindJSON` 为例）：

```go
if err := json.NewDecoder(own.Request.Body).Decode(object); err != nil {
	return err
}

// 新增 3 行：仅在目标实现 Validatable 时校验。
if v, ok := object.(Validatable); ok {
	return v.Validate()
}

return nil
```

除以上 5 处，**根包不再改动**；其余全部是新增子包。

---

## 七、PR 切片（建议顺序）

| # | 范围 | 产出 | 验收 |
| --- | --- | --- | --- |
| 1 | `testutil` + `lifecycle` | 测试 DSL、关闭钩子 | 现有测试可用新 DSL 重写 1 个示例 |
| 2 | `validatable` + `validate` + `problem` | 标签校验 + RFC 9457 | 覆盖率 ≥ 90% |
| 3 | `config` | JSON/env/flag + 热更新 | 并发读写 `-race` 通过 |
| 4 | `middlewares/compress` + `middlewares/cache` | 压缩与缓存 | SSE/WS 旁路用例 |
| 5 | `metrics` + `RegisterMetrics` | 环形缓冲、FileStore、Prometheus | 格式逐字对比 |
| 6 | `openapi` + `RegisterOpenAPI` | Spec + 内嵌 UI | 两次序列化字节一致 |
| 7 | `middlewares/trace` + `jwt` 扩展 | traceparent、RS256/ES256 | 非法 traceparent 用例 |
| 8 | `session` + `upload` | 会话、分片上传 | 并发合并与秒传 |
| 9 | WebSocket / SSE 增强 | 分片、压缩、房间、回放 | RFC 6455 用例 + fuzz |
| 10 | `job` / `gateway` / `cmd/leego` | 调度、反代、脚手架 | 示例可跑通 |

每个 PR 必须满足：`gofmt -l .` 为空、`go vet` 无输出、`go test -race` 通过、
新包覆盖率 ≥ 85%、文档同步（`docs/` + `RELEASE.md`）。

---

## 八、测试与验收要点

| 包 | 必测场景 | 特殊要求 |
| --- | --- | --- |
| `config` | 优先级、类型转换、热更新、并发读写 | `-race` |
| `validate` | 全部规则、嵌套、`dive`、nil 指针 | 并发首次解析 |
| `problem` | 状态码映射、已写响应时不覆盖 | JSON 字段稳定性 |
| `openapi` | 模式转换、schema 深度截断、排序稳定 | 快照对比 |
| `metrics` | 环形边界、JSONL 尾部损坏、轮转 | 大文件流式读 |
| `compress` | 已协商/未协商、SSE/WS 旁路、小体不压 | 与 `Flush` 交互 |
| `cache` | 命中/未命中、vary、过期、overflow | 池化归还断言 |
| `trace` | 非法 traceparent、ID 唯一性 | fuzz 解析入口 |
| `session` | 过期、ID 轮换、签名篡改 | Cookie 属性断言 |
| `upload` | 乱序分片、重复分片、中断续传 | 并发合并 |
| WS/SSE | 分片边界、压缩协商、回放 | RFC 用例 + fuzz |
| `job` | 取消、在途等待、队满拒绝 | 时钟可控 |
| `gateway` | 加权分配、流式透传、上游失败 | 超时预算 |

**明确不做**：为任何包引入 `require` 条目；在根包内直接调用外部 SDK；
把 C 档适配器写进 `contrib/` 以外的位置；为可选能力修改既有公开签名。

