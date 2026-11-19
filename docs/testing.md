# 测试指南

本项目的测试目标不是“凑覆盖率”，而是**固化行为契约**：路由匹配优先级、冻结时机、
错误响应格式、并发安全、池化约束、协议帧解析等，都必须有可回归的断言。

---

## 一、常用命令

在 `leego` 模块根目录执行：

```bash
# 全量单元测试
go test ./...

# 禁用缓存，确保真正执行
go test ./... -count=1

# 数据竞争检测（必跑）
go test -race ./...

# 覆盖率汇总
go test ./... -cover

# 生成可浏览的覆盖率报告
go test ./... -coverprofile=cover.out
go tool cover -html=cover.out -o cover.html

# 只跑某个包 / 某个用例
go test ./middleware/jwt/ -run TestParser -v

# 基准测试（带内存分配统计）
go test ./... -run '^$' -bench . -benchmem

# 输出更多日志
go test ./... -v
```

> Windows PowerShell 不支持 `&&`，多个命令请用 `;` 分隔。

**建议的提交前检查顺序**：

```bash
gofmt -l .
go vet ./...
go test -race ./...
```

`gofmt -l .` 应输出空列表；有任何文件被列出就执行 `gofmt -w <file>`。

---

## 二、测试组织

绝大多数测试使用**与被测包相同的 package**（内部测试），以便直接验证未导出符号——
例如路由器的 `search`、熔断器的窗口推进、SSE 的帧格式化。这是有意的取舍：

- 好处：可以测到真正的实现细节，避免为了测试而导出内部 API；
- 代价：测试与实现耦合更紧，重构时可能需要同步调整测试。

例外是 `harness/service_test.go`：它使用外部包 `harness_test`，只通过 `NewService`
与真实 HTTP 客户端验证公开契约，以便及早发现 Cookie 下发、响应头顺序等
内部测试（`httptest.ResponseRecorder`）容易掩盖的问题。

| 位置 | 覆盖重点 |
| --- | --- |
| `*_test.go`（根包） | 引擎启停、路由匹配/405、分组、Handler 链、Context 绑定与响应、密码哈希 |
| `tree/` | 节点类型、Trie 插入与匹配优先级、冲突检测 |
| `middleware/` | 日志、恢复、CORS、JWT、限流三算法、熔断状态机与指标 |
| `middleware/collector/` | 指标累计、快照、并发安全、报告聚合 |
| `dashboard/` | 端点解析、过滤、截断、CSV、安全头、页面渲染 |
| `response/` | 写入器池化、状态码、SSE 消息与管理器、WebSocket 帧与握手 |
| `harness/stresstest/` | 配置校验、请求分配、分位数算法、报告格式 |
| `harness/testutil/` | 测试客户端、响应断言、Cookie 与冻结时钟 |
| `harness/` | 体验服务集成：会话、缓存、限流、上传、网关、SSE、任务与链路 |

**命名约定**：

| 类型 | 形式 | 示例 |
| --- | --- | --- |
| 测试 | `Test<被测对象><行为>` | `TestEngineNotFound`、`TestParserRejectsTamperedSignatureAndPayload` |
| 基准 | `Benchmark<被测对象><场景>` | `BenchmarkRouterSearchParam` |
| 辅助函数 | `create<对象>` / `perform<动作>` | `createTestEngine`、`performRequest` |

测试名必须与**当前**被测 API 同名：被测函数从 `CreateXxx` 改为 `NewXxx` 后，
测试名同步改为 `TestNewXxx`；测试辅助函数不受构造函数命名规则约束，
继续使用 `createTestXxx` 以区分「框架构造」与「测试夹具搭建」。

---

## 三、辅助函数

各包在测试文件顶部定义少量辅助函数，**不跨包共享**（Go 测试文件不能互相导入）：

| 包 | 辅助函数 | 作用 |
| --- | --- | --- |
| 根包 | `createTestEngine()` | 构造关闭启动日志的引擎 |
| 根包 | `performRequest(engine, method, target, body)` | 直接调 `ServeHTTP` 并返回 `*httptest.ResponseRecorder` |
| 根包 | `createTestContext(req)` | 构造 `Context` 与记录器 |
| `jwt` | `createTestSigner(t)`、`createSignedToken(t, signer, claims)`、`createTestClaims()` | 构造签名器、令牌与合法声明 |
| `sse` | `createSyncRecorder()`、`waitForCondition(t, timeout, cond)` | 并发安全记录器、条件轮询等待 |
| `router` | `newRouter(...)` | 构造已插入路由的测试路由器 |

`waitForCondition` 的实现要点：以 5ms 为步长轮询，超时后 `Fatal` 并输出当前状态。
它用于等待**异步协程**产生可观测结果，而不是用固定 `time.Sleep` 猜时间。

---

## 四、编写风格

### 4.1 表驱动 + 子测试

同一逻辑的多组输入优先使用表驱动，便于定位失败用例：

```go
func TestCreateHMACSignerUsesExpectedAlgorithm(t *testing.T) {
	cases := []struct {
		name      string
		algorithm Algorithm
	}{
		{name: "HS256", algorithm: HS256},
		{name: "HS384", algorithm: HS384},
		{name: "HS512", algorithm: HS512},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// 断言
		})
	}
}
```

> Go 1.22 起循环变量每次迭代都是新变量，因此不再需要 `testCase := testCase`。
> 但**捕获循环变量到 `t.Parallel()` 子测试**时仍需注意执行时机；本仓库未使用 `t.Parallel()`，
> 以避免共享全局状态（混色日志、模式变量等）带来的干扰。

### 4.2 断言信息要能自证

失败信息应包含**实际值、期望值与输入**：

```go
if recorder.Code != http.StatusMethodNotAllowed {
	t.Fatalf("状态码 = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
}
```

不要写 `t.Fatal("失败")`——半年后的维护者无法凭它定位问题。

### 4.3 用 `t.Fatalf` 还是 `t.Errorf`

- 后续断言依赖当前结果（如 `nil` 检查）→ `t.Fatalf` 立即终止；
- 多组独立断言希望一次看全 → `t.Errorf` 继续执行。

### 4.4 资源清理

- 临时目录用 `t.TempDir()`，测试结束自动删除；
- 启动的服务用 `t.Cleanup` 或 `defer` 关闭；
- 需要端口的测试监听 `127.0.0.1:0`，由内核分配空闲端口，避免 CI 端口冲突。

---

## 五、HTTP 测试

### 5.1 直接用 `httptest.ResponseRecorder`

绝大多数路由/中间件测试不需要真实网络：

```go
recorder := httptest.NewRecorder()
engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ping", nil))
```

优点：无端口、无协程、无超时，执行快且稳定。

### 5.2 需要真实连接时用 `httptest.NewServer`

SSE、WebSocket、优雅关闭、`Engine.Run` 相关测试需要真实 TCP：

```go
server := httptest.NewServer(engine)
defer server.Close()

response, err := http.Get(server.URL + "/events")
```

WebSocket 测试还需把 `http://` 替换为 `ws://`，并使用包内提供的客户端。

### 5.3 并发安全的记录器

`httptest.ResponseRecorder` 不是并发安全的。SSE 的写入发生在独立协程中，
因此 `sse` 包内定义了 `syncRecorder`：内部持锁，记录状态码、响应体与是否 `Flush`。

```go
type syncRecorder struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	status  int
	flushed bool
}
```

编写类似测试时，**先确认记录器是否会被多协程访问**；会的话必须自己加锁，
否则 `-race` 会直接报错。

---

## 六、并发测试

并发安全是本项目的核心契约之一。测试至少覆盖以下模式：

### 6.1 多协程同时读写

```go
const goroutines, iterations = 16, 50

var waitGroup sync.WaitGroup
for worker := 0; worker < goroutines; worker++ {
	waitGroup.Add(1)
	go func(worker int) {
		defer waitGroup.Done()
		for index := 0; index < iterations; index++ {
			// 读或写共享对象
		}
	}(worker)
}
waitGroup.Wait()
```

配合 `go test -race ./...` 才能真正发挥作用。

真实用例：

- `TestParserConcurrentUse`（jwt）：16 协程 × 50 次解析；
- `TestParserConcurrentClockSkewUpdates`（jwt）：4 个写协程持续 `SetClockSkew`，
  8 个读协程持续 `GetClockSkew` 并解析令牌——回归“配置热更新与解析竞争”；
- `TestRouterConcurrentSearch`（根包）：并发查询已冻结的路由表；
- collector、limiter、breaker 各自的并发累计测试。

### 6.2 并发基准

```go
b.RunParallel(func(pb *testing.PB) {
	index := 0
	for pb.Next() {
		router.Search(http.MethodGet, paths[index%len(paths)])
		index++
	}
})
```

`RunParallel` 会自动按 `GOMAXPROCS` 启动协程，适合评估锁竞争。
对比 `BenchmarkEngineServeHTTP` 与 `BenchmarkEngineServeHTTPParallel` 可以观察串行/并行的差距。

### 6.3 并发测试的常见陷阱

| 陷阱 | 后果 | 规避 |
| --- | --- | --- |
| 断言写在协程里直接调用 `t.Fatalf` | 只终止当前协程，测试可能“假通过” | 用 `channel` 收集错误，主协程断言 |
| 共享 `map` 无锁写入 | `-race` 报错或进程崩溃 | 每个 worker 独立采集后合并 |
| 依赖 `time.Sleep` 等协程完成 | CI 上偶发失败 | 用 `waitForCondition` 或 `channel` 同步 |
| 测试中修改全局状态（日志级别、模式） | 用例相互污染 | 用 `defer` 恢复，或避免修改 |

---

## 七、时间敏感测试

限流器、熔断器、SSE 心跳都依赖时间。原则是：

1. **尽量缩短窗口**：限流测试用几十毫秒的窗口，而不是 1 秒；
2. **留足余量**：断言“窗口过期后放行”时，等待时间取窗口的 1.5～2 倍；
3. **不测绝对时间**：断言“放行/拒绝”这类状态，而不是精确的耗时数值；
4. **优先用条件等待**：`waitForCondition(t, 2*time.Second, func() bool { ... })`。

需要注意的**硬性下限**（源码固定值，无法通过参数调小）：

| 组件 | 固定值 |
| --- | --- |
| 滑动窗口限流 | 窗口最小 1 秒 |
| 熔断器 | 滑动窗口固定 10 个桶 |
| 限流等待轮询 | 50ms |
| SSE 默认心跳 | 30 秒（可通过配置调小） |

---

## 八、基准测试

当前仓库共有 28 个基准，主要集中在热路径：

| 包 | 基准 |
| --- | --- |
| 根包 | `BenchmarkEngineServeHTTP`、`BenchmarkEngineServeHTTPParallel`、`BenchmarkRouterSearchStatic`、`BenchmarkRouterSearchParam`、`BenchmarkRouterSearchWildcard`、`BenchmarkRouterParallelSearch` |
| `tree` | `BenchmarkTrieSearchStatic`、`BenchmarkTrieSearchParameter`、`BenchmarkTrieSearchParallel` |
| `middleware/collector` | `BenchmarkIncreaseDecrease`、`BenchmarkGetSnapshot` |
| `middleware/limiter` | `BenchmarkFixedWindowAllow`、`BenchmarkSlidingWindowAllow`、`BenchmarkTokenBucketAllow` |
| `middleware/jwt` | `BenchmarkHMACSignerSign`、`BenchmarkCreateHeader`、`BenchmarkParserParse` |
| `response` | `BenchmarkCreateWriter`、`BenchmarkWriterPool` |
| `response/sse` | `BenchmarkMessageWrite`、`BenchmarkMessageFormat`、`BenchmarkClientWriteMessage`、`BenchmarkManagerBroadcast` |
| `dashboard` | `BenchmarkServePage`、`BenchmarkServeReport` |
| `harness/stresstest` | `BenchmarkLatenciesAppend`、`BenchmarkLatenciesGetPercentile`、`BenchmarkMetricsRecord` |

**编写基准的要点**：

- 用 `b.ReportAllocs()` 暴露内存分配；
- 构造数据的开销放在 `b.ResetTimer()` 之前；
- 不要在循环内做日志、字符串拼接等与测量目标无关的事；
- 需要防止编译器优化掉结果时，把结果赋给包级变量或 `b.Fatal` 消费掉。

**对比性能变化**：

```bash
go test ./tree/ -run '^$' -bench Trie -count 5 > old.txt
# 修改代码
go test ./tree/ -run '^$' -bench Trie -count 5 > new.txt
```

如需统计显著性，可安装 `golang.org/x/perf/cmd/benchstat` 后执行 `benchstat old.txt new.txt`。

---

## 九、覆盖率

```bash
go test ./... -cover
```

关注两类数字：

1. **包级覆盖率**：核心包（根包、tree、collector、jwt、limiter、breaker、sse、websocket）应保持较高覆盖；
2. **未覆盖分支**：用 HTML 报告定位，优先补“错误路径”和“边界条件”，而不是重复主流程。

无法覆盖的分支（如 `os.Exit`、真实信号、网络不可达）可在报告中说明原因，
不要为了数字写无意义的测试。

---

## 十、回归测试的写法

修复缺陷时，先写**能复现缺陷的失败测试**，再改实现。注释里写清楚“为什么这个用例重要”：

```go
// TestParseAndValidateIssuerAndAudienceHonorsClockSkew 验证签发者/受众校验沿用时钟偏差。
//
// 回归场景：解析器先按偏差校验时间，再校验签发者与受众；若第二次校验丢弃偏差，
// 则「仅轻微过期」的令牌会被误判为过期，与单独做时间校验的结果不一致。
func TestParseAndValidateIssuerAndAudienceHonorsClockSkew(t *testing.T) {
	// ...
}
```

这条注释比测试名更能防止未来有人“顺手简化”而重新引入缺陷。

---

## 十一、CI 建议

```yaml
# 概念示例，按实际 CI 平台调整
steps:
  - run: gofmt -l .
  - run: go vet ./...
  - run: go test ./... -count=1
  - run: go test -race ./... -count=1
  - run: go test ./... -coverprofile=cover.out
```

注意事项：

- `-race` 会显著变慢，可与普通测试拆成两个 job；
- `-count=1` 禁用缓存，避免“本地通过、CI 跳过”；
- 覆盖率文件可上传到 CI 的制品或覆盖率服务；
- Windows 与 Linux 的行为差异主要出现在路径与终端颜色判定上，
  涉及文件的测试请使用 `filepath.Join` 与 `t.TempDir()`。

---

## 十二、常见问题

**Q：`go test ./...` 报 `no required module provides package`？**
A：确认在 `leego` 模块根目录执行，且 `go.mod` 中的模块路径为 `gitee.com/leminewx/leego`。

**Q：`-race` 在 CI 上偶发超时？**
A：先在本地复现；确认没有依赖 `time.Sleep` 的同步；必要时给 `go test` 加 `-timeout 5m`。

**Q：测试能访问未导出字段，是否说明设计有问题？**
A：不是。内部测试是 Go 的标准做法，用于验证实现契约；对外行为仍通过 `Engine.ServeHTTP`
等公开入口验证。

**Q：SSE/WebSocket 测试偶发失败？**
A：异步写入必须用 `waitForCondition` 等待，不要用固定睡眠；连接关闭后用 `defer` 确保清理，
避免协程泄漏影响后续用例。
