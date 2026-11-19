# 限流与熔断

`middlewares/limiter` 提供三种服务端限流算法，`middlewares/breaker` 提供基于失败率的滑动窗口熔断器。
两者都实现为普通中间件，可与 `Engine.Use`、`Group.Use`、`Handler.Use` 自由组合。

- 包路径：`gitee.com/leminewx/leego/middlewares/limiter`、`gitee.com/leminewx/leego/middlewares/breaker`
- 共同约定：创建后即生效、配置支持热更新、拒绝请求时不会进入后续处理链

---

## 一、限流器

### 1.1 统一接口

三种限流器都实现 `limiter.Limiter`：

```go
type Limiter interface {
	Allow() bool
	AllowWithTimeout(ctx context.Context, timeout time.Duration) bool
	Wait(ctx context.Context) error
	AdaptToMiddleware() leego.MiddlewareFunc
	Enable()
	Disable()
	IsEnabled() bool
}
```

| 方法 | 阻塞 | 消耗配额 | 说明 |
| --- | --- | --- | --- |
| `Allow()` | 否 | 放行时消耗 | 单纯的“试一次”，适合网关或非 HTTP 场景 |
| `AllowWithTimeout(ctx, timeout)` | 最多阻塞 `timeout` | 放行时消耗 | `timeout <= 0` 时退化为 `Allow()` |
| `Wait(ctx)` | 是 | 放行时消耗 | 直到放行、上下文取消或返回错误 |
| `AdaptToMiddleware()` | 取决于 `Config.WaitTimeout` | 放行时消耗 | 中间件形态，见下节 |
| `Enable()` / `Disable()` | 否 | — | 运行期开关，`Disable` 后中间件直接放行 |
| `IsEnabled()` | 否 | — | 读取当前开关 |

**公共错误**：

```go
var ErrNoDeadline = errors.New("limiter: context has no deadline")
```

固定窗口与滑动窗口的 `Wait` 在“首次尝试失败且上下文没有截止时间”时直接返回 `ErrNoDeadline`——
没有截止时间的等待可能永久阻塞，因此在入口处拒绝，而不是静默挂起。

> 令牌桶的 `Wait` 直接委托给 `golang.org/x/time/rate`，不做截止时间检查。
> 传入 `context.Background()` 且限额很低时，它会一直阻塞到拿到令牌。生产代码请始终传带超时的上下文。

### 1.2 `AdaptToMiddleware` 的等待语义

等待上限不再由方法参数传入，而是构造时写入的 `limiter.Config.WaitTimeout`
（**每个请求**的最大等待时长）：

| `WaitTimeout` | 行为 |
| --- | --- |
| `0`（默认） | 不等待，配额不足立即返回 `429`（推荐用于对外接口） |
| 正数 | 在超时内轮询等待，期间请求被挂起 |
| 负数 | 归一化为 `0` |

等待使用请求自身的上下文派生超时上下文：客户端断开或超时后立刻停止等待。

拒绝时统一走 `writeTooManyRequests`：

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 1
Content-Type: application/json; charset=utf-8

{"code":429,"message":"Too Many Requests"}
```

`Retry-After` 固定为 1 秒，表示“建议 1 秒后再试”；它不是精确的窗口剩余时间。

### 1.3 等待配额的实现细节

```go
// waitForQuota 先尝试一次，失败后按 50ms 轮询。
func waitForQuota(ctx context.Context, allow func() bool) error
```

- **先尝试再轮询**：绝大多数请求在第一次检查时就能通过，只有真正触发限流的请求才会创建定时器。
- **轮询间隔 50ms**：兼顾响应速度与 CPU 开销；对高 QPS 场景，最坏情况是配额释放后多等 50ms。
- **不会排队**：轮询不保证公平，多个等待者之间存在竞争，先醒来的先拿到配额。

### 1.4 固定窗口限流

```go
limiter := limiter.NewFixedWindowLimiter(100, time.Second)
engine.Use(limiter.AdaptToMiddleware())
```

**参数**：

| 参数 | 说明 |
| --- | --- |
| `limit` | 窗口内允许的最大请求数；`<= 0` 表示不限流（始终放行） |
| `windowSize` | 窗口长度；`<= 0` 时使用默认值 `1s` |

**运行时配置**：

| 方法 | 行为 |
| --- | --- |
| `SetLimit(limit)` | 原子更新限额，立即生效；非正数表示不限流 |
| `SetWindowSize(size)` | 更新窗口长度并**重置**当前窗口计数 |
| `GetLimit()` / `GetWindowSize()` / `GetConfig()` | 读取配置 |

**为什么改窗口要重置计数**：按比例折算历史计数会让“限额”与“实际放行量”的对应关系变得不可预测；
重置后的语义清晰——改完配置立刻从新窗口重新计数。

**已知取舍：窗口边界双倍突发**。若限额为 N，在相邻窗口交界处的极短时间内可能通过最多 2N 个请求。
需要平滑边界时改用滑动窗口或令牌桶。

**并发实现**：计数与窗口起点由同一把 `sync.Mutex` 保护。不能拆成“原子快路径 + 锁慢路径”——
两者互不互斥会出现计数丢失，实际放行量可能超过限额。

### 1.5 滑动窗口限流

```go
limiter := limiter.NewSlidingWindowLimiter(100, 10, time.Second)
engine.Use(limiter.AdaptToMiddleware())
```

**参数**：

| 参数 | 说明 |
| --- | --- |
| `limit` | 窗口内允许的最大请求数；`<= 0` 表示不限流 |
| `bucketCount` | 桶数量；`< 1` 时使用默认值 `10` |
| `windowSize` | 窗口长度；`< 1s` 时使用默认值 `1s` |

桶越多，窗口边界越平滑，但单次判断的计算量也越大（每次请求最多推进 `bucketCount` 个桶）。

**运行时配置**：

| 方法 | 行为 |
| --- | --- |
| `SetLimit(limit)` | 原子更新限额 |
| `SetBucketCount(count)` | 重建桶数组并重置索引、累计计数 |
| `SetWindowSize(size)` | 更新窗口长度并清空全部桶 |
| `GetLimit()` / `GetBucketCount()` / `GetWindowSize()` / `GetCurrentCount()` / `GetConfig()` | 读取配置与当前窗口计数 |

**推进算法**：

1. 计算 `steps = elapsed / bucketSize`；
2. `steps <= 0` 直接返回（同一时间片内不做任何工作）；
3. `steps >= 桶数量` 说明已跨过整个窗口，整体清零；
4. 否则逐个推进环形下标，减去被覆盖桶的计数并清零；
5. 兜底把负的 `total` 修正为 0（浮点/时钟回拨的防御）。

`total` 是缓存值，必须与桶数组同锁更新，否则会与桶内实际计数不一致。

> 注意：窗口长度最小值被硬性限制为 1 秒，`NewSlidingWindowLimiter(10, 10, 500*time.Millisecond)` 实际窗口仍是 1 秒。

### 1.6 令牌桶限流

```go
limiter := limiter.NewTokenBucketLimiter(100, 20)
engine.Use(limiter.AdaptToMiddleware())
```

**参数**：

| 参数 | 说明 |
| --- | --- |
| `limit` | 每秒补充的令牌数；`<= 0` 表示不限流 |
| `burst` | 桶容量，即允许的最大突发量；`< 1` 时按 `1` 处理 |

底层使用 `golang.org/x/time/rate`，它本身并发安全；本类型额外维护 `limit`/`burst` 的原子副本，
用于无锁读取配置。

**`limit <= 0` 的语义**：映射为 `rate.Inf`，表示“不限流”，而不是“一个令牌也不发”——
避免调用方误传 0 导致服务整体不可用。

**运行时配置**：

| 方法 | 行为 |
| --- | --- |
| `SetLimit(limit)` | 更新补充速率；非正数切到 `rate.Inf` |
| `SetBurst(burst)` | 更新桶容量；`< 1` 按 1 处理 |
| `GetLimit()` / `GetBurst()` / `GetConfig()` | 读取配置 |

**与窗口算法的差异**：

| 维度 | 固定窗口 | 滑动窗口 | 令牌桶 |
| --- | --- | --- | --- |
| 边界突发 | 最多 2N | 被桶数量摊平 | 允许 `burst` 内的突发 |
| 长期速率 | 窗口内平均 | 近似平均 | 严格按 `limit` 平滑 |
| 单次开销 | 1 次加锁 | 1 次加锁 + 桶推进 | 1 次原子/内部锁 |
| 适用场景 | 简单配额 | 需要平滑的入口限流 | 调用下游 API 的速率控制 |

### 1.7 选型建议

- **对外 API 的粗粒度配额**：固定窗口，`WaitTimeout = 0`，快速失败。
- **需要避免边界突刺**：滑动窗口，桶数量取 `10`～`20`。
- **限速调用第三方服务**：令牌桶，`limit` 等于对方的 QPS 配额，`burst` 取 `limit` 的 10%～50%。
- **多个限流器叠加**：例如“每 IP 固定窗口 + 全局令牌桶”，注意顺序——外层先拦掉明显超限的请求。

### 1.8 组合示例

```go
package main

import (
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middlewares/limiter"
)

func main() {
	engine := leego.NewEngine()

	global := limiter.NewTokenBucketLimiter(2000, 200)
	perRoute := limiter.NewSlidingWindowLimiter(50, 10, time.Second, limiter.Config{WaitTimeout: 100 * time.Millisecond})

	engine.Use(global.AdaptToMiddleware())

	group := engine.NewGroup("/api", perRoute.AdaptToMiddleware())
	group.GET("/ping", func(ctx *leego.Context) {
		ctx.ResponseJSON(200, map[string]string{"message": "pong"})
	})

	engine.Run(":8080")
}
```

---

## 二、熔断器

### 2.1 为什么用失败率

与“连续失败次数”模型相比，失败率模型更贴近真实故障：

- 高流量接口偶发失败不会误触发；
- 低流量接口不会因为一次失败就长期熔断；
- 通过 `MinimumRequests` 保证样本量足够才评估。

### 2.2 配置

```go
type Config struct {
	IsEnabled                bool
	WindowSize               time.Duration
	MinimumRequests          int64
	FailureRate              float64
	OpenTimeout              time.Duration
	HalfOpenMaxRequests      int64
	HalfOpenSuccessThreshold int64
}
```

**默认值**（`breaker.NewDefaultConfig()`）：

| 字段 | 默认值 | 含义 |
| --- | --- | --- |
| `IsEnabled` | `true` | 创建后立即生效 |
| `WindowSize` | `10s` | 失败率统计窗口 |
| `MinimumRequests` | `10` | 触发评估所需的最小样本数 |
| `FailureRate` | `0.5` | 失败率达到 50% 触发熔断 |
| `OpenTimeout` | `30s` | 打开状态持续时长 |
| `HalfOpenMaxRequests` | `1` | 半开状态允许的并发探测数 |
| `HalfOpenSuccessThreshold` | `3` | 连续成功 3 次后恢复闭合 |

**`Config.Normalize()` 修正规则**：

| 字段 | 非法值 | 修正为 |
| --- | --- | --- |
| `WindowSize` | `<= 0` | `10s` |
| `MinimumRequests` | `< 1` | `1` |
| `FailureRate` | 不在 `(0, 1]` | `0.5` |
| `OpenTimeout` | `<= 0` | `30s` |
| `HalfOpenMaxRequests` | `< 1` | `1` |
| `HalfOpenSuccessThreshold` | `< 1` | `1` |

> `Normalize` **不会**修改 `IsEnabled`。直接传 `breaker.Config{}` 得到的是**禁用**状态；
> 需要默认启用请使用 `NewDefaultConfig()`。

### 2.3 创建与状态

```go
circuitBreaker := breaker.NewCircuitBreaker(breaker.NewDefaultConfig())
engine.Use(circuitBreaker.AdaptToMiddleware())
```

三种状态以字符串常量表示，便于日志与 JSON 输出：

| 常量 | 值 | 语义 |
| --- | --- | --- |
| `STATE_CLOSED` | `CircuitBreaker.Closed` | 正常放行并统计 |
| `STATE_OPEN` | `CircuitBreaker.Open` | 直接拒绝，直到 `OpenTimeout` 到期 |
| `STATE_HALF_OPEN` | `CircuitBreaker.HalfOpen` | 放行少量探测请求验证下游 |

状态迁移：

```
Closed   --(窗口样本量 >= MinimumRequests 且失败率 >= FailureRate)--> Open
Open     --(等待 OpenTimeout)-->                                      HalfOpen
HalfOpen --(连续成功 >= HalfOpenSuccessThreshold)-->                  Closed
HalfOpen --(任意一次失败)-->                                          Open
```

补充细节：

- 打开状态到期后，**第一个到达的请求**触发 `Open → HalfOpen` 迁移并成为探测请求，不是后台定时器。
- 半开阶段探测数达到 `HalfOpenMaxRequests` 后，其余请求一律按“打开”处理（计为 blocked）。
- 每次进入半开、以及恢复闭合时都会**清空滑动窗口**，避免历史失败率影响新一轮评估。
- `stateChanges` 只在真正发生状态迁移时递增。

### 2.4 中间件行为

```go
func (own *CircuitBreaker) AdaptToMiddleware() leego.MiddlewareFunc
```

| 情况 | 行为 |
| --- | --- |
| 熔断器未启用 | 直接 `next(ctx)`，不统计 |
| 打开 / 半开探测已满 | 写 `503 Service Unavailable` 与固定提示语，不执行 `next` |
| 其余情况 | 执行 `next(ctx)`，返回后按状态码记录成功/失败 |

拒绝时的响应体：

```json
{"code":503,"message":"service is temporarily unavailable and please try again later"}
```

**成功判定**：默认 `err == nil && status < 500`，可通过 `SetSuccessChecker` 覆盖：

```go
circuitBreaker.SetSuccessChecker(func(status int, err error) bool {
	// 例如把 429 也视为“下游可用，只是被限流”
	return err == nil && status < 500 && status != http.StatusTooManyRequests
})
```

传入 `nil` 恢复默认策略。

> **panic 会计为失败后继续上抛**：中间件用 `defer` 捕获 panic，先以 500 记录失败、
> 归还半开探测额度，再把 panic 重新抛出。因此恢复中间件仍应挂在熔断器的**外层**，
> 由它负责向客户端返回 500；熔断器只保证不会因 panic 泄漏探测额度而导致永久 503。

### 2.5 非 HTTP 场景

```go
err := circuitBreaker.Execute(func() error {
	return callDownstream()
})

err = circuitBreaker.ExecuteWithContext(ctx, func(ctx *leego.Context) error {
	return callDownstreamWithContext(ctx)
})
```

| 方法 | 状态码来源 | 备注 |
| --- | --- | --- |
| `Execute(call)` | `err == nil` → 200，否则 500 | `call == nil` 返回错误（不进入统计） |
| `ExecuteWithContext(ctx, call)` | `ctx.GetStatusCode() != 0` 时用状态码；否则 `err != nil` → 500 | `ctx` 允许为 nil |

被拒绝时返回哨兵错误：

```go
if errors.Is(err, breaker.ErrCircuitBreakerIsOpen) {
	// 熔断中，未执行 call
}
```

### 2.6 运行指标

```go
snapshot := circuitBreaker.GetMetricsSnapshot()
```

| 字段 | JSON 键 | 含义 |
| --- | --- | --- |
| `TotalRequests` | `total_requests` | 累计进入熔断判断的请求数（**包含被拒绝的**） |
| `FailedRequests` | `failed_requests` | 累计被判定为失败的请求数 |
| `BlockedRequests` | `blocked_requests` | 累计被熔断拒绝、未真正执行的请求数 |
| `StateChanges` | `state_changes` | 累计状态切换次数 |
| `CurrentState` | `current_state` | 快照时刻的状态 |
| `WindowSuccesses` | `window_successes` | 当前滑动窗口内成功数 |
| `WindowFailures` | `window_failures` | 当前滑动窗口内失败数 |
| `WindowFailureRate` | `window_failure_rate` | 当前窗口失败率，`[0,1]`；无样本时为 0 |
| `SnapshotTime` | `snapshot_time` | 快照生成时间 |

**一致性说明**：累计计数器是原子读，状态与窗口统计需要短暂持锁，因此快照是“近似同一时刻”的视图。
锁内只做整数累加、不调用外部代码，不会造成锁升级或重入。

`ResetMetrics()` 只清零累计计数器，**不改变当前状态**——避免运维误操作导致保护失效。

### 2.7 并发与实现要点

| 关注点 | 实现 |
| --- | --- |
| 状态字段 | 单一 `sync.Mutex` 保护 |
| 累计计数 | `atomic.Int64`，避免与状态锁竞争 |
| 滑动窗口 | 10 个环形桶，`bucketSize = WindowSize / 10` |
| 启用开关 | `atomic.Bool` |
| 成功判定函数 | 由状态锁保护，可在运行期替换 |

滑动窗口推进与限流器一致：`steps >= bucketCount` 时整体清零，否则逐桶推进并清零。

### 2.8 常见问题

**Q：为什么请求一直被 503，但 `FailureRate` 看起来没到阈值？**
A：`MinimumRequests` 只在闭合状态评估时生效。半开状态任意一次失败会**立即**重新打开，
不等待样本量，这是有意的保守策略。

**Q：调用 `Disable()` 后还需要手动关闭状态吗？**
A：不需要。禁用后中间件直接放行，但状态与指标保留；重新 `Enable()` 后从原状态继续。

**Q：能把熔断器挂在路由级而不是全局吗？**
A：可以。每个下游依赖建议使用独立熔断器，挂在对应的 `Group` 或 `Handler` 上，
避免不同依赖的失败互相牵连。

**Q：需要预热吗？**
A：不需要。空窗口 + 样本不足时不会触发熔断，只统计不决策。
