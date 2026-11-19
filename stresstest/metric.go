package stress

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MetricsSnapshot 是一次压测结束后生成的不可变指标快照。
//
// 快照中的所有字段都在同一把锁内读取，因此彼此一致，可直接用于渲染报表。
type MetricsSnapshot struct {
	// StartTime 为压测开始时间。
	StartTime time.Time
	// EndTime 为压测结束时间。
	EndTime time.Time
	// Duration 为压测总耗时。
	Duration time.Duration
	// TotalRequests 为实际发起的请求数。
	TotalRequests int64
	// SuccessCount 为状态码小于 400 且无错误的请求数。
	SuccessCount int64
	// FailedCount 为失败请求数（含状态码错误与网络错误）。
	FailedCount int64
	// ErrorCount 为发生网络错误的请求数。
	ErrorCount int64
	// SuccessRate 为成功率，取值区间 [0, 1]。
	SuccessRate float64
	// QPS 为每秒成功请求数。
	QPS float64
	// MinLatency 为最小延迟。
	MinLatency time.Duration
	// MaxLatency 为最大延迟。
	MaxLatency time.Duration
	// AvgLatency 为平均延迟。
	AvgLatency time.Duration
	// P50Latency 为中位延迟。
	P50Latency time.Duration
	// P90Latency 为 90 分位延迟。
	P90Latency time.Duration
	// P95Latency 为 95 分位延迟。
	P95Latency time.Duration
	// P99Latency 为 99 分位延迟。
	P99Latency time.Duration
	// StatusCodes 为状态码分布；错误请求计入状态码 0。
	StatusCodes map[int]int64
}

// Metrics 收集一次压测的原始指标。
//
// 所有读写都经过内部互斥锁，因此可被多个 worker 并发调用；
// 对外暴露的只有快照与报表字符串，避免调用方读取到撕裂的字段。
type Metrics struct {
	// lock 保护以下全部字段。
	lock sync.Mutex
	// startTime 为压测开始时间。
	startTime time.Time
	// endTime 为压测结束时间。
	endTime time.Time
	// totalRequests 为总请求数。
	totalRequests int64
	// successCount 为成功请求数。
	successCount int64
	// failedCount 为失败请求数。
	failedCount int64
	// errorCount 为网络错误数。
	errorCount int64
	// latencies 为延迟样本。
	latencies *Latencies
	// statusCodes 为状态码分布。
	statusCodes map[int]int64
}

// NewMetrics 创建一个新的指标收集器，并以当前时间作为开始时间。
func NewMetrics() *Metrics {
	return &Metrics{
		startTime:   time.Now(),
		latencies:   NewLatencies(),
		statusCodes: make(map[int]int64),
	}
}

// Record 记录一次请求结果。
//
// 参数：
//   - statusCode：响应状态码；网络错误时传 0；
//   - latency：本次请求耗时；
//   - err：网络错误，nil 表示请求正常完成。
func (own *Metrics) Record(statusCode int, latency time.Duration, err error) {
	own.lock.Lock()
	defer own.lock.Unlock()

	own.totalRequests++
	own.latencies.Append(latency)
	own.statusCodes[statusCode]++

	if err == nil && statusCode > 0 && statusCode < 400 {
		own.successCount++
		return
	}

	own.failedCount++
	if err != nil {
		own.errorCount++
	}
}

// Finish 标记压测结束；重复调用只有第一次生效。
func (own *Metrics) Finish() {
	own.lock.Lock()
	defer own.lock.Unlock()

	if own.endTime.IsZero() {
		own.endTime = time.Now()
	}
}

// GetSnapshot 返回当前指标的只读快照。
//
// 若压测尚未结束，EndTime 与 Duration 使用“当前时间”计算，便于实时观测。
func (own *Metrics) GetSnapshot() MetricsSnapshot {
	own.lock.Lock()
	defer own.lock.Unlock()

	endTime := own.endTime
	if endTime.IsZero() {
		endTime = time.Now()
	}

	duration := endTime.Sub(own.startTime)

	snapshot := MetricsSnapshot{
		StartTime:     own.startTime,
		EndTime:       endTime,
		Duration:      duration,
		TotalRequests: own.totalRequests,
		SuccessCount:  own.successCount,
		FailedCount:   own.failedCount,
		ErrorCount:    own.errorCount,
		StatusCodes:   make(map[int]int64, len(own.statusCodes)),
		MinLatency:    own.latencies.GetMin(),
		MaxLatency:    own.latencies.GetMax(),
		AvgLatency:    own.latencies.GetAverage(),
		P50Latency:    own.latencies.GetPercentile(50),
		P90Latency:    own.latencies.GetPercentile(90),
		P95Latency:    own.latencies.GetPercentile(95),
		P99Latency:    own.latencies.GetPercentile(99),
	}

	for code, count := range own.statusCodes {
		snapshot.StatusCodes[code] = count
	}

	if own.totalRequests > 0 {
		snapshot.SuccessRate = float64(own.successCount) / float64(own.totalRequests)
	}

	if duration > 0 {
		snapshot.QPS = float64(own.successCount) / duration.Seconds()
	}

	return snapshot
}

// GetReport 生成可直接打印的性能测试报告。
//
// 报告包含耗时、请求统计、QPS、延迟分位数与状态码分布。
func (own *Metrics) GetReport() string {
	return FormatReport(own.GetSnapshot())
}

// FormatReport 将指标快照格式化为人类可读的报告文本。
func FormatReport(snapshot MetricsSnapshot) string {
	buffer := new(strings.Builder)

	buffer.WriteString("\n================================= 性能测试报告 =================================\n")
	buffer.WriteString("开始时间: ")
	buffer.WriteString(snapshot.StartTime.Format(time.DateTime))
	buffer.WriteString("\n结束时间: ")
	buffer.WriteString(snapshot.EndTime.Format(time.DateTime))
	buffer.WriteString("\n测试耗时: ")
	buffer.WriteString(snapshot.Duration.String())
	buffer.WriteString("\n总请求数: ")
	buffer.WriteString(strconv.FormatInt(snapshot.TotalRequests, 10))
	buffer.WriteString("\n成功请求: ")
	buffer.WriteString(strconv.FormatInt(snapshot.SuccessCount, 10))
	buffer.WriteString("\n失败请求: ")
	buffer.WriteString(strconv.FormatInt(snapshot.FailedCount, 10))
	buffer.WriteString("\n网络错误: ")
	buffer.WriteString(strconv.FormatInt(snapshot.ErrorCount, 10))
	buffer.WriteString("\n成功率: ")
	buffer.WriteString(strconv.FormatFloat(snapshot.SuccessRate*100, 'f', 2, 64))
	buffer.WriteString("%\nQPS: ")
	buffer.WriteString(strconv.FormatFloat(snapshot.QPS, 'f', 2, 64))

	buffer.WriteString("\n最小延迟: ")
	buffer.WriteString(snapshot.MinLatency.String())
	buffer.WriteString("\n最大延迟: ")
	buffer.WriteString(snapshot.MaxLatency.String())
	buffer.WriteString("\n平均延迟: ")
	buffer.WriteString(snapshot.AvgLatency.String())
	buffer.WriteString("\nP50 延迟: ")
	buffer.WriteString(snapshot.P50Latency.String())
	buffer.WriteString("\nP90 延迟: ")
	buffer.WriteString(snapshot.P90Latency.String())
	buffer.WriteString("\nP95 延迟: ")
	buffer.WriteString(snapshot.P95Latency.String())
	buffer.WriteString("\nP99 延迟: ")
	buffer.WriteString(snapshot.P99Latency.String())

	buffer.WriteString("\n状态码分布:")

	codes := make([]int, 0, len(snapshot.StatusCodes))
	for code := range snapshot.StatusCodes {
		codes = append(codes, code)
	}

	sort.Ints(codes)
	for _, code := range codes {
		count := snapshot.StatusCodes[code]
		ratio := 0.0
		if snapshot.TotalRequests > 0 {
			ratio = float64(count) / float64(snapshot.TotalRequests) * 100
		}

		_, _ = fmt.Fprintf(buffer, "\n  %d: %d (%.1f%%)", code, count, ratio)
	}

	buffer.WriteString("\n")

	return buffer.String()
}
