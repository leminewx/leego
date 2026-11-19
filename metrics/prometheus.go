package metrics

import (
	"net/http"
	"strconv"
	"strings"

	"gitee.com/leminewx/leego/middleware/collector"
)

const (
	// _PROMETHEUS_CONTENT_TYPE 为 Prometheus 文本格式的响应类型。
	_PROMETHEUS_CONTENT_TYPE = "text/plain; version=0.0.4; charset=utf-8"
	// _SECOND 为微秒到秒的换算基数。
	_SECOND = 1_000_000.0
)

// NewPrometheusHandler 创建 Prometheus 抓取端点。
//
// source 为指标快照提供函数，通常传入 engine.CollectReport；
// 传 nil 时输出空指标集，保证抓取端不会因为未接线而失败。
func NewPrometheusHandler(source func() collector.Report) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		report := collector.NewEmptyReport()
		if source != nil {
			report = source()
		}

		resp.Header().Set("Content-Type", _PROMETHEUS_CONTENT_TYPE)
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write([]byte(encodePrometheus(report)))
	})
}

// encodePrometheus 把汇总报告编码为 Prometheus 文本格式。
//
// 输出顺序完全由"方法 + 模式 + 状态码"决定，两次抓取之间只有数值变化，
// 便于逐字对比测试与人工 diff。
func encodePrometheus(report collector.Report) string {
	builder := new(strings.Builder)

	writeCounterHeader(builder, "leego_http_requests_total", "Total number of HTTP requests handled.")
	writeSummaryHeader(builder, "leego_http_request_duration_seconds", "HTTP request latency in seconds.")
	writeGaugeHeader(builder, "leego_http_requests_in_flight", "Number of HTTP requests currently being processed.")
	writeCounterHeader(builder, "leego_http_requests_failed_total", "Total number of 5xx HTTP responses.")

	for _, route := range report.Routes {
		labels := routeLabels(route)

		for _, metric := range route.Metrics {
			builder.WriteString("leego_http_requests_total{")
			builder.WriteString(labels)
			builder.WriteString(",status=\"")
			builder.WriteString(strconv.Itoa(metric.Code))
			builder.WriteString("\"} ")
			builder.WriteString(strconv.FormatUint(metric.Total, 10))
			builder.WriteString("\n")
		}

		builder.WriteString("leego_http_request_duration_seconds_sum{")
		builder.WriteString(labels)
		builder.WriteString("} ")
		builder.WriteString(formatSeconds(route.TotalCostMicro))
		builder.WriteString("\n")

		builder.WriteString("leego_http_request_duration_seconds_count{")
		builder.WriteString(labels)
		builder.WriteString("} ")
		builder.WriteString(strconv.FormatUint(route.Total, 10))
		builder.WriteString("\n")

		builder.WriteString("leego_http_requests_in_flight{")
		builder.WriteString(labels)
		builder.WriteString("} ")
		builder.WriteString(strconv.FormatInt(route.Flights, 10))
		builder.WriteString("\n")

		if route.Failed > 0 {
			builder.WriteString("leego_http_requests_failed_total{")
			builder.WriteString(labels)
			builder.WriteString("} ")
			builder.WriteString(strconv.FormatUint(route.Failed, 10))
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

// routeLabels 生成路由维度的标签串（不含 status）。
func routeLabels(route collector.Snapshot) string {
	builder := new(strings.Builder)
	builder.WriteString("method=\"")
	builder.WriteString(escapeLabelValue(route.Method))
	builder.WriteString("\",pattern=\"")
	builder.WriteString(escapeLabelValue(route.Pattern))
	builder.WriteString("\"")

	if route.Name != "" {
		builder.WriteString(",name=\"")
		builder.WriteString(escapeLabelValue(route.Name))
		builder.WriteString("\"")
	}

	return builder.String()
}

// formatSeconds 把微秒转换为秒，并保留 6 位小数。
func formatSeconds(micro int64) string {
	return strconv.FormatFloat(float64(micro)/_SECOND, 'f', 6, 64)
}

// escapeLabelValue 按 Prometheus 规范转义标签值。
func escapeLabelValue(value string) string {
	if !strings.ContainsAny(value, "\\\"\n") {
		return value
	}

	var builder strings.Builder
	builder.Grow(len(value) + 8)
	for _, char := range value {
		switch char {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`\"`)
		case '\n':
			builder.WriteString(`\n`)
		default:
			builder.WriteRune(char)
		}
	}

	return builder.String()
}

// writeCounterHeader 写入 counter 的 HELP/TYPE 注释。
func writeCounterHeader(builder *strings.Builder, name, help string) {
	builder.WriteString("# HELP ")
	builder.WriteString(name)
	builder.WriteString(" ")
	builder.WriteString(help)
	builder.WriteString("\n# TYPE ")
	builder.WriteString(name)
	builder.WriteString(" counter\n")
}

// writeGaugeHeader 写入 gauge 的 HELP/TYPE 注释。
func writeGaugeHeader(builder *strings.Builder, name, help string) {
	builder.WriteString("# HELP ")
	builder.WriteString(name)
	builder.WriteString(" ")
	builder.WriteString(help)
	builder.WriteString("\n# TYPE ")
	builder.WriteString(name)
	builder.WriteString(" gauge\n")
}

// writeSummaryHeader 写入 summary 的 HELP/TYPE 注释。
func writeSummaryHeader(builder *strings.Builder, name, help string) {
	builder.WriteString("# HELP ")
	builder.WriteString(name)
	builder.WriteString(" ")
	builder.WriteString(help)
	builder.WriteString("\n# TYPE ")
	builder.WriteString(name)
	builder.WriteString(" summary\n")
}
