# 性能测试报告

本报告记录 Leego 在固定环境下的三层性能测试：**引擎层微基准**、**中间件与组件层微基准**、
**真实监听下的端到端压测**。所有基准位于 `./harness/bench` 包（包名 `bench`），只依赖标准库。

结论先行：

- 静态路由查找 **154 ns/op**，1000 条路由的查找表仍是 **188 ns/op**——查找成本只与路径深度相关，与路由总数无关。
- 中间件链在 `Prepare` 阶段预编译为闭包，**0、1、4 层中间件的耗时都在 151 ns 附近**，运行期没有链式遍历与额外分配。
- 请求上下文与响应写入器均从池中复用，一次完整请求的可观测分配只有 **2 次 / 24 B**。
- 内置路由级指标采集 **36 ns/op、0 分配**，可以常开。
- 主要成本集中在 JSON 编码与请求体读取：**小 JSON 635 ns/op / 7 次分配**，**6.6 KB JSON（120 条结构体）12.5 µs/op**（复用缓冲区，仅 3 次分配），**读 8 KB 请求体 17.6 µs/op**。
- 端到端 32 并发下，裸 JSON 接口中位吞吐 **9.3 万 QPS**，4 层中间件栈 **7.3 万 QPS**，约 5 KB 文本 + gzip **12.8 万 QPS**。

> 端到端 QPS 是「客户端与服务器同进程共享 CPU」条件下的参考值，不是服务端上限；
> 延迟分位受 Windows 时钟粒度限制，详见[第七节](#七已知局限)。

---

## 一、测试环境

| 项目 | 取值 |
| --- | --- |
| CPU | Intel Core i5-14600KF（14 核 20 线程，基准频率 3.5 GHz） |
| 内存 | 16 GB |
| 操作系统 | Windows 11 家庭中文版（build 26200） |
| Go 版本 | go1.27.4 windows/amd64 |
| GOMAXPROCS | 20（基准名后缀 `-20` 即由此而来） |
| 网络 | 本机回环（127.0.0.1），无 TLS |
| 时钟粒度 | 单调时钟步进 512.9 µs（实测 20 万次 `time.Now()` 只有 2 个不同值） |

微基准与端到端压测在同一台机器上依次运行，期间没有并行执行其他编译或测试任务。
机器仍有 IDE 等后台进程，绝对数值会随环境变化，横向比较应以同一次运行内的相对关系为准。

---

## 二、测试方法

### 2.1 三层基准的分工

| 层次 | 位置 | 衡量什么 | 为什么不混在一起 |
| --- | --- | --- | --- |
| 引擎层 | `bench/engine_bench_test.go` | 路由匹配、中间件编排、响应写出、请求读取、并行扩展 | 直接调用 `ServeHTTP`，绕开 socket，暴露纯框架开销 |
| 组件层 | `bench/middleware_bench_test.go`、`bench/component_bench_test.go` | 单个中间件的边际成本、采集/校验/配置/指标/代理等组件 | 一次只测一个组件，便于定位热点 |
| 端到端 | `bench/endtoend_test.go` | 真实 TCP 监听下的吞吐与延迟分布 | 含内核协议栈与客户端成本，只用于容量参考 |

### 2.2 口径与约束

- 微基准统一使用 `b.ReportAllocs()`，报告 `ns/op`、`B/op`、`allocs/op`；
- 响应写入器使用 `bench` 包内的 `discardWriter` 而非 `httptest.ResponseRecorder`，
  避免把测试工具自身的缓冲与分配算进框架成本；
- **引擎内置的路由级指标采集恒开**（`serveMatched` 中的 `Increase` / `Decrease`，没有导出开关），
  因此所有引擎层数字**已经包含采集开销**；`middlewares.NewMonitor` 只用于无法被引擎采集的独立处理器，
  叠加使用会重复计数；
- 需要请求体的场景（`ReadBody8KB`、`BindJSON1KB`）每轮重新构造请求，
  因此数字里包含 `httptest.NewRequest` 与 `bytes.NewReader` 的构造开销，属于**上界**；
- 端到端压测使用 `harness/stresstest`，32 个 worker、每场景 10 万请求、`KeepAlive` 开启，
  以 3 轮结果的中位数报告，避免单轮受 GC 与后台负载干扰。

### 2.3 运行命令

```bash
# 全量微基准：3 轮，约 3 分钟
go test ./bench -run '^$' -bench . -benchmem -benchtime=1s -count=3

# 端到端压测：5 个场景 × 3 轮，约 20 秒
go test ./bench -run TestEndToEndLoad -v -count=3

# 只跑单个场景或单个基准
go test ./bench -run 'TestEndToEndLoad/gzip' -v
go test ./bench -run '^$' -bench 'EngineRouting/Param' -benchmem
```

`-short` 会跳过端到端压测，便于在 CI 中只跑微基准。

---

## 三、引擎层结果

### 3.1 路由匹配

| 基准 | ns/op | B/op | allocs/op | 说明 |
| --- | --- | --- | --- | --- |
| `Routing/Static` | 154.1 | 24 | 2 | 静态路径 |
| `Routing/Table1000` | 187.8 | 24 | 2 | 1000 条路由表中的静态路径 |
| `Routing/Param` | 519.2 | 360 | 4 | 单参数 `/users/:id` |
| `Routing/Wildcard` | 504.5 | 376 | 4 | 通配 `/files/*path` |
| `Routing/GroupNested` | 418.3 | 360 | 4 | 两层分组 + 参数 |
| `Routing/NotFound` | 451.1 | 224 | 8 | 未命中（404 路径） |

路由表从 1 条增长到 1000 条，静态查找只从 154 ns 升到 188 ns，说明前缀树查找是
**O(路径段数)** 而非 O(路由总数)。参数化路由多出的约 365 ns 与 2 次分配来自提取路径参数
（`pathValues` 切片），存在参数的路由都会付出这部分成本，与参数个数基本无关。

`NotFound` 的 8 次分配比命中路径更多，因为未命中时要额外收集允许方法并构造错误响应体。

### 3.2 中间件编排

| 基准 | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `Middlewares/Chain0` | 150.7 | 24 | 2 |
| `Middlewares/Chain1` | 151.2 | 24 | 2 |
| `Middlewares/Chain4` | 152.8 | 24 | 2 |

0 层与 4 层中间件的差距约 2 ns，且分配次数完全相同。原因是 `Prepare()` 已经把
「全局链 + 分组链 + 路由处理器」折叠成一个闭包，运行期只做一次函数调用，
不存在切片遍历、条件判断或中间件对象分配。每层中间件的真实成本由它自己的逻辑决定，
见[第四节](#四中间件与组件结果)。

### 3.3 响应与请求

| 基准 | ns/op | B/op | allocs/op | 说明 |
| --- | --- | --- | --- | --- |
| `Response/Text` | 150.8 | 24 | 2 | 短文本响应 |
| `Response/JSONSmall` | 635.1 | 409 | 7 | `leego.JSON{"message":"pong"}` |
| `Response/JSON8KB` | 12496 | 64 | 3 | 120 条约 6.6 KB 的结构体切片，走池化缓冲区 |
| `Request/HeadersCookieIP` | 836.1 | 554 | 10 | 读头、写头、写 Cookie、取客户端 IP |
| `Request/BindJSON1KB` | 8866 | 11400 | 35 | 含每轮重建请求，上界 |
| `Request/ReadBody8KB` | 17638 | 22883 | 25 | 含每轮重建请求，上界 |

三点值得注意：

1. **文本响应几乎免费**：`Text` 与空处理器（`Chain0`）只差 0.1 ns，说明池化后的写入路径没有额外开销；
2. **小 JSON 贵在编码**：`JSONSmall` 要编码 `leego.JSON`（`map[string]any`），触发反射与 map 遍历，
   单次 7 次分配；`JSON8KB` 只编码结构体切片并复用缓冲区，6.6 KB 载荷耗时 12.5 µs，
   分配反而只有 3 次——**编码结构体远比编码 map 便宜**；
3. **请求体读取是最大的单点成本**：`ReadBody8KB` 的 22.9 KB 分配中，既有每轮重建请求的开销，
   也有 `io.ReadAll` 按 2 倍扩容产生的多次拷贝。已知长度的请求体按 `Content-Length` 预分配，
   或复用业务侧缓冲区，可以明显降低这部分开销。

### 3.4 并行扩展

| 基准 | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `EngineParallel` | 404.1 | 763 | 10 |

并行基准在 20 个逻辑核上运行参数化路由 + JSON 响应，`allocs/op` 为 10，与串行
`Routing/Param`（4）和 `Response/JSONSmall`（7）之和（11）基本相同，说明对象池与内置采集
在并发下没有引入新的分配来源。该数字衡量多核吞吐，不能与某个串行基准直接相除算加速比。

---

## 四、中间件与组件结果

### 4.1 中间件

基准中的处理器都是返回固定文本的空业务，因此 `ns/op` 可视为该中间件的**边际成本**。

| 基准 | ns/op | B/op | allocs/op | 成本来源 |
| --- | --- | --- | --- | --- |
| `MiddlewareRecovery` | 158.0 | 24 | 2 | 仅一层 `defer` |
| `MiddlewareBreaker` | 185.3 | 24 | 2 | 原子计数与状态判断 |
| `MiddlewareLimiter` | 211.0 | 59 | 3 | 令牌桶取令牌 |
| `MiddlewareCORS/Simple` | 260.0 | 40 | 3 | 写 3 个响应头 |
| `MiddlewareCORS/Preflight` | 541.6 | 192 | 7 | OPTIONS 预检直接结束 |
| `MiddlewareCache` | 524.0 | 304 | 8 | 命中后直接写出 |
| `MiddlewareTrace` | 1034 | 930 | 10 | 生成 trace 上下文 |
| `MiddlewareLogger` | 1131 | 1010 | 17 | 格式化访问日志 |
| `MiddlewareChain` | 1804 | 1421 | 24 | Logger + Recovery + CORS + Breaker |
| `MiddlewareSession` | 2263 | 2946 | 32 | 解析 Cookie、载入会话 |
| `MiddlewareCompress` | 8021 | 6017 | 7 | 约 4.1 KB 文本压缩，**524 MB/s**（按载荷长度计） |

`MiddlewareChain` 的 1804 ns 与四个单项之和（1131 + 158 + 260 + 185 ≈ 1734 ns）基本吻合，
说明中间件之间没有额外的编排损耗。`Session` 与 `Logger` 是分配最多的两个中间件，
高 QPS 场景应当评估是否必须开启、是否可以采样或异步化。

### 4.2 组件

| 基准 | ns/op | B/op | allocs/op | 说明 |
| --- | --- | --- | --- | --- |
| `ResponseWriterPool` | 9.0 | 0 | 0 | 从池中取写器并重置 |
| `Config/GetString` | 20.5 | 0 | 0 | 读配置项 |
| `MetricsStore/Append` | 28.8 | 0 | 0 | 追加一个采样点 |
| `Collector/IncreaseDecrease` | 36.4 | 0 | 0 | 记录一次请求的开始与结束 |
| `Config/GetSnapshot` | 63.0 | 0 | 0 | 取配置快照 |
| `Collector/Snapshot` | 305.1 | 320 | 1 | 汇总全部路由的采集数据 |
| `ValidateCheck` | 434.8 | 0 | 0 | 校验 3 个字段（元信息已缓存） |
| `MetricsStore/Query` | 497.6 | 2496 | 6 | 按窗口查询时间序列 |
| `Dashboard/Report` | 3427 | 3341 | 23 | 生成 JSON 报表 |
| `Dashboard/Page` | 4005 | 25648 | 13 | 渲染内嵌 HTML 页面（含 Recorder 分配） |
| `GatewayProxy` | 60396 | 40752 | 90 | 转发到本机回环上游 |

`Collector/IncreaseDecrease` 的 36 ns、0 分配是引擎层所有数字已经包含采集开销的直接依据；
`ResponseWriterPool` 的 9 ns 说明池化本身可以忽略。`GatewayProxy` 的 60 µs 主要花在
与上游完成一次完整的 HTTP 往返（基准用 `httptest.NewServer`，是真实的本机回环 TCP）
以及请求/响应头的转发上，不是转发逻辑本身的开销——因此它不能与纯内存的引擎基准直接相加。

---

## 五、端到端压测结果

每场景 32 并发、10 万请求、`KeepAlive` 开启，表内为中位轮（3 轮运行），
`avg` 为大样本均值，作 QPS 的辅助口径。

| 场景 | 链路 | QPS | avg | P90 | P99 | 三轮 QPS |
| --- | --- | --- | --- | --- | --- | --- |
| `basic-json` | 单路由 + JSON 响应 | 92772 | 318.7 µs | 1.036 ms | 1.828 ms | 80219 / 92772 / 95195 |
| `middleware-stack` | Logger + Recovery + CORS + Limiter + Breaker | 72577 | 409.6 µs | 1.059 ms | 1.803 ms | 43553 / 79870 / 72577 |
| `gzip-4kb` | Compress + 约 5.3 KB 文本 | 127732 | 230.4 µs | 924.2 µs | 2.006 ms | 127732 / 128533 / 125791 |
| `cache-hit` | Cache 命中后直接写出 | 87853 | 336.0 µs | 1.048 ms | 1.827 ms | 88864 / 87853 / 86528 |
| `h2c` | 明文 HTTP/2，单路由 + JSON | 47218 | 375.5 µs | 880.9 µs | 5.528 ms | 47665 / 47218 / 46480 |

所有场景均无网络错误，状态码 100% 为 `200`，成功率 100%。

### 5.1 如何解读

1. **缓存命中略慢于裸 JSON（88k 对 93k QPS）**：`Cache` 命中本身只要 524 ns，
   但命中路径仍要完成中间件包装与响应写出；在客户端成为瓶颈的测量条件下，
   两者的差距落在轮间波动范围内，不应解读为「缓存拖慢了接口」。
   真正收益在业务处理器昂贵时才会显现。
2. **gzip 场景吞吐最高（12.8 万 QPS）**：约 5.3 KB 文本压缩到百字节量级，
   回环上的字节数与系统调用次数显著下降。压缩本身要 8 µs（4.1 KB 载荷、524 MB/s），
   但对**大响应**来说「压缩换带宽」在当前环境下是净收益。
3. **4 层中间件栈比裸接口低约 22%**：与第 4.1 节的单项成本一致，
   其中 Logger 贡献最大。
4. **h2c 单连接吞吐约为 HTTP/1.1 的一半，但 P90 更低**：32 并发在 HTTP/2 下复用同一条 TCP 连接，
   多路复用带来的帧头、流控与客户端协议栈开销抵消了省下的连接成本；
   P99 明显抬高（5.5 ms）说明单连接上的队头效应在尾延迟上可见。
   结论是 **h2c 的收益在跨网络、需要多路复用时才成立，本机回环不算它的优势场景**。

---

## 六、优化建议

### 6.1 使用侧

| 场景 | 建议 | 依据 |
| --- | --- | --- |
| 返回固定结构 | 优先编码结构体而非 `map` | `JSON8KB` 3 次分配、6.6 KB 仅 12.5 µs，`JSONSmall` 7 次分配 |
| 大响应 | 开启 `compress` | 约 5.3 KB 压到百字节量级，端到端吞吐提升约 37% |
| 已知长度的请求体 | 按 `Content-Length` 预分配或复用缓冲区 | `ReadBody8KB` 22.9 KB 分配、17.6 µs |
| 高频只读接口 | 评估 `cache` 中间件 | 命中 524 ns，避免重复计算 |
| 生产日志 | 采样或异步输出 | Logger 1.1 µs / 17 次分配 |
| 会话 | 仅在需要时启用 | Session 2.3 µs / 32 次分配，是所有中间件中最重的 |
| 尾延迟敏感 | 避免单连接承载过多并发流 | h2c P99 5.5 ms 高于 HTTP/1.1 的 1.8 ms |

### 6.2 框架侧（后续可优化点）

1. **参数化路由的 `pathValues` 分配**：目前每个带参数的路由请求固定多 2 次分配、约 336 B。
   可以在上下文中按参数个数复用定长数组，或把参数值直接写入池化的字符串切片；
2. **`leego.JSON`（`map`）响应**：可在 `response` 包增加「预编码字节」入口，
   让固定响应跳过反射；
3. **`ReadBody` 的扩容拷贝**：`Content-Length` 已知时一次性分配，未知时改用 `bytes.Buffer` 的增长策略；
4. **`Session` 的会话对象**：把会话 map 与 Cookie 解析移到对象池，
   目标是降到与 `Cache` 同一量级；
5. **`Dashboard/Page` 的 25 KB 分配**：页面是常量，可在首次生成后缓存字节切片。

以上任何一项都应先用本报告的复现命令取得基线，再以 `-count=5` 加 `benchstat` 对比，
避免把噪声当成优化效果。

---

## 七、已知局限

| 局限 | 影响 | 应对 |
| --- | --- | --- |
| 单调时钟粒度 512.9 µs | 亚毫秒响应的延迟样本在 0、512.9 µs 与 1 ms 附近跳动，`P50` 常显示 `0s`，`P90` 常落在 1 ms 附近 | 分位只按粒度倍数解读；横向比较用 `avg` 与 QPS，或换 Linux / 更高分辨率时钟的平台重测 |
| 客户端与服务器同进程 | 压测客户端自身占用 CPU，32 并发时无法线性扩展（同一服务 1 并发约 1.5 万 QPS，32 并发按响应大小不同只有 2.5 万–12.8 万 QPS） | QPS 只作端到端参考；要测服务端上限，请把 `harness` 客户端与服务器分进程、分机器运行 |
| 本机回环 | 没有真实网卡的带宽、RTT 与丢包 | 跨网络容量必须实测，不能外推 |
| 无 TLS | 未覆盖握手、对称加密与 ALPN 开销 | 需要时用 `RunTLS` 单独压测 |
| 未覆盖长连接协议 | SSE、WebSocket、文件上传下载不在本次范围 | 参见 `docs/sse-websocket.md`，按连接数与消息速率单独设计基准 |
| 单机、单版本 | 数值与 CPU、Go 版本、后台负载强相关 | 对比优化前后时，同一台机器、同一版本、同一时段运行 |
| 微基准不含 socket | 引擎层数字是纯框架开销，不含内核协议栈 | 与端到端结果互为参照，不要互相替代 |

延迟分位的具体表现可以这样理解：一次 300 µs 的请求有约六成概率跨越时钟跳变，
从而被记为 0 或 512.9 µs 的整数倍；而 QPS 由整段墙钟时间计算，不受该粒度影响。
因此本报告把 **QPS 作为吞吐主指标，把 `avg` 作为延迟主指标**，分位数仅用于观察尾部长短。

---

## 八、复现与回归

```bash
# 1. 微基准基线（保存到文件，便于 benchstat 对比）
go test ./bench -run '^$' -bench . -benchmem -benchtime=1s -count=5 > baseline.txt

# 2. 端到端基线
go test ./bench -run TestEndToEndLoad -v -count=3 | Tee-Object e2e.txt

# 3. 修改代码后重复上述两步，再比对
go install golang.org/x/perf/cmd/benchstat@latest
benchstat baseline.txt after.txt
```

`bench` 包纳入常规回归：`go test ./...` 会编译全部基准，`-short` 跳过端到端压测。
端到端结果以 `PERF` 行打印，字段与第五节表格一一对应：

```text
PERF scenario=basic-json concurrent=32 total=100000 qps=92772 min=0s avg=318.659µs ...
```
