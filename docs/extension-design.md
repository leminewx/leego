# 拓展功能实现方式与代码结构（零第三方依赖）

本文是 [extension-roadmap.md](extension-roadmap.md) 的落地篇：把 12 个重点模块
与 4 个候选能力拆成**可独立提交的包、接口签名与实现步骤**，约束是只使用 Go 标准库与
`golang.org/x/{crypto,net,time}`（已在 `go.mod`）。

本文只给结构与签名，不含完整实现；实现时按第五节的切片顺序逐个 PR 落地。

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

## 二、目标目录结构

```
leego/
├── config/                     # 配置加载与热更新（A 档，纯标准库）
│   ├── config.go               # Config：不可变快照 + 绑定 + 变更订阅
│   ├── source.go               # Source 接口 + JSONSource / EnvSource / FlagSource
│   ├── watch.go                # 轮询监听（os.Stat）
│   └── config_test.go
├── validate/                   # 标签校验（A 档）
│   ├── validate.go             # Struct() + Validator
│   ├── rule.go                 # 内置规则表
│   ├── cache.go                # reflect.Type → 字段元信息（sync.Map）
│   └── validate_test.go
├── problem/                    # RFC 9457 错误模型（A 档）
│   ├── problem.go              # Problem / FieldError / 错误码
│   ├── writer.go               # WriteProblem(ctx)
│   └── i18n.go                 # 消息表
├── openapi/                    # OpenAPI 3.1 + 内嵌浏览页（A 档，不导入 leego）
│   ├── spec.go                 # Spec / PathItem / Operation / Schema
│   ├── builder.go              # 路由 → Spec
│   ├── schema.go               # reflect → JSON Schema
│   ├── handler.go              # /openapi.json + UI
│   └── assets/index.html       # go:embed 自写页面
├── metrics/                    # 指标持久化与导出（A 档）
│   ├── series.go               # TimeSeries 环形缓冲
│   ├── store.go                # Store 接口 + MemoryStore / FileStore
│   ├── prometheus.go           # 文本格式编码器
│   └── provider.go             # 实现 dashboard.Provider
├── middleware/
│   ├── compress/               # gzip 压缩中间件（A 档）
│   ├── cache/                  # 响应缓存（A 档，含 Recorder）
│   ├── trace/                  # traceparent 传播（A 档，含 Exporter 接口）
│   └── secure/                 # 请求签名 + 防重放（A 档）
├── session/                    # 会话（A 档，Store 接口）
├── upload/                     # 分片上传（A 档，Storage 接口）
├── job/                        # 进程内调度（A 档）
├── gateway/                    # 反向代理与灰度（A 档）
├── testutil/                   # 测试工具包（A 档）
├── cmd/leego/                  # 脚手架（A 档，flag + embed）
└── contrib/                    # 预留：C 档适配器（独立 go.mod，不入根模块）
    ├── redis/                  # Backend / Broadcaster / Store 实现
    ├── otel/                   # Exporter 实现
    └── s3/                     # Storage 实现

根包仅新增 4 个适配文件（与上表平级，见第六节）：
`validatable.go`、`openapi.go`、`metrics.go`、`lifecycle.go`。
```

---

## 三、包依赖图

```
                    ┌──────────────────────────────┐
                    │  cmd/leego（main，可选）      │
                    └──────────────┬───────────────┘
                                   │
   config  validate  problem  session  upload  job  gateway  testutil
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
                     middleware/{compress,cache,trace,secure}
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
| `config.go` | `Config`、`Load`、`Bind`、`OnChange`、快照替换 |
| `source.go` | `Source` 接口；`JSONSource`（文件）、`EnvSource`、`FlagSource` |
| `watch.go` | `watcher`：轮询 `os.Stat`，变化时重读并比较内容哈希 |

**核心签名**

```go
// Source 定义配置来源。Watch 在数据变化时回调 notify，返回停止函数。
type Source interface {
	Load() (map[string]any, error)
	Watch(notify func(map[string]any)) (stop func(), err error)
}

// Config 是不可变快照的持有者，可被并发读取。
type Config struct {
	values atomic.Pointer[map[string]any] // 只读快照，整体替换
	subs   sync.Map                       // key → []func(any)
}

func Load(sources ...Source) (*Config, error)
func (own *Config) Get(key string) (any, bool)
func (own *Config) GetString(key, fallback string) string
func (own *Config) GetDuration(key string, fallback time.Duration) time.Duration
func (own *Config) Bind(target any) error          // 标签 config:"server.read_timeout"
func (own *Config) OnChange(key string, fn func(any)) (cancel func())
func (own *Config) Snapshot() map[string]any       // 返回浅拷贝，供诊断页展示
```

**实现步骤**

1. 合并顺序：默认值 → 文件 → 环境变量 → 命令行；键名统一转小写并用 `.` 分层。
2. `Bind` 复用根包已有的反射经验：`reflect` 遍历字段，支持 `config:"a.b"`、
   `default:"..."`、`time.Duration`/`int64`/`bool`/`[]string` 的转换。
3. 热更新：`watcher` 每 1s `os.Stat`，`ModTime + Size` 变化时重读，
   算出 `hash/fnv` 摘要与旧值比较；不同则构造新 map 并 `Store`，
   再按 key 差异派发 `OnChange`。
4. 生命周期：`Watch` 的协程由 `stop()` 停止；`stop` 幂等（`sync.Once`）。

**接线点**：`main` 中加载后手动应用启动期参数
（`leego.UseEngineName(cfg.GetString(...))`）；运行期参数通过 `OnChange`
回调写进 `limiter`/`breaker` 的可变配置，不触碰 `EngineConfig`。

**测试点**：优先级合并、类型转换失败、并发 `Get` 与替换（`-race`）、
`OnChange` 只对变化 key 触发、`stop` 幂等。

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
func Struct(target any) error            // 返回 Errors 或 nil
func Var(value any, tags string) error   // 校验单个值
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
func Write(ctx *leego.Context, p *Problem) // Content-Type: application/problem+json
func FromValidationErr(err error) *Problem // validate.Errors → 422
```

**实现步骤**

1. 元信息缓存：`sync.Map[reflect.Type]*structInfo`，`structInfo` 只存
   字段下标、标签解析结果与嵌套路径，避免每请求解析标签。
2. 标签解析：`validate:"required,min=1,max=64,email"`；`param` 取逗号后的内容；
   未知规则在 `Struct` 首次执行时返回错误（不要静默忽略）。
3. 值获取：`reflect.Value` 逐层解引用；指针为 nil 且 `required` 时报错，
   否则跳过（语义与常见校验库一致）。
4. `dive`：切片/数组/map 逐元素递归，路径拼成 `items[0].name`。
5. `problem.Write`：先 `ctx.GetStatusCode() != 0` 判断，已写响应则只记录不覆盖。

**接线点**：根包新增 5 行接口（第六节），`Bind` 成功后自动调用：

```go
// validatable.go（根包）
type Validatable interface{ Validate() error }
```

业务 DTO 实现 `Validate()` 转发到 `validate.Struct(own)`，即可零耦合获得校验。

**测试点**：标签全部规则、嵌套与 `dive` 路径、nil 指针、并发首次解析、
`Problem` JSON 字段顺序与 Content-Type。

---

### 4.3 `openapi`：OpenAPI 3.1 生成

**为什么它不能导入 `leego`**：若 `Engine.RegisterOpenAPI` 存在，则依赖是
`leego → openapi`；若 `openapi` 再导入 `leego` 就成环。因此 `openapi`
只认自己的最小结构。

**核心签名**

```go
type Route struct {
	Method  string
	Pattern string
	Name    string
	Tags    []string
}

type RouteSource interface{ GetRoutes() []Route }

type Spec struct { OpenAPI, Info, Paths, Components }
func NewSpec(source RouteSource, options ...Option) *Spec
func (own *Spec) MarshalJSON() ([]byte, error)       // 稳定排序，便于 diff
func (own *Spec) AddOperation(method, pattern string, op Operation)
func (own *Spec) AddSchema(name string, value any)   // reflect → Schema

func NewHandler(spec *Spec) http.Handler  // GET {prefix}/openapi.json
func NewUIHandler(spec *Spec) http.Handler // 自写 HTML + 内嵌 JS
```

**实现步骤**

1. 路径转换：`/users/:id` → `/users/{id}`；`*path` → `/{path}`（标记为 wildcard）。
2. 每个 `Route` 生成 `PathItem` 的一个方法项，`operationId` 用
   `Name` 或 `method + pattern` 的规范化串。
3. `Schema` 生成：基本类型 → `type/format`；结构体 → `object + properties`；
   切片 → `array + items`；`time.Time` → `string/date-time`；`any` → `{}`；
   深度上限 8 层，超出回退 `{"type":"object"}`。
4. UI：`embed` 一个自写页面（左侧路由树、右侧 schema、顶部搜索），
   纯 `fetch` + 原生 DOM，不引 CDN。
5. 快照语义：`RegisterOpenAPI` 在 `Prepare()` 前构建 `Spec`，
   运行期只读；路由变更需重新构建（注册期语义）。

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
JSON 输出稳定性（两次序列化字节一致）、UI 页面可离线加载。

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

### 4.5 `middleware/compress`：gzip 压缩

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

### 4.6 `middleware/cache`：响应缓存

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

1. 缓存键：`method + ctx.GetURL().Path + query(raw) + vary 头摘要`（SHA-256 前 16 字节）。
2. `Recorder` 实现 `response.Writer`：镜像 body 到 `bytes.Buffer`，
   超过 `MaxBody` 标记 `overflow` 并停止镜像（仍透传）。
3. 命中：直接写 `header + status + body`，设置 `X-Cache: HIT`，**不调用 next**。
4. 未命中：`ctx.Response = recorder`（利用导出字段），`next(ctx)`；
   返回后若状态码 ∈ {200} 且未 `overflow`、未 `Hijack`、非 `text/event-stream`，
   存入 LRU（`container/list` + map，按字节淘汰）。
5. `stale-while-revalidate`：过期条目先返回旧值并设置 `X-Cache: STALE`，
   同时用 `singleflight`（仓库内自研，20 行）触发一次后台刷新。

**关键约束**：`Recorder` 必须在 `next` 返回后立刻 `Flush` 并归还；
不得在缓存命中路径上复用 `Context` 之外的状态。

---

### 4.7 `middleware/trace`：W3C traceparent 传播

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
func From(ctx context.Context) (*Session, bool)
```

**实现步骤**

1. 内存 Store：`map[string]*entry` + `sync.RWMutex`；后台清理协程按 `ttl/2` 扫描。
2. Cookie：`base64url(payload).base64url(hmac)`，HMAC-SHA256；
   校验 `SameSite=Lax`、`HttpOnly`、`Secure`（可配置）。
3. ID 生成：`crypto/rand` 32 字节；**轮换**：登录成功后更换 ID，防会话固定。
4. 请求结束统一 `Save`（`defer`），未修改则不写。

---

### 4.9 `middleware/jwt` 扩展：RS256 / ES256 / JWKS

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

### 4.10 WebSocket 增强

扩展现有 `response/websocket`。

| 能力 | 文件 | 实现方式 |
| --- | --- | --- |
| 分片消息 | `upgrader/conn.go` | 累积 `continuation` 帧到 `bytes.Buffer`，总长受 `MaxReadLimit` 限制；控制帧必须 `FIN=1` 且 ≤125 字节 |
| permessage-deflate | `upgrader/permessage.go` | `compress/flate` 读写器；发送前追加 `0x00 0x00 0xff 0xff`，接收时去除；RSV1 位由连接维护 |
| 房间 | `manager.go` | `rooms map[string]map[string]*Client`（`RWMutex`），广播按房间过滤，保持非阻塞投递 |
| ACK | `message.go` / `client.go` | 新增消息类型 + 每连接重发窗口（`map[seq]frame` + 定时器，上限 N） |
| 背压指标 | `manager.go` | 导出 `len(sendChan)`、丢弃计数到 `collector` |

---

### 4.11 SSE 增强

| 能力 | 文件 | 实现方式 |
| --- | --- | --- |
| `Last-Event-ID` 回放 | `client.go` / `manager.go` | 客户端维护单调 `eventID`；Manager 保留环形缓冲（默认 256 条），`Replay(fromID)` 补发 |
| 跨副本广播 | `broadcaster.go`（新） | `Broadcaster` 接口 + `localBroadcaster`；外部实现由使用方注入 |
| 背压 | `manager.go` | 发送通道满时丢弃最旧事件并计数，日志限流打印 |

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
	PutPart(ctx context.Context, id string, index int, r io.Reader, size int64) error
	Complete(ctx context.Context, id string, parts []Part) (string, error)
	Abort(ctx context.Context, id string) error
}

func NewLocalStorage(root string) Storage
func NewHandler(storage Storage, options ...Option) leego.HandleFunc
```

实现要点：会话元信息以 JSON 文件落在 `root/.sessions/<id>.json`，
并发用 per-session `sync.Mutex`（`sync.Map` 存锁）；`Complete` 按序拼接并
校验 SHA-256（秒传索引为 `sha256 → 路径` 的 JSON 映射）；`Abort` 清理目录。

---

### 4.13 `job`：进程内调度

```go
type Job interface{ Run(ctx context.Context) error }

type Scheduler struct{ /* 最小堆 + 单协程 */ }
func NewScheduler(options ...Option) *Scheduler
func (own *Scheduler) Every(interval time.Duration, job Job) (cancel func())
func (own *Scheduler) After(d time.Duration, job Job)
func (own *Scheduler) Start(ctx context.Context)
func (own *Scheduler) Stop(ctx context.Context) error // 等待在途任务
```

实现要点：`container/heap` 按 `nextRun` 排序；单协程 `select` 定时器 + 取消；
Worker 数有界（默认 `GOMAXPROCS`），队满拒绝并计数；
语义明确为 **at-most-once**（进程崩溃则丢失），写入包注释。

---

### 4.14 `gateway`：反向代理与灰度

```go
type Upstream struct {
	URL    *url.URL
	Weight int
}

func NewBalancer(upstreams ...Upstream) (*Balancer, error) // 加权轮询（平滑加权）
func NewProxy(balancer *Balancer, options ...Option) leego.HandleFunc
```

实现要点：`httputil.NewSingleHostReverseProxy` 每上游一个实例并复用连接；
灰度按 header 命中或权重分流；`FlushInterval = -1` 支持 SSE 流式透传；
`ErrorHandler` 里读 `breaker` 状态做快速失败；超时预算通过
`X-Request-Timeout` 向下游传递。

---

### 4.15 `testutil`：测试工具包

```go
type Client struct{ /* 包装 http.Handler */ }
func NewClient(handler http.Handler) *Client
func (own *Client) Get(path string, options ...RequestOption) *Response
func (own *Client) JSON(method, path string, body any, options ...RequestOption) *Response

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (own *Response) AssertStatus(t *testing.T, want int)
func (own *Response) BindJSON(t *testing.T, target any)
func (own *Response) String() string

type Clock interface{ Now() time.Time }
type FrozenClock struct{ At time.Time }
```

实现要点：全部基于 `net/http/httptest`；`RequestOption`
（`WithHeader`、`WithCookie`、`WithQuery`）为函数式选项；
`FrozenClock` 用于稳定时间断言（不改根包时钟）。

---

### 4.16 `cmd/leego`：脚手架

```go
// 只用 flag.NewFlagSet 实现子命令：new / gen handler / routes
go:embed templates/**   // 工程骨架模板
```

实现要点：`leego new` 生成目录 + 配置模板 + Dockerfile；
`leego gen handler` 生成 handler/测试/注册片段；
`leego routes` 通过替换 `main` 中的空壳 `Engine` 调用 `GetRoutes()` 输出表格。

---

## 五、六个关键实现难点

### 5.1 配置热更新：不可变快照 + 差异派发

```go
func (own *Config) apply(next map[string]any) {
	previous := own.values.Swap(&next)
	if previous == nil { return }

	// 只对发生变化的 key 派发订阅，避免“任何字段变更触发全部回调”。
	for key, fn := range own.subsAll() {
		if v, ok := next[key]; ok && !equalValue(v, (*previous)[key]) {
			fn(v)
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

### 5.4 WebSocket 分片与 permessage-deflate 状态机

每个连接维护三个状态位：

| 状态 | 含义 | 非法转移 |
| --- | --- | --- |
| `inFragment` | 正在累积分片 | 收到新的非 continuation 数据帧 → 1002 |
| `deflateOut` | 出方向启用压缩 | 未协商却收到 RSV1 → 1002 |
| `deflateIn` | 入方向启用压缩 | RSV1 置位但 `inFragment` 为真 → 1002 |

解压尾部处理：发送时 `flate` 后追加 `0x00 0x00 0xff 0xff`；
接收时先去掉这 4 字节再 `flate.NewReader`。控制帧（Ping/Pong/Close）
永不参与压缩，`RSV1` 必须为 0。

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
| 4 | `middleware/compress` + `middleware/cache` | 压缩与缓存 | SSE/WS 旁路用例 |
| 5 | `metrics` + `RegisterMetrics` | 环形缓冲、FileStore、Prometheus | 格式逐字对比 |
| 6 | `openapi` + `RegisterOpenAPI` | Spec + 内嵌 UI | 两次序列化字节一致 |
| 7 | `middleware/trace` + `jwt` 扩展 | traceparent、RS256/ES256 | 非法 traceparent 用例 |
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

