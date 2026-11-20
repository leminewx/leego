package harness

import (
	"fmt"
	"time"

	"gitee.com/leminewx/leego/job"
	"gitee.com/leminewx/leego/openapi"
	"gitee.com/leminewx/leego/upload"
)

// _HARNESS_VERSION 为体验服务对外声明的版本号。
const _HARNESS_VERSION = "1.0.0"

// 业务分组名称：既作为 OpenAPI 顶层 tags，也作为路由的第二个标签。
const (
	// _TAG_OVERVIEW 为概览分组。
	_TAG_OVERVIEW = "概览"
	// _TAG_SESSION 为会话分组。
	_TAG_SESSION = "会话"
	// _TAG_CACHE 为缓存分组。
	_TAG_CACHE = "缓存"
	// _TAG_LIMITER 为限流分组。
	_TAG_LIMITER = "限流"
	// _TAG_UPLOAD 为分片上传分组。
	_TAG_UPLOAD = "分片上传"
	// _TAG_GATEWAY 为反向代理分组。
	_TAG_GATEWAY = "反向代理"
	// _TAG_STREAM 为实时推送分组。
	_TAG_STREAM = "实时推送"
	// _TAG_JOBS 为定时任务分组。
	_TAG_JOBS = "定时任务"
	// _TAG_TRACE 为链路追踪分组。
	_TAG_TRACE = "链路追踪"
	// _TAG_PLATFORM 为平台页面分组。
	_TAG_PLATFORM = "平台"
)

// overviewConfig 为概览响应中的运行参数。
type overviewConfig struct {
	Addr       string `json:"addr"`
	DataDir    string `json:"data_dir"`
	CacheTTL   string `json:"cache_ttl"`
	CacheStale string `json:"cache_stale"`
	RateLimit  int    `json:"rate_limit"`
	RateBurst  int    `json:"rate_burst"`
	ChunkSize  int64  `json:"chunk_size"`
}

// overviewResponse 描述 /api/hello 的响应。
type overviewResponse struct {
	Service   string            `json:"service"`
	Version   string            `json:"version"`
	Go        string            `json:"go"`
	StartedAt time.Time         `json:"started_at"`
	Uptime    string            `json:"uptime"`
	Config    overviewConfig    `json:"config"`
	Links     map[string]string `json:"links"`
}

// healthResponse 描述 /api/healthz 的响应。
type healthResponse struct {
	Status string    `json:"status"`
	Uptime string    `json:"uptime"`
	Jobs   job.Stats `json:"jobs"`
}

// echoResponse 描述 /api/echo 的响应。
type echoResponse struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Query    map[string]string `json:"query"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body"`
	ClientIP string            `json:"client_ip"`
	Trace    string            `json:"trace"`
}

// sessionStateResponse 描述 /api/session/current 的响应。
type sessionStateResponse struct {
	ID      string   `json:"id"`
	New     bool     `json:"new"`
	Name    any      `json:"name"`
	LoginAt any      `json:"login_at"`
	Keys    []string `json:"keys"`
}

// sessionLoginResponse 描述 /api/session/login 的响应。
type sessionLoginResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	LoginAt string `json:"login_at"`
}

// cacheResponse 描述 /api/cache/timestamp 的响应。
type cacheResponse struct {
	Value     string `json:"value"`
	CostMS    int64  `json:"cost_ms"`
	CacheHint string `json:"cache_hint"`
}

// limiterResponse 描述 /api/limited/ping 的响应。
type limiterResponse struct {
	OK    bool   `json:"ok"`
	Limit int    `json:"limit"`
	Burst int    `json:"burst"`
	At    string `json:"at"`
}

// uploadPartResponse 描述单个分片的上传结果。
type uploadPartResponse struct {
	Index int   `json:"index"`
	Size  int64 `json:"size"`
}

// uploadCompleteResponse 描述分片拼接结果。
type uploadCompleteResponse struct {
	Path string `json:"path"`
}

// okResponse 描述无附加数据的成功响应。
type okResponse struct {
	OK bool `json:"ok"`
}

// errorResponse 描述统一错误响应。
type errorResponse struct {
	Error string `json:"error"`
}

// gatewayResponse 描述内部上游的回显内容。
type gatewayResponse struct {
	Upstream string `json:"upstream"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Query    string `json:"query"`
	Host     string `json:"host"`
	Tag      string `json:"tag"`
	At       string `json:"at"`
}

// jobResponse 描述 /api/jobs 的响应。
type jobResponse struct {
	Ticks      int64     `json:"ticks"`
	Workers    int       `json:"workers"`
	Stats      job.Stats `json:"stats"`
	Uptime     string    `json:"uptime"`
	Interval   string    `json:"interval"`
	Registered int       `json:"registered"`
}

// traceItem 为单条链路片段。
type traceItem struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	Name       string            `json:"name"`
	Status     int               `json:"status"`
	DurationMS float64           `json:"duration_ms"`
	At         time.Time         `json:"at"`
	Tags       map[string]string `json:"tags"`
}

// traceResponse 描述 /api/traces 的响应。
type traceResponse struct {
	Total int         `json:"total"`
	Items []traceItem `json:"items"`
}

// registerOpenAPI 注册结构化 OpenAPI 文档。
//
// 必须在其它能力注册完成之后调用：文档会读取已注册路由，
// 再由声明式覆盖表补充中文摘要、参数说明、请求体与多状态码响应。
func (own *Service) registerOpenAPI() error {
	options := make([]openapi.Option, 0, 8+len(openAPITagOptions()))
	options = append(
		options,
		openapi.UseTitle("leego harness"),
		openapi.UseVersion(_HARNESS_VERSION),
		openapi.UseDescription(
			"leego 零依赖扩展能力的开发者体验服务。\n\n"+
				"所有接口都是真实可调的：请求会被记录为指标与链路片段，"+
				"可分别在 /metrics 与 /dashboard 中查看。",
		),
		openapi.UseLicense("MIT", ""),
		openapi.UseServer("http://"+own.config.Addr, "本地实例"),
		openapi.UseServer("/", "与文档同源"),
		openapi.UseSchemas(openAPISchemas()),
		openapi.UseOperations(openAPIOperations()),
	)
	options = append(options, openAPITagOptions()...)

	if err := own.engine.RegisterOpenAPI("/openapi", options...); err != nil {
		return fmt.Errorf("harness: register openapi: %w", err)
	}

	return nil
}

// openAPITagOptions 返回分组说明，顺序即浏览页的展示顺序。
func openAPITagOptions() []openapi.Option {
	return []openapi.Option{
		openapi.UseTag(_TAG_OVERVIEW, "服务概览、健康检查与请求回显"),
		openapi.UseTag(_TAG_SESSION, "基于 HMAC 签名的服务端会话，登录时轮换会话 ID"),
		openapi.UseTag(_TAG_CACHE, "响应缓存：新鲜期内直接命中，陈旧期内可异步回源"),
		openapi.UseTag(_TAG_LIMITER, "令牌桶限流：额度不足立即返回 429"),
		openapi.UseTag(_TAG_UPLOAD, "分片上传：创建会话、逐片写入、拼接与下载"),
		openapi.UseTag(_TAG_GATEWAY, "加权轮询与会话粘滞的反向代理，支持灰度标签"),
		openapi.UseTag(_TAG_STREAM, "基于 SSE 的运行状态推送"),
		openapi.UseTag(_TAG_JOBS, "进程内任务调度器的执行统计"),
		openapi.UseTag(_TAG_TRACE, "最近导出的链路片段，用于观察耗时分布"),
		openapi.UseTag(_TAG_PLATFORM, "由框架组件自行注册的页面与导出端点"),
	}
}

// openAPISchemas 返回命名模式表，供响应体以 $ref 复用。
func openAPISchemas() map[string]any {
	return map[string]any{
		"HarnessOverview": overviewResponse{},
		"HarnessHealth":   healthResponse{},
		"HarnessEcho":     echoResponse{},
		"SessionState":    sessionStateResponse{},
		"SessionLogin":    sessionLoginResponse{},
		"CacheStamp":      cacheResponse{},
		"LimiterPing":     limiterResponse{},
		"UploadSession":   upload.Session{},
		"UploadInit":      upload.InitRequest{},
		"UploadPart":      uploadPartResponse{},
		"UploadComplete":  uploadCompleteResponse{},
		"GatewayEcho":     gatewayResponse{},
		"JobSnapshot":     jobResponse{},
		"TraceFeed":       traceResponse{},
		"HarnessOK":       okResponse{},
		"HarnessError":    errorResponse{},
	}
}

// openAPIOperations 返回声明式操作表。
//
// 键必须与已注册路由的"方法 + 模式"一致；路由改名后对应声明会被忽略，
// 避免文档中出现不存在的接口。
func openAPIOperations() map[string]openapi.Operation {
	return map[string]openapi.Operation{
		"GET /": {
			Summary:     "体验首页",
			Description: "返回内嵌的单页控制台，左侧菜单按能力分组。",
			Tags:        []string{_TAG_OVERVIEW},
			Responses: map[string]openapi.Response{
				"200": {
					Description: "HTML 页面",
					Content: map[string]openapi.MediaType{
						"text/html": {Schema: map[string]any{"type": "string"}},
					},
				},
			},
		},
		"GET /api/hello": {
			Summary:     "服务概览",
			Description: "返回服务名、Go 版本、启动时间、运行参数与平台入口。",
			Tags:        []string{_TAG_OVERVIEW},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("服务概览", "HarnessOverview"),
			},
		},
		"GET /api/healthz": {
			Summary:     "健康检查",
			Description: "返回状态、运行时长与调度器统计，可直接用于探针。",
			Tags:        []string{_TAG_OVERVIEW},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("健康状态", "HarnessHealth"),
			},
		},
		"GET /api/echo": {
			Summary:     "回显请求（GET）",
			Description: "回显方法、路径、查询参数、请求头、请求体、客户端地址与链路 ID。",
			Tags:        []string{_TAG_OVERVIEW},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("q", openapi.IN_QUERY, "任意查询参数，会在响应 query 中原样返回", false, nil),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("回显结果", "HarnessEcho"),
			},
		},
		"POST /api/echo": {
			Summary:     "回显请求（POST）",
			Description: "请求体会以 UTF-8 文本形式出现在响应的 body 字段。",
			Tags:        []string{_TAG_OVERVIEW},
			RequestBody: openapi.NewJSONRequestBody(false, map[string]any{"message": "hello"}),
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("回显结果", "HarnessEcho"),
				"400": openapi.NewJSONRefResponse("请求体读取失败", "HarnessError"),
			},
		},
		"GET /api/session/current": {
			Summary:     "读取当前会话",
			Description: "未登录时返回 new=true 的空会话，不会写入存储。",
			Tags:        []string{_TAG_SESSION},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("会话内容", "SessionState"),
				"500": openapi.NewJSONRefResponse("会话中间件缺失", "HarnessError"),
			},
		},
		"POST /api/session/login": {
			Summary:     "登录并轮换会话 ID",
			Description: "先轮换会话 ID 再写入 name，避免会话固定攻击。",
			Tags:        []string{_TAG_SESSION},
			RequestBody: openapi.NewJSONRequestBody(true, map[string]any{"name": "leego"}),
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("登录结果", "SessionLogin"),
				"400": openapi.NewJSONRefResponse("名称缺失或 JSON 非法", "HarnessError"),
				"500": openapi.NewJSONRefResponse("会话中间件缺失", "HarnessError"),
			},
		},
		"POST /api/session/logout": {
			Summary:     "销毁会话",
			Description: "清空服务端会话并让浏览器删除会话 Cookie。",
			Tags:        []string{_TAG_SESSION},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("已销毁", "HarnessOK"),
				"500": openapi.NewJSONRefResponse("会话中间件缺失", "HarnessError"),
			},
		},
		"GET /api/cache/timestamp": {
			Summary:     "缓存时间戳",
			Description: "模拟 10~39 毫秒的昂贵计算；命中缓存时不会出现这段延迟，响应头 X-Cache 标记 HIT。",
			Tags:        []string{_TAG_CACHE},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("时间戳与本次耗时", "CacheStamp"),
			},
		},
		"GET /api/limited/ping": {
			Summary:     "限流探测",
			Description: "令牌桶非阻塞判定：连续点击即可观察到 429，令牌按配置速率恢复。",
			Tags:        []string{_TAG_LIMITER},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("放行", "LimiterPing"),
				"429": openapi.NewJSONRefResponse("额度不足", "HarnessError"),
			},
		},
		"POST /api/upload": {
			Summary: "创建或完成上传会话",
			Description: "按查询参数 action 分发：\n\n" +
				"- action=init：请求体为 {name,size,part_size}，返回分片会话；\n" +
				"- action=complete：按 id 拼接全部分片，返回最终文件路径。",
			Tags: []string{_TAG_UPLOAD},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("action", openapi.IN_QUERY, "init 或 complete", true, map[string]any{"type": "string", "enum": []string{"init", "complete"}}),
				openapi.NewParameter("id", openapi.IN_QUERY, "上传会话标识，action=complete 时必填", false, nil),
			},
			RequestBody: openapi.NewJSONRequestBody(true, upload.InitRequest{Name: "demo.bin", Size: 1 << 20, PartSize: 256 << 10}),
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("上传会话；complete 动作返回 UploadComplete", "UploadSession"),
				"400": openapi.NewJSONRefResponse("参数非法", "HarnessError"),
				"404": openapi.NewJSONRefResponse("会话不存在", "HarnessError"),
				"409": openapi.NewJSONRefResponse("分片不完整", "HarnessError"),
				"413": openapi.NewJSONRefResponse("超出单文件上限", "HarnessError"),
			},
		},
		"PUT /api/upload": {
			Summary:     "上传一个分片",
			Description: "请求体为原始字节，不经过 multipart 解析。分片序号从 0 开始，除最后一片外长度必须等于 part_size。",
			Tags:        []string{_TAG_UPLOAD},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("action", openapi.IN_QUERY, "固定为 part", true, map[string]any{"type": "string", "enum": []string{"part"}}),
				openapi.NewParameter("id", openapi.IN_QUERY, "上传会话标识", true, nil),
				openapi.NewParameter("index", openapi.IN_QUERY, "分片序号，从 0 开始", true, map[string]any{"type": "integer"}),
			},
			RequestBody: &openapi.RequestBody{
				Description: "分片字节",
				Required:    true,
				Content: map[string]openapi.MediaType{
					openapi.MEDIA_OCTET_STREAM: {Schema: map[string]any{"type": "string", "format": "binary"}},
				},
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("写入结果", "UploadPart"),
				"400": openapi.NewJSONRefResponse("序号或长度非法", "HarnessError"),
				"404": openapi.NewJSONRefResponse("会话不存在", "HarnessError"),
			},
		},
		"GET /api/upload": {
			Summary:     "查询上传状态",
			Description: "返回会话信息与每个分片是否已写入。",
			Tags:        []string{_TAG_UPLOAD},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("action", openapi.IN_QUERY, "固定为 status", true, map[string]any{"type": "string", "enum": []string{"status"}}),
				openapi.NewParameter("id", openapi.IN_QUERY, "上传会话标识", true, nil),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("会话状态", "UploadSession"),
				"404": openapi.NewJSONRefResponse("会话不存在", "HarnessError"),
			},
		},
		"DELETE /api/upload": {
			Summary:     "放弃上传",
			Description: "删除会话与全部分片。",
			Tags:        []string{_TAG_UPLOAD},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("action", openapi.IN_QUERY, "固定为 abort", true, map[string]any{"type": "string", "enum": []string{"abort"}}),
				openapi.NewParameter("id", openapi.IN_QUERY, "上传会话标识", true, nil),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("已放弃", "HarnessOK"),
				"404": openapi.NewJSONRefResponse("会话不存在", "HarnessError"),
			},
		},
		"GET /api/upload/download/*filepath": {
			Summary:     "下载已合并文件",
			Description: "只允许访问 files/ 下的最终文件，路径包含 .. 时直接拒绝。",
			Tags:        []string{_TAG_UPLOAD},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("filepath", openapi.IN_PATH, "以 files/ 开头的最终文件路径", true, nil),
			},
			Responses: map[string]openapi.Response{
				"200": {
					Description: "文件字节流",
					Content: map[string]openapi.MediaType{
						openapi.MEDIA_OCTET_STREAM: {Schema: map[string]any{"type": "string", "format": "binary"}},
					},
				},
				"400": openapi.NewJSONRefResponse("路径非法", "HarnessError"),
				"404": openapi.NewJSONRefResponse("文件不存在", "HarnessError"),
			},
		},
		"GET /api/gateway/*path": {
			Summary:     "反向代理（GET）",
			Description: "按权重选择上游（stable:3、canary:1）；请求头 X-Leego-Canary 为 v2 时优先命中 canary。",
			Tags:        []string{_TAG_GATEWAY},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("path", openapi.IN_PATH, "转发到上游的路径", true, nil),
				openapi.NewParameter("X-Leego-Canary", openapi.IN_HEADER, "灰度标签，取值 v2", false, nil),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("上游回显", "GatewayEcho"),
				"502": openapi.NewJSONRefResponse("上游不可用", "HarnessError"),
			},
		},
		"POST /api/gateway/*path": {
			Summary:     "反向代理（POST）",
			Description: "请求体与请求头原样转发到选中的上游。",
			Tags:        []string{_TAG_GATEWAY},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("path", openapi.IN_PATH, "转发到上游的路径", true, nil),
				openapi.NewParameter("X-Leego-Canary", openapi.IN_HEADER, "灰度标签，取值 v2", false, nil),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("上游回显", "GatewayEcho"),
				"502": openapi.NewJSONRefResponse("上游不可用", "HarnessError"),
			},
		},
		"GET /api/events": {
			Summary:     "实时状态推送",
			Description: "每秒推送一次 tick 事件，首个事件为 hello；断开连接即回收协程。",
			Tags:        []string{_TAG_STREAM},
			Responses: map[string]openapi.Response{
				"200": {
					Description: "SSE 事件流，事件名 hello 与 tick，数据为 JSON",
					Content: map[string]openapi.MediaType{
						openapi.MEDIA_EVENT_STREAM: {Schema: map[string]any{"type": "string"}},
					},
				},
			},
		},
		"GET /api/jobs": {
			Summary:     "定时任务统计",
			Description: "内置任务每 3 秒执行一次，返回成功计数与调度器统计。",
			Tags:        []string{_TAG_JOBS},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("任务统计", "JobSnapshot"),
			},
		},
		"GET /api/traces": {
			Summary:     "最近的链路片段",
			Description: "返回最近至多 20 条片段，按时间倒序。",
			Tags:        []string{_TAG_TRACE},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("链路列表", "TraceFeed"),
			},
		},
		"GET /dashboard": {
			Summary:     "分析后台",
			Description: "路由列表、指标趋势与重置操作的运维页面。",
			Tags:        []string{_TAG_PLATFORM},
			Responses:   map[string]openapi.Response{"200": {Description: "HTML 页面"}},
		},
		"GET /dashboard/*filepath": {
			Summary:     "分析后台静态资源",
			Description: "分析后台引用的脚本与样式，由组件内嵌提供。",
			Tags:        []string{_TAG_PLATFORM},
			Responses:   map[string]openapi.Response{"200": {Description: "静态资源字节流"}},
		},
		"HEAD /dashboard": {
			Summary:     "分析后台（HEAD）",
			Description: "与 GET 一致，仅返回响应头。",
			Tags:        []string{_TAG_PLATFORM},
			Responses:   map[string]openapi.Response{"200": {Description: "仅响应头"}},
		},
		"HEAD /dashboard/*filepath": {
			Summary:     "分析后台静态资源（HEAD）",
			Description: "与 GET 一致，仅返回响应头。",
			Tags:        []string{_TAG_PLATFORM},
			Responses:   map[string]openapi.Response{"200": {Description: "仅响应头"}},
		},
		"GET /metrics": {
			Summary:     "指标趋势页",
			Description: "展示内存指标存储的历史采样与当前路由统计。",
			Tags:        []string{_TAG_PLATFORM},
			Responses:   map[string]openapi.Response{"200": {Description: "HTML 页面"}},
		},
		"GET /metrics/prometheus": {
			Summary:     "Prometheus 导出",
			Description: "标准文本 exposition 格式，可直接被抓取器使用。",
			Tags:        []string{_TAG_PLATFORM},
			Responses: map[string]openapi.Response{
				"200": {
					Description: "指标文本",
					Content: map[string]openapi.MediaType{
						openapi.MEDIA_TEXT: {Schema: map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
}
