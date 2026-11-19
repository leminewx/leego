# 指标与分析后台

Leego 把「指标采集」做成框架内建能力：每个注册路由自带采集器，无需额外中间件
即可获得 QPS、状态码分布、延迟与在途请求数。`dashboard` 包在此基础上提供单页
分析后台与 JSON / CSV 导出接口。

---

## 指标模型

### 层级

```
Report（一次汇总）
├── GeneratedAt
├── Totals（全局汇总）
└── Routes[]（Snapshot，按方法与模式排序）
    └── Metrics[]（Metric，按状态码升序）
```

### `Snapshot`

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `Name` | `string` | 路由名称（注册时 `tags[0]`） |
| `Method` | `string` | HTTP 方法 |
| `Pattern` | `string` | 注册模式（含 `:param` / `*param`） |
| `Tags` | `[]string` | 业务标签（最多 5 个） |
| `Enabled` | `bool` | 路由是否启用 |
| `Flights` | `int64` | 当前在途请求数 |
| `Metrics` | `[]Metric` | 各状态码指标 |
| `Total` | `uint64` | 累计响应次数 |
| `Failed` | `uint64` | **5xx** 响应次数 |
| `TotalCostMicro` | `int64` | 累计耗时（微秒） |

派生方法：`GetAverageCostMicro()`、`GetFailureRate()`。

> `Failed` 只统计 `>= 500` 的响应。4xx 属于客户端问题，不计入失败率；需要 4xx
> 比例时从 `Metrics` 中按状态码自行汇总。

### `Metric`

| 字段 | 含义 |
| --- | --- |
| `Code` | 状态码 |
| `Total` | 该状态码的响应次数 |
| `MaxCostMicro` | 最大耗时（微秒） |
| `MinCostMicro` | 最小耗时（微秒）；从未记录时为 0 |
| `TotalCostMicro` | 累计耗时（微秒） |

派生方法：`GetAverageCostMicro()`。

### `Totals`

`Routes`、`Requests`、`Flights`、`Failed`、`TotalCostMicro`，以及
`GetAverageCostMicro()`、`GetFailureRate()`。

---

## 采集流程

```go
// 引擎内部等价逻辑
monitor := handler.GetCollector()
monitor.Increase()                              // 在途 +1
start := time.Now()
if handler.IsEnabled() {
	handler.ServeHTTP(ctx)
} else {
	ctx.ResponseError(http.StatusLocked, "locked") // 禁用路由
}
monitor.Decrease(ctx.GetStatusCode(), time.Since(start))
```

细节：

- `Decrease` 在状态码为 0 时按 `200` 记录，避免「未写响应」的路由出现 `0` 桶；
- 耗时以微秒记录，负数被截断为 0；
- 最大/最小值的「读-比较-写」由互斥锁保护，与 `Flights` 的原子计数分属不同
  同步域，二者不混用；
- 快照是深拷贝，采集器继续累加不会影响已生成的快照。

### 读取指标

```go
report := engine.CollectReport()              // 汇总报告

for _, snapshot := range engine.GetAllHandlerSnapshots() {
	// 直接序列化，或按需计算
}
```

`CollectReport()` 与 `GetAllHandlerSnapshots()` 都是只读操作，可并发调用。

### 运行时操作

```go
handler, _ := engine.GetHandler(http.MethodGet, "/users/:id")
collector := handler.GetCollector()

collector.Reset()                     // 清空计数，保留元信息与启停状态
snapshot := collector.GetSnapshot()    // 只读快照
clone := collector.Clone()             // 独立副本，可长期持有
```

---

## 分析后台

### 注册

```go
import "gitee.com/leminewx/leego/dashboard"

err := engine.RegisterDashboard("/metrics",
	dashboard.UseTitle("订单服务指标"),
	dashboard.UseRefreshInterval(5*time.Second),
	dashboard.UseMaxRoutes(200),
	dashboard.EnableExport(true),
	dashboard.UseAuthorizer(func(req *http.Request) bool {
		return req.Header.Get("Authorization") == "Bearer admin-token"
	}),
)
```

`RegisterDashboard` 的默认行为：

- 前缀为空时使用 `/metrics`；
- 自动注入 `dashboard.UseProvider(engine.CollectReport)`，无需手动提供数据源；
- 同时注册 `GET <prefix>` 与 `GET <prefix>/*filepath`，并补 `HEAD`；
- 返回错误表示注册失败（例如引擎已冻结）。

### 端点

| 端点 | 方法 | 说明 |
| --- | --- | --- |
| `<prefix>` | GET | 单页后台 |
| `<prefix>/api/report` | GET | 完整报告（JSON，缩进输出） |
| `<prefix>/api/routes` | GET | 路由清单（JSON） |
| `<prefix>/api/health` | GET | 健康检查：`status`、`started_at`、`uptime` |
| `<prefix>/export.csv` | GET | CSV 导出（需开启导出） |

路径解析采用**后缀匹配**，因此后台可以挂在任意前缀下，且兼容结尾斜杠：

- `<prefix>`、`<prefix>/`、`<prefix>/index.html` 都渲染页面；
- 只有一段路径时一律视为页面（避免把未知子路径误判为 404）；
- 其他路径返回 `404`；非 `GET`/`HEAD` 返回 `405 + Allow: GET, HEAD`。

### 查询参数

`api/report`、`api/routes`、`export.csv` 支持同一组过滤参数：

| 参数 | 作用 |
| --- | --- |
| `q` | 按方法、模式、名称、标签模糊匹配（大小写不敏感） |
| `method` | 按方法精确过滤 |
| `status` | 按状态码过滤，只保留匹配的状态码指标；无匹配的路由会被整条移除 |
| `enabled` | `true` / `false` 过滤启用状态 |

过滤后 `Totals` 会基于保留下来的路由重新计算，而不是沿用原始汇总。

### 截断策略

- `UseMaxRoutes(n)` 对报告与路由清单生效：按请求量从高到低保留前 `n` 条，
  截断后重算 `Totals`；
- CSV 导出额外限制最多 `5000` 行（防止超大报表拖垮浏览器与 Excel）；
- 默认 `MaxRoutes` 为 `500`。

### CSV 格式

列固定为：

```
method,pattern,name,status,total,avg_cost_micro,max_cost_micro,min_cost_micro
```

- 每个「路由 × 状态码」一行；
- 没有指标的路由输出一行，`status`、`max`、`min` 为空，`total` 与平均值照常输出；
- 文件名为 `leego-metrics.csv`，编码为 `text/csv; charset=utf-8`。

### 安全响应头

后台所有响应都会带上：

```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Cache-Control: no-store
```

`Authorizer` 返回 `false` 时统一响应 `401`。默认不鉴权，**生产环境必须配置**，
或把后台挂到内网/独立端口。

---

## 扩展后台

### 自定义数据源

```go
dashboard.UseProvider(func() collector.Report {
	// 合并多个引擎、过滤敏感路由、补充外部指标等
	return collector.NewReport(snapshots)
})
```

`Provider` 每次请求调用一次，必须返回不可变快照；不要在内部修改采集器状态。

### 自定义选项

`Option` 是公开的函数类型，可直接编写：

```go
func UseOnlyHealthyRoutes() dashboard.Option {
	return func(options *dashboard.Options) {
		options.Title = "健康路由指标"
	}
}
```

### 替换页面

页面通过 `go:embed` 内嵌 `dashboard/assets/index.html`，创建时替换三个占位符：

| 占位符 | 替换内容 |
| --- | --- |
| `{{TITLE}}` | 页面标题（已做 HTML 转义） |
| `{{REFRESH_MS}}` | 自动刷新间隔（毫秒） |
| `{{EXPORT_ENABLED}}` | 是否开启 CSV 导出 |

需要定制外观时直接修改 `assets/index.html`，并保留上述占位符。页面为静态字节
切片，渲染后缓存在 `Dashboard` 内，不存在每请求模板开销。

---

## 典型用法

### 运维接口

```go
engine.GET("/internal/metrics", func(ctx *leego.Context) {
	_ = ctx.ResponseJSON(http.StatusOK, engine.CollectReport())
})
```

### 指标重置

```go
engine.POST("/internal/metrics/reset", func(ctx *leego.Context) {
	for _, route := range engine.GetRoutes() {
		if handler, ok := engine.GetHandler(route.Method, route.Pattern); ok {
			handler.GetCollector().Reset()
		}
	}
	ctx.SetStatusCode(http.StatusNoContent)
})
```

### 告警采集

```go
ticker := time.NewTicker(time.Minute)
defer ticker.Stop()

for range ticker.C {
	report := engine.CollectReport()
	if report.Totals.GetFailureRate() > 0.05 {
		alert(report)
	}
}
```

---

## 常见问题

**Q：为什么 `Failed` 一直是 0，但 404 很多？**
`Failed` 只统计 5xx。404/401 等 4xx 属于客户端问题，需要从 `Metrics` 中按状态码
统计。

**Q：为什么路由的 `Total` 与各状态码之和一致，但 `Flights` 不为 0？**
`Flights` 是瞬时值，报告生成时可能有请求正在处理。它不会随报告冻结。

**Q：后台页面数据不刷新？**
检查前端刷新间隔与浏览器缓存：后台响应带 `Cache-Control: no-store`，若仍被代理
缓存，请检查反向代理配置。

**Q：能直接对接 Prometheus 吗？**
可以。用 `GetAllHandlerSnapshots()` 或 `CollectReport()` 自行转换为文本格式，
挂到独立端点即可；`dashboard` 不绑定具体监控系统。
