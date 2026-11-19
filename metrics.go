package leego

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"gitee.com/leminewx/leego/logging"
	"gitee.com/leminewx/leego/metrics"
	"gitee.com/leminewx/leego/middlewares/collector"
)

const (
	// _DEFAULT_METRICS_PREFIX 为默认指标挂载前缀。
	_DEFAULT_METRICS_PREFIX = "/metrics"
	// _DEFAULT_METRICS_INTERVAL 为默认采集间隔。
	_DEFAULT_METRICS_INTERVAL = 5 * time.Second
	// _DEFAULT_METRICS_STEP 为默认查询聚合步长。
	_DEFAULT_METRICS_STEP = 30 * time.Second
	// _METRICS_QUERY_WINDOW 为历史查询的默认回溯窗口。
	_METRICS_QUERY_WINDOW = 24 * time.Hour
)

// MetricsConfig 定义指标挂载行为。
type MetricsConfig struct {
	// Prefix 为挂载前缀，空值时使用 /metrics。
	Prefix string
	// Interval 为采集间隔，非正数时使用 5s。
	Interval time.Duration
	// Step 为查询聚合步长，非正数时使用 30s。
	Step time.Duration
	// DisablePrometheus 为 true 时不注册 /prometheus 子路由。
	DisablePrometheus bool
	// Authorizer 为指标端点的访问控制函数，返回 false 时拒绝请求。
	//
	// 指标包含路由名、请求量与耗时等运行信息，nil 表示不做校验，
	// 仅适用于只在内网或本机监听的部署。
	Authorizer func(*Context) bool
}

// NewMetricsConfig 返回推荐配置。
func NewMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Prefix:   _DEFAULT_METRICS_PREFIX,
		Interval: _DEFAULT_METRICS_INTERVAL,
		Step:     _DEFAULT_METRICS_STEP,
	}
}

// normalize 修正非法配置。
func (own MetricsConfig) normalize() MetricsConfig {
	if own.Prefix == "" {
		own.Prefix = _DEFAULT_METRICS_PREFIX
	}

	if own.Interval <= 0 {
		own.Interval = _DEFAULT_METRICS_INTERVAL
	}

	if own.Step <= 0 {
		own.Step = _DEFAULT_METRICS_STEP
	}

	return own
}

// MetricsOption 修改 MetricsConfig。
type MetricsOption func(*MetricsConfig)

// UseMetricsInterval 设置采集间隔。
func UseMetricsInterval(interval time.Duration) MetricsOption {
	return func(config *MetricsConfig) {
		if interval > 0 {
			config.Interval = interval
		}
	}
}

// UseMetricsStep 设置查询聚合步长。
func UseMetricsStep(step time.Duration) MetricsOption {
	return func(config *MetricsConfig) {
		if step > 0 {
			config.Step = step
		}
	}
}

// DisableMetricsPrometheus 关闭 Prometheus 抓取路由。
func DisableMetricsPrometheus() MetricsOption {
	return func(config *MetricsConfig) {
		config.DisablePrometheus = true
	}
}

// UseMetricsAuthorizer 设置指标端点的访问控制函数。
//
// 传入 nil 等价于不做校验；建议在生产环境用内网限制或鉴权函数保护。
func UseMetricsAuthorizer(authorizer func(*Context) bool) MetricsOption {
	return func(config *MetricsConfig) {
		config.Authorizer = authorizer
	}
}

// authorize 校验管理端点的访问权限。
//
// 未配置授权函数时放行；拒绝时写入 401 并返回 false。
func (own MetricsConfig) authorize(ctx *Context) bool {
	if own.Authorizer == nil {
		return true
	}

	if own.Authorizer(ctx) {
		return true
	}

	ctx.ResponseError(http.StatusUnauthorized, "metrics: unauthorized")
	return false
}

// RegisterMetrics 挂载指标导出路由，并启动后台采集。
//
// 注册的路由：
//   - GET {prefix}             历史趋势 JSON（按 Step 聚合）
//   - GET {prefix}/prometheus  Prometheus 文本格式（可用 DisableMetricsPrometheus 关闭）
//
// 必须在 Prepare 之前调用；后台采集协程与存储会随 Engine.Shutdown 一并关闭。
func (own *Engine) RegisterMetrics(prefix string, store metrics.Store, options ...MetricsOption) error {
	if store == nil {
		return errors.New("leego: metrics store must not be nil")
	}

	config := NewMetricsConfig()
	config.Prefix = prefix
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	config = config.normalize()

	prefix = config.Prefix

	own.GET(prefix, func(ctx *Context) {
		if !config.authorize(ctx) {
			return
		}

		now := time.Now()
		points, err := store.Query(now.Add(-_METRICS_QUERY_WINDOW), now, config.Step)
		if err != nil {
			// 存储错误可能包含文件路径，只记日志，对外返回固定文案。
			logging.LogWarn("metrics: query failed: %v", err)
			ctx.ResponseError(http.StatusServiceUnavailable, "metrics: unavailable")
			return
		}

		_ = ctx.ResponseJSON(http.StatusOK, map[string]any{
			"generated_at": now,
			"step":         config.Step.String(),
			"points":       points,
		})
	}, "metrics")

	if !config.DisablePrometheus {
		prometheus := metrics.NewPrometheusHandler(own.CollectReport)
		own.GET(prefix+"/prometheus", func(ctx *Context) {
			if !config.authorize(ctx) {
				return
			}

			prometheus.ServeHTTP(ctx.Response, ctx.Request)
		}, "metrics-prometheus")
	}

	own.startMetricsCollector(store, config.Interval)
	return nil
}

// startMetricsCollector 启动后台采集，并在关闭时释放存储。
func (own *Engine) startMetricsCollector(store metrics.Store, interval time.Duration) {
	stop := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(interval)
		for {
			select {
			case <-ticker.C:
				own.appendMetrics(store)
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()

	_ = own.AddShutdownHook(func(_ context.Context) error {
		once.Do(func() { close(stop) })
		return store.Close()
	})
}

// appendMetrics 把当前汇总写入存储。
//
// 汇总后的 Snapshot 只保留全局计数：按路由拆分的历史由 dashboard 负责，
// 这里的目标是提供"整体水位"趋势，避免同一指标重复存储。
func (own *Engine) appendMetrics(store metrics.Store) {
	report := own.CollectReport()
	snapshot := collector.Snapshot{
		Total:          report.Totals.Requests,
		Failed:         report.Totals.Failed,
		Flights:        report.Totals.Flights,
		TotalCostMicro: report.Totals.TotalCostMicro,
	}

	_ = store.Append(snapshot)
}
