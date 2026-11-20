# 开发者体验服务（harness）

`harness` 把仓库里原本分散的开发者工具收敛到一个包内，并提供一套可直接运行的
实体服务：内置单页前端、两个模拟上游、内存会话、分片上传、SSE、任务调度与
链路追踪，用于在本地一次性体验框架的主要能力。

自 v0.15.0.0 起，`stresstest`、`testutil` 与 `examples` 均位于 `harness/` 下。

---

## 一、目录结构

```
harness/
├── harness.go              # 体验服务：配置、装配、路由与生命周期
├── openapi.go              # 结构化 OpenAPI 声明：分组、摘要、模式与多响应
├── harness_test.go         # 配置归一化、选项与装配单测
├── openapi_internal_test.go # 声明表与真实路由、分组、模式的一致性校验
├── service_test.go         # 真实 HTTP 集成测试
├── statics/index.html      # 单页前端（go:embed，无外部 CDN）
├── testdata/               # 演示与测试数据：data.txt / ws-client.html / stress.html
├── internal/assetpath/     # 定位 testdata 目录（示例与基准服务共用）
├── tools/genlogo/main.go   # 品牌图生成器（//go:build ignore，不参与构建）
├── cmd/harness/main.go     # 可执行入口（flag + 信号关闭）
├── testutil/               # HTTP 测试客户端、响应断言、冻结时钟
├── stresstest/             # 压测客户端、分位数与报告
│   └── test/base/          # 压测基准服务
└── examples/               # 可运行示例：base / file / sse / websocket / stress
```

示例与基准服务不再各自携带数据文件：纯文本、WebSocket 调试页与压测说明页
统一放在 `harness/testdata`，由 `harness/internal/assetpath` 定位。
`assetpath.GetDir()` 先按源码位置推断，再依次尝试 `harness/testdata`、
`../testdata`、`testdata`，因此在仓库根目录 `go run ./harness/examples/base`，
或在其它工作目录下运行，都能找到同一份数据；`assetpath.Resolve()` 会把
通配符路径限制在数据目录内，越界时回落到 `data.txt`。

导入路径统一下移一层：

| 旧路径 | 新路径 |
| --- | --- |
| `gitee.com/leminewx/leego/stresstest` | `gitee.com/leminewx/leego/harness/stresstest` |
| `gitee.com/leminewx/leego/testutil` | `gitee.com/leminewx/leego/harness/testutil` |
| `examples/*`（不可导入） | `harness/examples/*`（不可导入） |

---

## 二、快速开始

```bash
# 默认监听 127.0.0.1:8080，数据目录位于系统临时目录
go run ./harness/cmd/harness

# 自定义监听地址与数据目录
go run ./harness/cmd/harness -addr :9000 -data ./tmp
```

启动后打开 `http://127.0.0.1:8080/`，左侧菜单底部提供平台入口：

| 入口 | 说明 |
| --- | --- |
| `/dashboard` | 指标分析后台 |
| `/openapi` | OpenAPI 文档与浏览页 |
| `/metrics` | 指标后台（JSON / CSV） |
| `/metrics/prometheus` | Prometheus 文本指标 |
| `/api/traces` | 最近 256 条链路 |

进程收到 `SIGINT` / `SIGTERM` 后先停止接收新请求，再执行关闭钩子。

---

## 三、体验端点

| 端点 | 能力 |
| --- | --- |
| `GET /` | 前端页面 |
| `GET /api/hello` | 服务概览：版本、Go 版本、运行时长、配置与后台链接 |
| `GET /api/healthz` | 健康检查（含调度器统计） |
| `GET/POST /api/echo` | 请求回显：方法、头、查询参数、请求体 |
| `GET /api/session/current` | 当前会话 |
| `POST /api/session/login` | 写入会话并轮换 ID |
| `POST /api/session/logout` | 销毁会话并清除 Cookie |
| `GET /api/cache/timestamp` | 响应缓存演示，响应头带 `X-Cache` |
| `GET /api/limited/ping` | 令牌桶限流（非阻塞，超额直接 429） |
| `POST/PUT/GET/DELETE /api/upload` | 分片上传：init / part / status / abort |
| `GET /api/upload/download/*filepath` | 下载已完成文件（限制在 `files/` 内） |
| `GET /api/events` | SSE：`hello` 事件后每秒推送一次 `tick` |
| `GET/POST /api/gateway/*path` | 反向代理；`X-Leego-Canary: v2` 走灰度上游 |
| `GET /api/jobs` | 调度器统计：已执行、丢弃、待执行 |
| `GET /api/traces` | 最近链路（由内存导出器托管） |

### 前端布局

页面采用左侧菜单 + 右侧功能区的两栏结构：

- 左栏按能力列出 10 个面板，底部固定平台入口（分析后台、OpenAPI、指标、Prometheus）；
- 右栏顶部显示当前面板的标题与说明，内容区一次只渲染一个面板；
- 面板位置记录在 `#overview`、`#upload` 等 hash 中，刷新后仍停留在同一面板；
- 宽度小于 900px 时侧栏收起为抽屉，由顶部“菜单”按钮展开，点击遮罩关闭；
- 请求全部使用原生 `fetch` / `EventSource`，响应以纯文本写入 `textContent`。

### OpenAPI 文档

`harness` 通过 `openapi.UseServer` / `UseTag` / `UseSchema` / `UseOperations`
把路由标签、中文摘要、参数说明、请求体与多状态码响应声明为元数据：

| 能力 | 声明方式 |
| --- | --- |
| 分组 | 路由第二个标签（`engine.GET(pattern, handle, name, tag)`），与 `UseTag` 同名 |
| 摘要与描述 | `UseOperation("GET /api/hello", openapi.Operation{Summary, Description})` |
| 复用模式 | `UseSchema("HarnessOverview", overviewResponse{})` + `NewJSONRefResponse` |
| 请求体 | `NewJSONRequestBody(required, example)` 或显式 `RequestBody` |
| 多状态码 | `Responses` 中的 `200` / `400` / `404` / `429` 等键 |
| 媒体类型 | `MEDIA_JSON` / `MEDIA_TEXT` / `MEDIA_EVENT_STREAM` / `MEDIA_OCTET_STREAM` |

浏览页按分组列出接口，详情区展示方法徽章、路径、operationId、参数表、
请求体、可展开的响应体与 cURL 示例；cURL 的基地址取自文档的 `servers`，
因此在非 8080 端口运行时示例仍然正确。

前端菜单覆盖：概览、回显、会话、缓存、限流（并发 20 次统计 200/429）、
分片上传（进度与下载）、SSE、网关灰度、任务统计与链路列表。

侧栏顶部的标识是内联 SVG（靛蓝到青绿的渐变圆角方 + 字形 L），与
`docs/assets/logo.png` 同源：两者都在 48 单位逻辑坐标系里描述，
修改样式时需要用 `go run ./harness/tools/genlogo/main.go ./docs/assets/logo.png`
重新生成位图。生成器按 1024 像素渲染后做 4×4 盒式下采样，输出 256 像素，
既保证边缘平滑，也避免 README 里的图片占满整行。

---

## 四、配置

`Config` 的零值会由 `normalize` 补齐，也可用 `Option` 覆盖：

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `Addr` | `127.0.0.1:8080` | 监听地址 |
| `DataDir` | `$TMPDIR/leego-harness` | 上传分片与运行期数据目录 |
| `SessionTTL` | 30 分钟 | 会话有效期 |
| `SessionSecret` | 演示密钥 | Cookie HMAC 密钥，生产必须替换 |
| `CacheTTL` / `CacheStale` | 5s / 15s | 缓存新鲜期与陈旧期 |
| `RateLimit` / `RateBurst` | 5 / 10 | 令牌桶速率与突发容量 |
| `MetricsInterval` | 2s | 指标采集间隔 |
| `ChunkSize` | 256 KiB | 上传默认分片大小 |
| `EnableLogging` | `true` | 访问日志（测试时建议关闭） |

```go
service, err := harness.NewService(
	harness.UseAddr("127.0.0.1:0"),
	harness.UseDataDir(t.TempDir()),
	harness.UseSessionTTL(10*time.Minute),
	harness.UseRateLimit(2, 2),
	harness.UseChunkSize(8),
	harness.UseLogging(false),
)
if err != nil {
	t.Fatal(err)
}
defer service.Shutdown(context.Background())
```

可用选项：`UseAddr`、`UseDataDir`、`UseSessionTTL`、`UseSessionSecret`、
`UseCacheTTL`、`UseRateLimit`、`UseMetricsInterval`、`UseChunkSize`、`UseLogging`。

---

## 五、作为库嵌入

`NewService` 只装配资源与路由，不监听端口；`Run` 才进入阻塞服务：

```go
// 嵌入测试
server := httptest.NewServer(service.GetHandler())
defer server.Close()

// 托管真实生命周期
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
if err := service.Run(ctx); err != nil {
	log.Printf("服务退出: %v", err)
}

// 二次扩展：继续注册业务路由
service.GetEngine().GET("/custom", handler)
```

`GetConfig()` 返回配置副本（密钥字段已深拷贝），修改副本不会影响运行中的服务。

---

## 六、分片上传协议

同一个处理器按查询参数分发，分片体直接读原始字节，不经过 multipart 解析：

| 动作 | 请求 | 说明 |
| --- | --- | --- |
| `POST ?action=init` | JSON `{name,size,part_size}` | 创建会话，返回会话对象（含 `id`、`parts`） |
| `PUT ?action=part&id=..&index=..` | 原始字节 | 分片可乱序、可重复（覆盖） |
| `POST ?action=complete&id=..` | 无 | 拼接分片并返回 `{path}` |
| `GET ?action=status&id=..` | — | 查询会话状态 |
| `DELETE ?action=abort&id=..` | — | 丢弃全部分片 |

拼接时计算 SHA-256，命中索引则复用已有文件（秒传）；秒传索引是追加式
`index.jsonl`（每行 `{sum,path}`），进程重启后仍可命中；会话元信息以 JSON 落盘，
重启后仍可续传。下载端只允许 `files/` 前缀且拒绝 `..`。

> 旧版部署的 `index.json` 不会被自动读取；升级后重建一次索引即可。

---

## 七、测试客户端

`harness/testutil` 提供基于 `httptest` 的轻量客户端，断言失败会直接 `t.Fatal`：

```go
import "gitee.com/leminewx/leego/harness/testutil"

client := testutil.NewClient(service.GetHandler()).
	UseHeader("X-Request-ID", "test-1")

response := client.SendJSON(http.MethodPost, "/api/session/login", map[string]any{"name": "demo"})
response.AssertStatus(t, http.StatusOK).AssertContains(t, "demo")

var payload map[string]any
response.BindJSON(t, &payload)

cookie, ok := response.GetCookie("leego_session")
```

`testutil.NewFrozenClock(at)` 返回固定时钟，便于测试 TTL、过期与限流窗口。

---

## 八、压测入口

```go
import stress "gitee.com/leminewx/leego/harness/stresstest"

config := stress.NewDefaultConfig("http://127.0.0.1:8080/api/hello")
config.Concurrent = 50
config.TotalRequests = 5000

client, err := stress.NewClient(config)
if err != nil {
	log.Fatal(err)
}
defer client.Close()

fmt.Println(stress.FormatReport(client.Run()))
```

配套基准服务：

```bash
go run ./harness/stresstest/test/base
```

详见 [stresstest.md](stresstest.md)。

---

## 九、生命周期

`Shutdown` 依次执行：

1. `http.Server.Shutdown`：停止接收新请求并等待在途请求；
2. 引擎关闭钩子（注册顺序的逆序）：关闭内部上游 → 刷新链路导出 →
   关闭会话存储 → 停止调度器并取消后台上下文。

因此 `Run` 返回后不会残留监听端口或后台协程。注册了自定义关闭钩子时，
应保证其幂等，避免重复 `Shutdown` 造成二次释放。

---

## 十、注意事项

- 默认会话密钥只用于本地演示，生产必须通过 `UseSessionSecret` 或配置中心注入；
- 数据目录会在启动时自动创建，测试中请使用 `t.TempDir()` 隔离；
- 内部上游使用 `127.0.0.1:0` 随机端口，随服务一起关闭；
- 前端不依赖 CDN，可离线使用；修改 `statics/index.html` 后需重新编译；
- 该服务定位是演示与联调，不是生产模板：鉴权、限流粒度、持久化与观测
  导出都需要按实际架构替换。