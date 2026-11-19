// Package dashboard 提供开箱即用的指标管理分析后台。
//
// 设计目标：
//   - 零依赖：页面、样式与脚本全部内嵌，可离线部署，不引用任何 CDN；
//   - 只读安全：后台仅暴露聚合指标，不提供任何修改业务状态的接口；
//   - 可扩展：数据来源通过 Provider 注入，鉴权通过 Authorizer 注入，
//     业务方可以替换数据源或叠加自己的权限模型；
//   - 可嵌入：Dashboard 本身实现 http.Handler，可挂到任意路由框架。
//
// 典型用法：
//
//	engine.RegisterDashboard("/metrics",
//		dashboard.UseTitle("订单服务指标"),
//		dashboard.UseAuthorizer(func(req *http.Request) bool {
//			return req.Header.Get("X-Admin-Token") == os.Getenv("ADMIN_TOKEN")
//		}),
//	)
package dashboard

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitee.com/leminewx/leego/middlewares/collector"
)

// indexHTML 为内嵌的单页应用，包含全部样式与脚本。
//
//go:embed assets/index.html
var indexHTML []byte

// 常量定义后台的默认行为。
const (
	// _DEFAULT_TITLE 为默认页面标题。
	_DEFAULT_TITLE = "Leego 指标管理后台"
	// _DEFAULT_REFRESH_INTERVAL 为前端默认自动刷新间隔。
	_DEFAULT_REFRESH_INTERVAL = 5 * time.Second
	// _DEFAULT_MAX_ROUTES 为单次返回的最大路由数量，防止超大报表拖慢浏览器。
	_DEFAULT_MAX_ROUTES = 500
	// _MAX_EXPORT_ROWS 为 CSV 导出的最大行数。
	_MAX_EXPORT_ROWS = 5000
)

// Provider 定义指标数据提供函数。
//
// 每次请求都会调用一次，函数应返回不可变快照；
// 实现方不得在函数内部修改采集器状态。
type Provider func() collector.Report

// Authorizer 定义后台访问鉴权函数。
//
// 返回 false 时后台响应 401；返回 true 时继续处理。
// 传 nil 表示不鉴权（仅建议在内网或本地调试时使用）。
type Authorizer func(req *http.Request) bool

// Options 定义指标后台的可配置项。
type Options struct {
	// Title 为页面标题。
	Title string
	// RefreshInterval 为前端自动刷新间隔；小于等于 0 时使用默认值 5 秒。
	RefreshInterval time.Duration
	// MaxRoutes 为单次返回的最大路由数量；小于等于 0 时使用默认值 500。
	MaxRoutes int
	// EnableExport 表示是否开放 CSV 导出。
	EnableExport bool
	// Provider 为指标数据提供函数；为 nil 时返回空报告。
	Provider Provider
	// Authorizer 为访问鉴权函数；为 nil 时不鉴权。
	Authorizer Authorizer
}

// NewDefaultOptions 返回带安全默认值的后台配置。
func NewDefaultOptions() Options {
	return Options{
		Title:           _DEFAULT_TITLE,
		RefreshInterval: _DEFAULT_REFRESH_INTERVAL,
		MaxRoutes:       _DEFAULT_MAX_ROUTES,
		EnableExport:    true,
	}
}

// Option 定义后台配置选项的函数类型。
type Option func(options *Options)

// UseTitle 设置页面标题。
func UseTitle(title string) Option {
	return func(options *Options) {
		if title = strings.TrimSpace(title); title != "" {
			options.Title = title
		}
	}
}

// UseRefreshInterval 设置前端自动刷新间隔。
func UseRefreshInterval(interval time.Duration) Option {
	return func(options *Options) {
		if interval > 0 {
			options.RefreshInterval = interval
		}
	}
}

// UseMaxRoutes 设置单次返回的最大路由数量。
func UseMaxRoutes(maxRoutes int) Option {
	return func(options *Options) {
		if maxRoutes > 0 {
			options.MaxRoutes = maxRoutes
		}
	}
}

// UseProvider 设置指标数据提供函数。
func UseProvider(provider Provider) Option {
	return func(options *Options) {
		if provider != nil {
			options.Provider = provider
		}
	}
}

// UseAuthorizer 设置访问鉴权函数。
func UseAuthorizer(authorizer Authorizer) Option {
	return func(options *Options) {
		options.Authorizer = authorizer
	}
}

// EnableExport 启用或关闭 CSV 导出。
func EnableExport(enabled bool) Option {
	return func(options *Options) {
		options.EnableExport = enabled
	}
}

// Dashboard 是指标管理后台的 http.Handler 实现。
//
// Dashboard 是并发安全的：所有字段在创建后只读，
// 指标数据通过 Provider 在每次请求时实时获取。
type Dashboard struct {
	// options 为不可变配置。
	options Options
	// page 为渲染后的 HTML 页面（标题已替换）。
	page []byte
	// startedAt 为后台创建时间，用于展示运行时长。
	startedAt time.Time
	// cacheMu 保护 page 的惰性构建。
	cacheMu sync.RWMutex
}

// NewDashboard 创建指标后台。
//
// 返回值非 nil 表示配置非法（例如 Provider 为 nil 且未提供默认值）。
func NewDashboard(options ...Option) (*Dashboard, error) {
	config := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}

	if config.MaxRoutes <= 0 {
		return nil, errors.New("dashboard: max routes must be positive")
	}

	instance := &Dashboard{
		options:   config,
		startedAt: time.Now(),
	}
	instance.page = instance.renderPage()

	return instance, nil
}

// GetOptions 返回后台配置的副本。
func (own *Dashboard) GetOptions() Options {
	return own.options
}

// GetStartedAt 返回后台创建时间。
func (own *Dashboard) GetStartedAt() time.Time {
	return own.startedAt
}

// ServeHTTP 实现 http.Handler 接口。
//
// 路由约定（均相对于注册时使用的前缀）：
//   - GET  ""            指标页面；
//   - GET  "api/report"  完整指标报告（JSON）；
//   - GET  "api/routes"  路由清单（JSON）；
//   - GET  "api/health"  健康检查；
//   - GET  "export.csv"  CSV 导出（可通过 EnableExport(false) 关闭）；
//   - 其他路径返回 404。
func (own *Dashboard) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("X-Content-Type-Options", "nosniff")
	resp.Header().Set("X-Frame-Options", "DENY")
	resp.Header().Set("Referrer-Policy", "no-referrer")
	resp.Header().Set("Cache-Control", "no-store")

	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		resp.Header().Set("Allow", "GET, HEAD")
		http.Error(resp, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if own.options.Authorizer != nil && !own.options.Authorizer(req) {
		http.Error(resp, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	switch own.resolveRoute(req.URL.Path) {
	case _ROUTE_PAGE:
		own.writePage(resp)
	case _ROUTE_REPORT:
		own.writeReport(resp, req)
	case _ROUTE_ROUTES:
		own.writeRoutes(resp, req)
	case _ROUTE_HEALTH:
		own.writeHealth(resp)
	case _ROUTE_EXPORT:
		own.writeCSV(resp, req)
	default:
		http.Error(resp, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// pageRoute 定义后台内部路由枚举。
type pageRoute int

const (
	// _ROUTE_PAGE 表示指标页面。
	_ROUTE_PAGE pageRoute = iota
	// _ROUTE_REPORT 表示完整报告接口。
	_ROUTE_REPORT
	// _ROUTE_ROUTES 表示路由清单接口。
	_ROUTE_ROUTES
	// _ROUTE_HEALTH 表示健康检查接口。
	_ROUTE_HEALTH
	// _ROUTE_EXPORT 表示 CSV 导出接口。
	_ROUTE_EXPORT
	// _ROUTE_UNKNOWN 表示未识别的路径。
	_ROUTE_UNKNOWN
)

// resolveRoute 根据请求路径解析内部路由。
//
// 采用后缀匹配而非前缀匹配，因此后台可以挂在任意前缀下，
// 也兼容前缀本身是否带结尾斜杠。
func (own *Dashboard) resolveRoute(requestPath string) pageRoute {
	cleaned := "/" + strings.Trim(strings.ReplaceAll(requestPath, "\\", "/"), "/")
	trimmed := strings.Trim(cleaned, "/")
	if trimmed == "" {
		return _ROUTE_PAGE
	}

	// 挂载点本身（只有一段路径）直接渲染页面，使 /metrics 与 /metrics/ 行为一致。
	if !strings.Contains(trimmed, "/") {
		return _ROUTE_PAGE
	}

	switch path.Base(cleaned) {
	case "api":
		return _ROUTE_REPORT
	case "report":
		return _ROUTE_REPORT
	case "routes":
		return _ROUTE_ROUTES
	case "health":
		return _ROUTE_HEALTH
	case "export.csv":
		if own.options.EnableExport {
			return _ROUTE_EXPORT
		}
		return _ROUTE_UNKNOWN
	case "index.html":
		return _ROUTE_PAGE
	default:
		return _ROUTE_UNKNOWN
	}
}

// writePage 输出指标页面。
func (own *Dashboard) writePage(resp http.ResponseWriter) {
	own.cacheMu.RLock()
	page := own.page
	own.cacheMu.RUnlock()

	resp.Header().Set("Content-Type", "text/html; charset=utf-8")
	resp.WriteHeader(http.StatusOK)
	_, _ = resp.Write(page)
}

// writeReport 输出完整指标报告。
func (own *Dashboard) writeReport(resp http.ResponseWriter, req *http.Request) {
	report := own.collectReport()
	report = filterReport(report, req)
	report = limitReport(report, own.options.MaxRoutes)
	writeJSON(resp, http.StatusOK, report)
}

// writeRoutes 输出路由清单。
func (own *Dashboard) writeRoutes(resp http.ResponseWriter, req *http.Request) {
	report := filterReport(own.collectReport(), req)
	report = limitReport(report, own.options.MaxRoutes)
	writeJSON(resp, http.StatusOK, map[string]any{
		"generated_at": report.GeneratedAt,
		"routes":       report.Routes,
		"totals":       report.Totals,
	})
}

// writeHealth 输出健康检查结果。
func (own *Dashboard) writeHealth(resp http.ResponseWriter) {
	writeJSON(resp, http.StatusOK, map[string]any{
		"status":     "ok",
		"started_at": own.startedAt,
		"uptime":     time.Since(own.startedAt).String(),
	})
}

// writeCSV 输出 CSV 报表。
//
// 列固定为：方法、模式、名称、状态码、次数、平均耗时(微秒)、最大耗时(微秒)、最小耗时(微秒)。
// 该格式可直接被 Excel 与常见 BI 工具导入。
func (own *Dashboard) writeCSV(resp http.ResponseWriter, req *http.Request) {
	report := limitReport(filterReport(own.collectReport(), req), _MAX_EXPORT_ROWS)

	resp.Header().Set("Content-Type", "text/csv; charset=utf-8")
	resp.Header().Set("Content-Disposition", `attachment; filename="leego-metrics.csv"`)
	resp.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(resp)
	defer writer.Flush()

	_ = writer.Write([]string{"method", "pattern", "name", "status", "total", "avg_cost_micro", "max_cost_micro", "min_cost_micro"})
	for _, route := range report.Routes {
		if len(route.Metrics) == 0 {
			_ = writer.Write([]string{route.Method, route.Pattern, route.Name, "", strconv.FormatUint(route.Total, 10), strconv.FormatInt(route.GetAverageCostMicro(), 10), "", ""})
			continue
		}

		for _, item := range route.Metrics {
			_ = writer.Write([]string{
				route.Method,
				route.Pattern,
				route.Name,
				strconv.Itoa(item.Code),
				strconv.FormatUint(item.Total, 10),
				strconv.FormatInt(item.GetAverageCostMicro(), 10),
				strconv.FormatInt(item.MaxCostMicro, 10),
				strconv.FormatInt(item.MinCostMicro, 10),
			})
		}
	}
}

// collectReport 调用 Provider 获取报告，并对 nil 做兜底。
func (own *Dashboard) collectReport() collector.Report {
	if own.options.Provider == nil {
		return collector.NewEmptyReport()
	}

	return own.options.Provider()
}

// writeJSON 以 JSON 格式输出响应。
func writeJSON(resp http.ResponseWriter, status int, payload any) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(status)
	encoder := json.NewEncoder(resp)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

// filterReport 根据查询参数过滤报告。
//
// 支持的参数：
//   - q：按方法、模式、名称、标签模糊匹配（大小写不敏感）；
//   - method：按 HTTP 方法精确过滤；
//   - status：按状态码精确过滤（仅保留匹配的状态码指标）；
//   - enabled：传入 true/false 过滤启用状态。
func filterReport(report collector.Report, req *http.Request) collector.Report {
	query := req.URL.Query()
	keyword := strings.ToLower(strings.TrimSpace(query.Get("q")))
	method := strings.ToUpper(strings.TrimSpace(query.Get("method")))
	status, hasStatus := parseStatus(query.Get("status"))
	enabled, hasEnabled := parseBool(query.Get("enabled"))

	filtered := collector.Report{
		GeneratedAt: report.GeneratedAt,
		Routes:      make([]collector.Snapshot, 0, len(report.Routes)),
	}

	for _, route := range report.Routes {
		if method != "" && !strings.EqualFold(route.Method, method) {
			continue
		}

		if hasEnabled && route.Enabled != enabled {
			continue
		}

		if keyword != "" && !matchesKeyword(route, keyword) {
			continue
		}

		if hasStatus {
			route.Metrics = filterMetrics(route.Metrics, status)
			if len(route.Metrics) == 0 {
				continue
			}
		}

		filtered.Routes = append(filtered.Routes, route)
	}

	filtered.Totals = sumTotals(filtered.Routes)
	return filtered
}

// matchesKeyword 判断路由是否命中关键字。
func matchesKeyword(route collector.Snapshot, keyword string) bool {
	fields := []string{route.Method, route.Pattern, route.Name}
	fields = append(fields, route.Tags...)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), keyword) {
			return true
		}
	}

	return false
}

// filterMetrics 只保留指定状态码的指标。
func filterMetrics(metrics []collector.Metric, status int) []collector.Metric {
	result := make([]collector.Metric, 0, len(metrics))
	for _, item := range metrics {
		if item.Code == status {
			result = append(result, item)
		}
	}

	return result
}

// sumTotals 汇总过滤后的路由指标。
func sumTotals(routes []collector.Snapshot) collector.Totals {
	totals := collector.Totals{Routes: len(routes)}
	for _, route := range routes {
		totals.Requests += route.Total
		totals.Flights += route.Flights
		totals.Failed += route.Failed
		totals.TotalCostMicro += route.TotalCostMicro
	}

	return totals
}

// limitReport 按请求量截断路由数量，优先保留请求量最高的路由。
func limitReport(report collector.Report, limit int) collector.Report {
	if limit <= 0 || len(report.Routes) <= limit {
		return report
	}

	routes := append([]collector.Snapshot(nil), report.Routes...)
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].Total > routes[j].Total
	})

	report.Routes = routes[:limit]
	report.Totals = sumTotals(report.Routes)
	return report
}

// parseStatus 解析状态码查询参数。
func parseStatus(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	status, err := strconv.Atoi(value)
	if err != nil || status < 100 || status > 599 {
		return 0, false
	}

	return status, true
}

// parseBool 解析布尔查询参数。
func parseBool(value string) (bool, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, false
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}

	return parsed, true
}

// renderPage 渲染页面模板，替换标题与刷新间隔占位符。
func (own *Dashboard) renderPage() []byte {
	refreshMilliseconds := own.options.RefreshInterval.Milliseconds()
	if refreshMilliseconds <= 0 {
		refreshMilliseconds = _DEFAULT_REFRESH_INTERVAL.Milliseconds()
	}

	page := string(indexHTML)
	page = strings.ReplaceAll(page, "{{TITLE}}", escapeHTML(own.options.Title))
	page = strings.ReplaceAll(page, "{{REFRESH_MS}}", strconv.FormatInt(refreshMilliseconds, 10))
	page = strings.ReplaceAll(page, "{{EXPORT_ENABLED}}", strconv.FormatBool(own.options.EnableExport))
	return []byte(page)
}

// escapeHTML 对标题做最小化转义，避免注入到 <title> 中。
func escapeHTML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(value)
}

// String 返回后台的简短描述，便于日志输出。
func (own *Dashboard) String() string {
	return fmt.Sprintf("dashboard{title=%q, maxRoutes=%d}", own.options.Title, own.options.MaxRoutes)
}
