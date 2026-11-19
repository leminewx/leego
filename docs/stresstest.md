# 压力测试工具

`stresstest` 目录下是包名为 `stress` 的轻量级 HTTP 压测工具。它只依赖标准库，
适合在本地或 CI 中快速验证服务的吞吐与延迟分布。

```go
import stress "gitee.com/leminewx/leego/stresstest"
```

> 目录名是 `stresstest`，包名是 `stress`。导入时建议显式起别名，
> 避免阅读代码的人误以为包名与目录名一致。

---

## 一、设计目标与取舍

| 目标 | 做法 | 代价 |
| --- | --- | --- |
| 延迟统计不失真 | 每个 worker **串行**发请求，并发度由 worker 数量控制 | 并发度不能动态提升 |
| 压测客户端不成为瓶颈 | 所有 worker 共用一个 `http.Transport` 连接池 | 不同场景需要分别创建 `Client` |
| 指标快照一致 | 每次 `Run` 结束后一次性合并、加锁生成快照 | 无法在压测中途读取精确的实时指标 |
| 零依赖 | 只用标准库 | 没有分布式压测、没有动态脚本 |

**它不是**：分布式压测平台、协议级（非 HTTP）压测工具、持续负载发生器。
需要长时间稳压或复杂场景编排时，请使用专业工具。

---

## 二、配置

### 2.1 字段

```go
type Config struct {
	Method             string
	URL                string
	Headers            map[string]string
	Body               string
	Concurrent         int
	TotalRequests      int
	Timeout            time.Duration
	KeepAlive          bool
	InsecureSkipVerify bool
}
```

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `Method` | `GET` | 自动转大写并去空格；空字符串回落为 `GET` |
| `URL` | 无 | 必须是包含协议与主机的绝对地址，支持 `http`/`https` |
| `Headers` | 空 map | 每次请求都会重新设置 |
| `Body` | 空 | 非空时所有请求复用同一份内容（每次新建 `strings.Reader`，可重复发送） |
| `Concurrent` | `1` | worker 数量；`< 1` 取 1 |
| `TotalRequests` | `100` | 总请求数；`< 1` 取 100 |
| `Timeout` | `30s` | 单请求超时；`<= 0` 取 30 秒 |
| `KeepAlive` | `false` | 是否复用 TCP 连接；默认关闭以便观察建连开销 |
| `InsecureSkipVerify` | `false` | 跳过 TLS 证书校验，仅用于内网自签证书 |

> `KeepAlive = false` 对应 `Transport.DisableKeepAlives = true`。
> 关注**框架处理能力**时应开启；关注**端到端真实体验**（含 TLS 握手）时可关闭。

### 2.2 创建与校验

```go
config := stress.NewDefaultConfig("http://127.0.0.1:8080/examples")
config.Concurrent = 50
config.TotalRequests = 5000
config.KeepAlive = true

client, err := stress.NewClient(config)
if err != nil {
	log.Fatal(err)
}
defer client.Close()
```

`NewClient` 会先 `Clone` 配置（深拷贝 `Headers`）再规范化，因此：

- 创建后修改原 `config` 不会影响运行中的客户端；
- 可修复的问题（负数并发、零超时、缺少方法）被就地纠正；
- 不可修复的问题通过 `error` 返回。

**不可修复的错误**：

| 错误信息 | 触发条件 |
| --- | --- |
| `stress: nil config` | 传入 `nil` |
| `stress: empty target url` | `URL` 为空或只有空白 |
| `stress: invalid target url: ...` | `url.Parse` 失败 |
| `stress: unsupported scheme "..." ...` | 协议不是 `http`/`https` |
| `stress: target url must contain host` | 缺少主机 |

### 2.3 连接池

`NewClient` 内部按配置构造 `http.Transport`：

| 参数 | 取值 |
| --- | --- |
| `MaxIdleConns` | `Concurrent` |
| `MaxIdleConnsPerHost` | `Concurrent` |
| `IdleConnTimeout` | `90s` |
| `DisableKeepAlives` | `!KeepAlive` |
| `TLSClientConfig.MinVersion` | `tls.VersionTLS12` |

需要注入自定义 `RoundTripper`、代理或 HTTP/2 时：

```go
client.SetTransport(customTransport) // 传 nil 会被忽略
httpClient := client.GetHTTPClient()
```

`SetTransport(nil)` 是空操作，便于链式调用传可选值。

### 2.4 读取配置

```go
copied := client.GetConfig() // 返回 Config 值拷贝，Headers 亦为独立 map
```

---

## 三、执行模型

### 3.1 `Run` 的分配策略

```go
snapshot := client.Run()
```

1. `requestsPerWorker = TotalRequests / Concurrent`，`remainder = TotalRequests % Concurrent`；
2. 前 `remainder` 个 worker 各多执行 1 个请求，保证实际请求数**恰好**等于 `TotalRequests`；
3. 分配为 0 的 worker 不启动协程，但仍初始化空采集器，保证下标对齐；
4. `sync.WaitGroup.Go` 启动全部 worker，`Wait` 阻塞到全部结束；
5. 依次 `merge` 每个 worker 的结果，`Finish` 记录结束时间，返回快照。

> `Run` 是**同步阻塞**的。需要并行跑多组场景请自行启动协程，但同一 `Client` 上的并发 `Run`
> 会共享连接池与 `http.Client`，指标不会串（每次 `Run` 独立创建 `Metrics`），
> 但连接竞争会影响延迟分布，建议一组场景一个 `Client`。

### 3.2 单个 worker

```go
for index := 0; index < count; index++ {
	start := time.Now()
	request, err := own.newRequest()
	// ...
	response, err := own.client.Do(request)
	latency := time.Since(start)
	// 读完并关闭响应体
	result.record(statusCode, latency, err)
}
```

要点：

- **必须读完响应体再关闭**：否则连接无法复用，`KeepAlive = true` 也会退化。
- **延迟包含读体时间**：`latency` 在 `Do` 返回后立即计算，但读体使用了独立的 `io.Copy`，
  因此延迟实际为“建连 + 首字节 + 收到响应头”，不含读体时间。大响应体场景请以 QPS 为主要指标。
- **请求级失败不影响后续请求**：构造请求失败或网络错误只记录一次结果，循环继续。
- **请求体每次重建**：`strings.NewReader` 保证每个请求都有独立的 `Body`。

### 3.3 成功与失败的判定

| 条件 | 归类 |
| --- | --- |
| `err == nil && statusCode > 0 && statusCode < 400` | 成功 |
| 其余情况（`4xx`/`5xx`/无响应） | 失败 |
| 失败且 `err != nil`（网络错误） | 同时计入 `ErrorCount` |

`StatusCodes` 中，网络错误记为状态码 **0**。

> 3xx 被计为成功。客户端未设置 `CheckRedirect`，因此沿用 `http.Client` 的默认行为——
> 最多跟随 10 次重定向；超过上限时 `Do` 返回错误，该次请求计入网络错误。

---

## 四、指标与报告

### 4.1 `MetricsSnapshot` 字段

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `StartTime` / `EndTime` | `time.Time` | 压测起止时间 |
| `Duration` | `time.Duration` | 总耗时 |
| `TotalRequests` | `int64` | 实际发起的请求数 |
| `SuccessCount` | `int64` | 成功请求数 |
| `FailedCount` | `int64` | 失败请求数（含状态码失败与网络错误） |
| `ErrorCount` | `int64` | 网络错误数 |
| `SuccessRate` | `float64` | `SuccessCount / TotalRequests`，`[0,1]` |
| `QPS` | `float64` | **成功**请求数 / 耗时秒数 |
| `MinLatency` / `MaxLatency` / `AvgLatency` | `time.Duration` | 最小 / 最大 / 平均延迟 |
| `P50Latency` / `P90Latency` / `P95Latency` / `P99Latency` | `time.Duration` | 分位延迟 |
| `StatusCodes` | `map[int]int64` | 状态码分布，网络错误为 0 |

**QPS 只统计成功请求**。若需要“总吞吐”，可用 `TotalRequests / Duration.Seconds()` 自行计算。
失败率很高时 QPS 会显著低于实际发送速率，这是刻意的——避免用失败请求堆出好看的数字。

### 4.2 分位数算法

采用**最近秩法（nearest-rank）**：

```
index = ceil(p / 100 * n) - 1
```

- `p` 被裁剪到 `(0, 100]`；
- `n == 0` 时返回 0；
- 样本在首次计算时排序并缓存（`sorted` 标记），重复调用不会重复排序。

最近秩法返回的是**真实存在的样本**，不会像线性插值那样产生样本之间的虚拟值，
适合延迟这类离散分布；代价是样本较少时分位数会跳跃。

### 4.3 报告文本

```go
fmt.Println(stress.FormatReport(snapshot))
// 等价于
fmt.Println(client.Run().GetReport())
```

输出结构：

```
================================= 性能测试报告 =================================
开始时间: 2026-11-19 10:00:00
结束时间: 2026-11-19 10:00:05
测试耗时: 5.0012s
总请求数: 5000
成功请求: 4990
失败请求: 10
网络错误: 0
成功率: 99.80%
QPS: 997.76
最小延迟: 1.2ms
最大延迟: 42.1ms
平均延迟: 2.8ms
P50 延迟: 2.7ms
P90 延迟: 3.9ms
P95 延迟: 4.4ms
P99 延迟: 6.1ms
状态码分布:
  200: 4990 (99.8%)
  500: 10 (0.2%)
```

状态码按**数值升序**输出，占比以 `TotalRequests` 为分母，保留 1 位小数。

### 4.4 手动采集

不使用 `Client` 时，也可以直接使用指标收集器：

```go
metrics := stress.NewMetrics()

start := time.Now()
resp, err := http.Get("http://127.0.0.1:8080/ping")
if resp != nil {
	_ = resp.Body.Close()
}
metrics.Record(resp.StatusCode, time.Since(start), err)

metrics.Finish()
fmt.Println(metrics.GetReport())
```

- `Record(statusCode, latency, err)` 并发安全（内部互斥锁）；
- `Finish()` 只有第一次生效；
- `GetSnapshot()` 在未 `Finish` 时用“当前时间”计算 `EndTime` 与 `Duration`，便于实时观测。

### 4.5 延迟采集器

`stress.Latencies` 是**非并发安全**的延迟样本容器，供每个 worker 独占使用：

| 方法 | 说明 |
| --- | --- |
| `NewLatencies()` / `NewLatenciesWithCapacity(n)` | 创建空采集器，可预分配容量 |
| `Append(d)` / `Merge(other)` | 追加样本 / 合并另一个采集器（`other` 可为 nil） |
| `GetLatencies()` / `GetLen()` | 获取副本 / 样本数 |
| `CountTotal()` / `GetAverage()` / `GetMin()` / `GetMax()` | 统计值 |
| `GetPercentile(p)` / `Sort()` | 分位数 / 显式排序 |
| `Reset()` | 清空以便复用 |

---

## 五、配套基准服务

`stresstest/test/base` 是一个可直接运行的基准服务：

```bash
go run ./stresstest/test/base
```

| 端点 | 用途 |
| --- | --- |
| `GET /examples` | 返回固定 JSON，测量框架自身吞吐上限 |
| `GET /metadata` | 返回全部路由指标，观察采集开销 |
| `GET /files/*` | 静态文件服务，测量文件传输性能 |

它默认监听 `:8080`，并调用 `WaitSignalAndShutdown(5s, nil)` 支持优雅关闭。

---

## 六、使用建议

1. **先小后大**：先用 `Concurrent = 1`、`TotalRequests = 100` 验证正确性，再逐步加压。
2. **区分瓶颈**：客户端与服务端不要跑在同一台机器的同一核上；必要时用 `GOMAXPROCS` 限制服务端。
3. **观察状态码**：先看 `StatusCodes` 与 `ErrorCount`，再看延迟——大量失败时的低延迟没有意义。
4. **关注 P99**：平均值会被大量快请求拉低，P99 更能反映长尾。
5. **开启指标时注意开销**：`middleware.Collect()` 为每个请求记录指标，压测框架上限时应关闭它，
   单独测一次“开启采集”的损耗。
6. **复用 Client**：多次 `Run` 复用同一 `Client` 可避免重复建连；不同场景分别创建。
7. **结束后 `Close()`**：释放空闲连接，避免 `TIME_WAIT` 堆积；重复调用安全。

---

## 七、常见问题

**Q：为什么 `TotalRequests` 与实际发出数不一致？**
A：不会不一致。余数会分配给前若干个 worker，`TotalRequests` 精确命中。
若中途触发了 `Timeout`，请求仍会计入 `TotalRequests` 与失败数。

**Q：为什么 `QPS` 比预期低很多？**
A：先检查 `SuccessRate`。QPS 只统计成功请求；若大量请求返回 5xx，QPS 会按比例下降。

**Q：`KeepAlive = true` 后 QPS 没有提升？**
A：确认响应体被读完。`Run` 内部使用 `io.Copy(io.Discard, body)` 已正确处理；
手动采集时需要自己读完再关闭。

**Q：能压测 HTTPS 自签证书服务吗？**
A：可以，设置 `config.InsecureSkipVerify = true`。该选项只建议在内网与测试环境使用。

**Q：可以自定义 DNS、代理或 HTTP/2 吗？**
A：通过 `SetTransport` 注入自定义 `*http.Transport` 即可；也可以 `GetHTTPClient` 后替换
`Transport`。注意 `SetTransport` 只接受 `*http.Transport`，需要其它 `RoundTripper` 时直接修改
`GetHTTPClient().Transport`。
