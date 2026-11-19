# 编码规范

本文定义 leego 的命名、注释、错误处理、并发与测试约定。它不是风格偏好清单，
而是**可检查规则**：新增代码必须满足，代码评审逐条核对。

命名规范历经一次调整，当前生效的口径是：

| 主题 | 现行口径 | 关键变化 |
| --- | --- | --- |
| 构造函数 | 统一 `New*` / `new*` | 不再使用 `Create*` 作为纯内存构造 |
| 上下文响应 | 统一 `Response*` 前缀 | 不再分散为 `Write*` / `Render*` / `Send*` / `Serve*` / `Stream*` |
| 常量 | 公有全大写下划线连接；私有 `_` 开头全大写 | 取代 camelCase 私有常量 |
| 词性判定 | 以词典词性为准，兼类词不算违规（如 `Group`） | 不再机械按首词表面词性判定 |

---

## 一、命名规范

### 1.1 总则

1. 函数与方法名描述「做什么」，首词必须是**动词、be 动词或情态动词**。
2. 构造函数是唯一允许使用固定前缀 `New` 的例外：`New` 在此语境下是约定俗成的构造谓词。
3. 不允许用名词、形容词、副词或介词开头，例如 `Path()`、`Allowed()`、`NextSegment()`、`WithPrefix()`。
4. 名称应表达**语义**而非**实现**：`countActiveConnections` 优于 `loopMap`。

### 1.2 构造函数：`New` / `new`

- 导出构造函数：`New<类型>[]`，例如 `NewEngine`、`NewContext`、`NewManager`、`NewParser`。
- 包内私有构造函数：`new<类型>[]`，例如 `newContext`、`newRouter`、`newStaticHandler`、`newRequest`。
- 带默认值的构造函数：`NewDefault<类型>`，例如 `NewDefaultEngineConfig`、`NewDefaultConfig`、`NewDefaultCORS`。
- 带可选变体的构造函数：`New<类型>With<特征>`，例如 `NewLatenciesWithCapacity`、`NewMessageWithEvent`。
- 对象克隆不走构造函数族：用 `Clone`（返回独立副本）或 `Copy`（写入已有目标）。

`New` 与 `Create` 的边界：

| 前缀 | 语义 | 是否产生外部副作用 | 示例 |
| --- | --- | --- | --- |
| `New` | 纯内存构造，失败只可能来自参数非法 | 否 | `NewEngine`、`NewClient` |
| `Create` | 创建**持久化或外部**资源 | 是（写库、建文件、建表、发起注册） | `CreateUser`、`CreateTable`、`CreateUpload` |

这条边界保证：读代码时看到 `NewXxx` 就知道「不会碰 IO」，看到 `CreateXxx` 就知道「可能失败且需要回滚或错误处理」。

### 1.3 上下文响应族：`Response*`

`Context` 上所有「写响应」的方法统一以 `Response` 开头。`Response` 在这里是
**响应动作的限定词**（对应 HTTP Response），与同包字段 `Context.Response`
（底层 `response.Writer`）在阅读上互补：字段是「写到哪儿」，方法是「写什么」。

| 方法 | Content-Type | 是否阻塞 | 说明 |
| --- | --- | --- | --- |
| `ResponseJSON(code, data)` | `application/json` | 否 | 编码失败返回错误，此时响应头已写出 |
| `ResponseXML(code, data)` | `application/xml` | 否 | 同上 |
| `ResponseHTML(code, html)` | `text/html` | 否 | 不转义，调用方保证安全 |
| `ResponseText(code, text)` | `text/plain` | 否 | 不追加换行，不记录错误链 |
| `ResponseBytes(code, data)` | 调用方决定 | 否 | 不设置 Content-Type |
| `ResponseError(code, message)` | `text/plain` | 否 | 与 `http.Error` 对齐，同时记录错误链 |
| `ResponseRedirect(code, location)` | — | 否 | 仅接受 3xx，其他状态码 panic |
| `ResponseTemplate(name, data)` | `text/html` | 否 | 使用引擎预加载模板 |
| `ResponseTemplateFile(file, name, data)` | `text/html` | 否 | 即时解析，不缓存 |
| `ResponseSSE(handler)` | `text/event-stream` | **是** | 阻塞到连接结束，通道关闭后补发 `close` 事件 |
| `ResponseFile(file)` | 由 `http.ServeFile` 推断 | 否 | inline，支持 Range 与缓存协商 |
| `ResponseFileFromFS(file, fs)` | 同上 | 否 | 临时改写 `URL.Path`，Context 不可并发复用 |
| `ResponseAttachment(file)` | 同上 | 否 | 追加 `Content-Disposition: attachment` |
| `ResponseAttachmentFromFS(file, fs)` | 同上 | 否 | 同上，从自定义文件系统读取 |

约束：**不要**新增 `WriteJSON`、`SendFile`、`StreamSSE` 这类同义前缀；新增响应能力一律并入
`Response*` 族，并在上表中补充一行。

### 1.4 动词表

下表按语义域分组。选取首词时先确定「这个函数对什么做了什么」，再查表；
表内没有合适动词时，优先使用同义的标准动词，而不是自造名词化写法。

**构造与复制**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `New` | 纯内存构造 | `NewEngine`、`NewNode` |
| `NewDefault` | 构造带安全默认值的实例 | `NewDefaultConfig` |
| `NewFrom` / `NewWith` | 从既有对象或附加特征构造 | `NewMessageWithEvent` |
| `Clone` | 深拷贝 | `EngineConfig.Clone` |
| `Copy` | 复制到已有目标 | `CopyTo` |
| `Restore` | 从快照恢复 | `RestoreSnapshot` |
| `Load` | 从外部载入到内存 | `LoadHTMLGlob` |
| `Open` | 打开资源句柄 | `OpenFile` |

**读取与查询**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Get` | 读取已有值（可能不存在） | `GetCookieValue`、`GetSnapshot` |
| `Fetch` | 跨边界取回（HTTP、DB） | `FetchRemoteConfig` |
| `Read` | 顺序读取流 | `ReadBody`、`ReadFrame` |
| `Lookup` | 按键查表 | `LookupHandler` |
| `Find` | 按条件搜索首个匹配 | `FindRoute` |
| `Search` | 搜索（可能多个结果） | `Search` |
| `Query` | 带条件的批量查询 | `QueryMetrics` |
| `Scan` | 遍历并读取 | `ScanRows` |
| `Peek` | 读但不消费 | `PeekFrame` |
| `Take` / `Pop` | 取出并移除 | `TakeSnapshot` |
| `Count` | 计数 | `CountConnections` |
| `Exists` / `Contains` | 存在性判断（bool） | `ContainsPath` |

**写入与修改**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Set` | 覆盖单个字段 | `SetStatusCode`、`SetPingInterval` |
| `Update` | 局部更新已有记录 | `UpdateConfig` |
| `Patch` | 打补丁式更新 | `PatchRoute` |
| `Replace` | 整体替换 | `ReplaceHandler` |
| `Apply` | 应用一组变更 | `ApplyOptions` |
| `Add` | 追加到集合 | `AddError`、`AddBlacklist` |
| `Append` | 尾部追加 | `AppendEvent` |
| `Insert` | 指定位置插入 | `InsertRoute` |
| `Remove` / `Delete` | 移除 | `RemoveFile`、`DeleteRoute` |
| `Clear` / `Reset` / `Purge` | 清空或复位 | `ResetMetrics`、`Clear` |
| `Store` / `Put` | 写入存储 | `StoreConfig` |
| `Save` / `Persist` / `Commit` | 落盘或提交事务 | `SaveReply` |
| `Flush` | 刷出缓冲 | `Flush` |

**判断与谓词**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Is` | 状态判断 | `IsPrepared`、`IsClosed`、`IsASCII` |
| `Has` | 拥有关系 | `HasPrefix`、`HasTag` |
| `Can` / `Should` / `Must` | 能力或约束判断 | `CanExecute`、`ShouldRetry` |
| `Match` | 模式匹配 | `MatchRoute` |
| `Equal` / `Compare` | 相等与比较 | `EqualConfig` |
| `Allow` / `Accept` / `Support` | 是否放行或支持 | `AllowOrigin`、`SupportH2C` |
| `Validate` / `Verify` / `Check` / `Ensure` | 校验 | `ValidateClaims`、`VerifyPassword` |
| `Authenticate` / `Authorize` | 认证与授权 | `AuthorizeRequest` |

**生命周期与调度**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Start` | 启动后台循环 | `Client.Start` |
| `Run` | 阻塞运行 | `Engine.Run` |
| `Serve` | 提供服务直到结束 | `Serve`、`ServeEventStream` |
| `Listen` / `Accept` | 监听与接受连接 | `ListenUnix` |
| `Wait` / `Await` | 等待条件成立 | `WaitSignalAndShutdown` |
| `Shutdown` / `Stop` / `Terminate` | 优雅或强制停止 | `Shutdown` |
| `Close` / `Dispose` | 释放资源（幂等） | `Close`、`CloseClient` |
| `Cancel` / `Abort` | 取消进行中的工作 | `CancelWork` |
| `Pause` / `Resume` / `Restart` | 暂停、恢复、重启 | `ResumeStream` |
| `Retry` / `Backoff` | 重试与退避 | `RetryRequest` |
| `Acquire` / `Release` / `Reserve` | 资源占用与归还 | `AcquireToken` |
| `Schedule` | 安排延迟执行 | `ScheduleFlush` |

**计算与转换**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Compute` / `Calculate` | 计算派生值 | `ComputePercentile` |
| `Sum` / `Aggregate` | 求和与聚合 | `AggregateMetrics` |
| `Merge` / `Concat` | 合并 | `MergeConfig` |
| `Split` / `Partition` / `Group` / `Bucket` | 拆分与分组 | `SplitNextSegment`、`GroupByTag` |
| `Filter` / `Map` / `Reduce` | 集合变换 | `FilterRoutes` |
| `Sort` / `Order` / `Reverse` / `Shuffle` | 排序与重排 | `SortRoutes` |
| `Normalize` / `Sanitize` | 规范化与清洗 | `NormalizePrefix`、`SanitizeName` |
| `Format` / `Render` | 格式化为文本 | `FormatLatency`、`FormatReport` |
| `Parse` / `Decode` / `Deserialize` / `Unmarshal` | 反序列化 | `ParsePrefix` |
| `Encode` / `Serialize` / `Marshal` | 序列化 | `EncodeMessage` |
| `Convert` / `Cast` / `Transform` | 类型或形态转换 | `ConvertToInt` |
| `Clamp` / `Round` / `Truncate` | 数值与长度收敛 | `ClampPercent` |
| `Hash` / `Sign` / `Encrypt` / `Decrypt` | 密码学运算 | `SignClaims`、`DecryptBody` |
| `Verify` | 验签或校验 | `VerifySignature` |

**输出与传输**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Response` | 写 HTTP 响应 | `ResponseJSON`、`ResponseFile` |
| `Write` | 写入实现了 `io.Writer` 的目标 | `WriteMessage`、`writeCloseFrame` |
| `Send` / `Push` / `Emit` / `Publish` | 主动投递 | `SendToClient`、`EmitEvent` |
| `Broadcast` | 一对多投递 | `Broadcast` |
| `Notify` / `Reply` / `Forward` | 通知、应答、转发 | `NotifyShutdown` |
| `Dispatch` | 分发到处理器 | `dispatch` |
| `Stream` / `Pipe` | 流式输出 | `StreamEvents` |
| `Redirect` | 重定向 | `ResponseRedirect` |
| `Export` / `Dump` | 导出为外部格式 | `ExportCSV` |

**采集、度量与诊断**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Collect` / `Record` / `Track` | 采集与记录 | `CollectReport`、`record` |
| `Measure` / `Observe` | 度量耗时或分布 | `MeasureLatency` |
| `Report` | 生成报告 | `NewReport` |
| `Snapshot` | 生成快照 | `GetSnapshot` |
| `Log` / `Print` / `Trace` / `Warn` / `Debug` | 日志输出 | `LogInfo`、`LogWarn` |

**包装与注册**

| 动词 | 语义 | 示例 |
| --- | --- | --- |
| `Wrap` / `Unwrap` | 包装与解包 | `WrapFromHTTPHandlerFunc`、`Unwrap` |
| `Bind` | 绑定参数或模板 | `BindJSON`、`BindForm` |
| `Attach` / `Detach` | 挂载与摘除 | `AttachMiddleware` |
| `Mount` | 挂载子树或目录 | `MountStatic` |
| `Register` / `Unregister` | 注册与注销 | `RegisterDashboard` |
| `Subscribe` / `Unsubscribe` | 订阅与退订 | `SubscribeTopic` |
| `Use` | 启用中间件或选项 | `UseMiddlewares`、`UseH2C` |
| `Enable` / `Disable` / `Toggle` | 开关 | `Enable`、`DisableStartupLog` |
| `Lock` / `Unlock` / `Freeze` / `Unfreeze` | 加解锁或冻结 | `Freeze` |
| `Mark` / `Unmark` | 打标记 | `MarkPrepared` |
| `Upgrade` / `Handshake` / `Dial` / `Connect` / `Disconnect` | 协议与连接动作 | `Upgrade` |

动词表中的词条可以组合，例如 `TryAcquireToken`、`MustParsePrefix`、
`ShouldRetryRequest`；组合后仍须保证首词是动词或情态动词。

### 1.5 词性判定与兼类词

判定只看**首词的词性**，不看整词是否像名词：

| 词 | 词性 | 结论 |
| --- | --- | --- |
| `Group` | v. 把……分组；n. 组 | 动词可用：`GroupByTag`、`NewGroup` 均合法 |
| `Stream` | v. 流式传输；n. 流 | 动词可用：`StreamEvents` |
| `Broadcast` | v. 广播；n. 广播节目 | 动词可用：`Broadcast` |
| `Cache` | v. 缓存；n. 缓存 | 动词可用：`CacheResponse` |
| `Benchmark` | v. 基准测试；n. 基准 | 动词可用：`Benchmark` |
| `Path` | n. 路径 | 仅名词，不可作首词：用 `GetPath`、`joinPath` |
| `Allowed` | adj. 被允许的 | 形容词，不可作首词：用 `IsAllowed` |

注意：`NewGroup` 中的 `Group` 虽可作动词，但**整个名称属于构造函数族**，
因此不会与 `GroupByTag` 这类真正的动词用法冲突。

### 1.6 常量命名

| 范围 | 规则 | 示例 |
| --- | --- | --- |
| 导出常量 | 全大写 + 下划线连接 | `MSG_TYPE_PING`、`STATE_OPEN`、`CLOSE_CODE_NORMAL_CLOSURE`、`CONTENT_TYPE` |
| 包内私有常量 | `_` 开头 + 全大写 + 下划线连接 | `_DEFAULT_TIMEOUT`、`_MAX_TAG_COUNT`、`_MSG_CONNECTED` |

约定细节：

1. 下划线按**词边界**切分：`_DEFAULT_READ_HEADER_TIMEOUT`，不写 `_DEFAULTREADHEADERTIMEOUT`。
2. 常量名与注释首词保持一致，注释以常量名开头，便于 godoc 检索。
3. 枚举常量统一前缀：`MSG_TYPE_*`、`STATE_*`、`CLOSE_CODE_*`、`OP_CODE_*`，前缀即分组名。
4. 导出错误是 `var` 而非 `const`，沿用 Go 生态约定 `ErrXxx`（如 `ErrManagerIsClosed`），**不受**全大写规则约束。
5. 路由模板、周期性参数等**取值不变**的字符串才定义常量；可配置项走 `Config` 字段，避免常量与配置双份真值。

已知例外（保持行业固定写法，不做下划线化）：

- JWT 算法名 `HS256`、`HS384`、`HS512`：算法标识符是协议契约，`HS_256` 会与规范文本不一致。
- `WILDCARD_DEFAULT_NAME` 等已符合规则的常量无需调整。

权衡说明：Go 官方风格指南把下划线视作反模式（`ST1003`），因此本项目的
`_` 前缀私有常量属于**项目内约定**，目的是在阅读时一眼区分「包内常量」与
「局部变量」。若启用 `staticcheck`，需要在配置中排除该规则；同时**严禁**
用 `_` 开头命名导出标识符（会造成“看似私有、实则导出”的误导）。

### 1.7 接口方法属于强制例外

以下方法名由接口契约决定，不得为了满足「动词开头」而改名：

- `http.ResponseWriter`：`Header`、`Write`、`WriteHeader`
- `http.Flusher`：`Flush`；`http.Hijacker`：`Hijack`
- `net.Conn`：`Read`、`Write`、`Close`、`LocalAddr`、`RemoteAddr`、`SetDeadline` 系列
- `error`：`Error`；`fmt.Stringer`：`String`
- `http.Handler`：`ServeHTTP`
- `context.Context`：`Done`、`Err`、`Value`、`Deadline`
- 类型转换接口：`Unwrap`

`GET`、`POST`、`DELETE` 等**HTTP 方法注册函数**本身即动词，保留为标准方法名。

### 1.8 类型、字段与测试命名

- 类型用名词短语：`SlidingWindowLimiter`、`ClientSnapshot`、`MetricsSnapshot`。
- 接口名表达能力或角色：`Group`、`Router`、`Tree`、`Signer`、`HandleFunc`。
- 字段名用名词；可变字段必须标注由哪把锁保护（见 4.2）。
- 测试函数：`Test<动作><场景>`，如 `TestManagerServeSendsHeartbeatPing`；基准：`Benchmark<对象>`。
- 测试辅助函数保持 `createXxx` / `buildXxx` / `serveXxx`，**不**套用构造函数的 `New` 规则：
  它们不属于公开 API，且带 `TestingT` 或 `testing.TB` 参数，语义是「搭测试夹具」。

---

## 二、注释规范

### 2.1 必须写注释的位置

- **包注释**：每包一处，说明职责、支持范围与**不支持范围**。
- **导出类型**：语义、并发安全性与生命周期。
- **导出字段**：取值来源或副作用。
- **导出函数与方法**：做什么、参数为 nil 时的行为、错误含义、是否阻塞。
- **并发与安全边界**：哪把锁保护什么、哪些调用不能并发。
- **设计取舍**：为什么这样实现（如「不复用 reader 会丢首帧」）。

### 2.2 写法要求

```go
/*
Manager 管理全部 WebSocket 连接。

Manager 是并发安全的，可被多个 HTTP 处理协程同时使用；
创建后即可直接 Serve，无需额外的 Start。
*/
```

- 多行注释用 `/* ... */`；单行用 `// 动词短语。`
- 注释写「做什么」与「为什么」，不复述代码字面；以句号结尾。
- 改名必须同步改注释与测试名，宁可删除过时注释，也不保留错误描述。

### 2.3 响应方法的注释模板

每个 `Response*` 方法至少写清三件事：Content-Type、是否阻塞、失败后的状态。

```go
// ResponseJSON 输出 JSON 响应。
//
// Content-Type 固定为 application/json; charset=utf-8。编码失败时返回错误，
// 但此时状态码与响应头已经写出，调用方不应再次写入响应。
```

---

## 三、错误处理

### 3.1 哨兵错误 + `errors.Is`

```go
var (
    ErrClientIsClosed   = errors.New("leego: websocket: client is closed")
    ErrSendBufferIsFull = errors.New("leego: websocket: client send channel is full")
)
```

- 错误文本统一前缀 `leego: <子包>:`，便于日志检索。
- 需要携带上下文时用 `fmt.Errorf("...: %w", err)` 包装，保留错误链。
- 导出错误必须注释触发条件；未导出错误可在 `var` 块内注释。

### 3.2 不 panic 的边界

框架代码只在**编译期断言**与**不可恢复的误用**处 panic；请求处理路径一律返回错误。
已设置的 recover 边界：引擎级 `Recovery` 中间件、WebSocket 读/写协程。

已知的主动 panic：
- `Context.ResponseRedirect` 传入非 3xx 状态码；
- 注册期结束后继续注册路由、追加中间件；
- `Group.SetStaticFS` 传入 nil 文件系统。

### 3.3 快速失败与静默丢弃

投递型 API（`WriteMessage`、`Broadcast`）采用**非阻塞快速失败**：

- 队列满返回 `ErrSendBufferIsFull`，由调用方决定丢弃或断开；
- 内部转发失败时静默丢弃，避免日志风暴拖慢每一条连接。

---

## 四、并发约定

### 4.1 通道

- **谁创建谁关闭**；业务通道永不关闭，只关闭信号通道。
- 广播与定向发送只做非阻塞 `select`，不为慢客户端阻塞全局。
- 关闭使用 `sync.Once` 保证幂等，并采用「先发信号、后关连接」的顺序。

### 4.2 锁与原子

- 锁的粒度按字段划分：客户端状态用 `stateMu sync.RWMutex`，连接写操作用 `writeLock sync.Mutex`，二者互不嵌套。
- 只读布尔量与计数器用 `atomic.Bool` / `atomic.Int64`，避免读路径加锁。
- 快照型 API（`GetSnapshot`、`GetClients`）返回**副本**。

### 4.3 生命周期

- 长生命周期对象通过 `Start(ctx)` 绑定上下文；`ctx.Done()` 触发 `Close`。
- `Close` 必须幂等且可重复调用；`Done()` 返回只读通道供外部等待。
- 禁止在 `Close` 中关闭通道后再向该通道发送数据。

### 4.4 池化

- `Context` 与 `Writer` 使用 `sync.Pool` 复用；
- 归还前必须 `Reset` 清空引用，防止请求数据泄漏到下一个请求；
- 业务代码不得跨请求保存池化对象。

---

## 五、API 命名迁移对照

从 v0.10.x 升级到当前版本时的完整改名清单：

### 5.1 构造函数

| 旧名称 | 新名称 |
| --- | --- |
| `engine.Group(prefix)` | `engine.NewGroup(prefix)` |
| `leego.NewEngine(...)` | `leego.NewEngine(...)`（保持） |
| `leego.CreateEngine(...)` | `leego.NewEngine(...)` |
| `leego.CreateContext(...)` | `leego.NewContext(...)` |
| `leego.CreateHTTPError(...)` | `leego.NewHTTPError(...)` |
| `response.CreateWriter(...)` | `response.NewWriter(...)` |
| `response.CreateWriterPool()` | `response.NewWriterPool()` |
| `sse.CreateClient(...)` / `websocket.CreateClient(...)` | `NewClient(...)` |
| `sse.CreateManager()` / `websocket.CreateManager(...)` | `NewManager(...)` |
| `upgrader.CreateUpgrader(...)` | `upgrader.NewUpgrader(...)` |
| `limiter.CreateFixedWindowLimiter(...)` 等同族 | `NewFixedWindowLimiter(...)` |
| `breaker.CreateCircuitBreaker(...)` | `breaker.NewCircuitBreaker(...)` |
| `collector.CreateCollector(...)` | `collector.NewCollector(...)` |
| `tree.CreateNode(...)` / `tree.CreateTrie()` | `tree.NewNode(...)` / `tree.NewTrie()` |
| `stresstest.CreateClient(...)` | `stresstest.NewClient(...)` |
| `dashboard.CreateDashboard(...)` | `dashboard.NewDashboard(...)` |
| 各包 `CreateDefaultConfig()` | `NewDefaultConfig()` |

### 5.2 上下文响应

| 旧名称 | 新名称 |
| --- | --- |
| `ctx.WriteJSON(code, data)` | `ctx.ResponseJSON(code, data)` |
| `ctx.WriteXML(code, data)` | `ctx.ResponseXML(code, data)` |
| `ctx.WriteHTML(code, html)` | `ctx.ResponseHTML(code, html)` |
| `ctx.WritePlain(code, text)` | `ctx.ResponseText(code, text)` |
| `ctx.WriteBytes(code, data)` | `ctx.ResponseBytes(code, data)` |
| `ctx.WriteError(code, msg)` | `ctx.ResponseError(code, msg)` |
| `ctx.RenderHTML(name, data)` | `ctx.ResponseTemplate(name, data)` |
| `ctx.RenderHTMLFile(file, name, data)` | `ctx.ResponseTemplateFile(file, name, data)` |
| `ctx.StreamSSE(handler)` | `ctx.ResponseSSE(handler)` |
| `ctx.ServeFileContent(file)` | `ctx.ResponseFile(file)` |
| `ctx.ServeFileContentFromFS(file, fs)` | `ctx.ResponseFileFromFS(file, fs)` |
| `ctx.SendFileAttachment(file)` | `ctx.ResponseAttachment(file)` |
| `ctx.SendFileAttachmentFromFS(file, fs)` | `ctx.ResponseAttachmentFromFS(file, fs)` |
| `ctx.RedirectTo(code, location)` | `ctx.ResponseRedirect(code, location)` |

### 5.3 常量

| 旧名称 | 新名称 |
| --- | --- |
| 私有 `defaultXxx` | `_DEFAULT_XXX` |
| 私有 `maxXxx` | `_MAX_XXX` |
| 私有 `routeXxx` | `_ROUTE_XXX` |
| 私有 `prefixInfo` / `prefixWarn` | `_PREFIX_INFO` / `_PREFIX_WARN` |
| 私有 `errFormatXxx` | `_ERR_FORMAT_XXX` |
| 私有 `bucketCount` | `_BUCKET_COUNT` |
| 导出 `MSG_TYPE_*` / `STATE_*` / `CLOSE_CODE_*` | 保持不变 |

迁移建议：先全局替换调用点，再跑 `go build ./...` 与 `go vet ./...`；
若使用接口断言，注意同步修改自定义实现的方法名。

---

## 六、代码风格与检查

### 6.1 工具链

```bash
gofmt -l .        # 必须为空
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./...
```

### 6.2 文件与导入

- 仓库统一使用 **LF** 换行；Windows 下禁止用会写入 CRLF 的工具批量改写源文件。
- 导入顺序：标准库 → 第三方 → 本仓库，三组之间空行分隔。
- 一个文件只放一组高内聚类型；实现与测试文件同名成对出现。

### 6.3 提交前检查清单

1. 构造函数是否使用 `New*` / `new*`，且没有引入 `Create*` 内存构造？
2. `Context` 新增的响应方法是否命名为 `Response*`，并在 1.3 的表格中补充？
3. 常量是否满足：导出全大写下划线、私有 `_` 开头全大写？
4. 新增导出函数首词是否为动词 / be / 情态动词（接口实现除外）？
5. 兼类词（`Group`、`Stream`、`Broadcast`、`Cache`）是否已确认词性？
6. 导出类型、字段、函数是否都有中文注释并说明取舍？
7. 新增错误是否为哨兵错误并带 `leego:` 前缀？
8. 并发代码是否说明了锁保护范围，通道是否遵守「永不关闭发送队列」？
9. 是否补充了单元测试、并发测试，必要时补充基准？
10. `gofmt -l .`、`go vet ./...`、`go test ./... -count=1`、`go test -race ./...` 是否全部通过？
11. 若修改了对外契约（名称、字段、消息类型、状态码），是否同步更新 `RELEASE.md` 与 `docs/`？