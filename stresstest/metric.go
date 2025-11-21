package stress

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 定义性能测试各项指标的数据结构
type Metrics struct {
	StartTime     time.Time     // 开始时间
	EndTime       time.Time     // 结束事件
	TotalRequests int64         // 总请求数
	SuccessCount  int64         // 成功请求数
	FailedCount   int64         // 失败请求数
	ErrorCount    int64         // 错误总数
	Latencies     *Latencies    // 延迟数据
	StatusCodes   map[int]int64 // 状态码数据

	lock sync.RWMutex
}

// NewMetrics 创建一个新的指标
func NewMetrics() *Metrics {
	return &Metrics{
		StartTime:   time.Now(),
		StatusCodes: make(map[int]int64),
		Latencies:   new(Latencies),
	}
}

// Record 记录一次请求结果
func (own *Metrics) Record(statusCode int, latency time.Duration, err error) {
	own.lock.Lock()
	defer own.lock.Unlock()

	atomic.AddInt64(&own.TotalRequests, 1)
	own.Latencies.Append(latency)
	own.StatusCodes[statusCode]++

	if err == nil && statusCode < 400 {
		atomic.AddInt64(&own.SuccessCount, 1)
		return
	}

	atomic.AddInt64(&own.FailedCount, 1)
	if err != nil {
		atomic.AddInt64(&own.ErrorCount, 1)
	}
}

// Calculate 计算性能测试指标
func (own *Metrics) Calculate() {
}

// Report 输出性能测试结果
func (own *Metrics) Report() string {
	duration := own.EndTime.Sub(own.StartTime)
	buffer := new(strings.Builder)

	buffer.WriteString("\n================================= 性能测试报告 =================================")
	buffer.WriteString("\n测试时间: ")
	buffer.WriteString(duration.String())
	buffer.WriteString("\n总请求数: ")
	buffer.WriteString(strconv.FormatInt(own.TotalRequests, 10))
	buffer.WriteString("\n成功请求: ")
	buffer.WriteString(strconv.FormatInt(own.SuccessCount, 10))
	buffer.WriteString("\n失败请求: ")
	buffer.WriteString(strconv.FormatInt(own.FailedCount, 10))

	buffer.WriteString("\n成功率: ")
	buffer.WriteString(strconv.FormatFloat(float64(own.SuccessCount)/float64(own.TotalRequests)*100, 'f', 2, 64))
	buffer.WriteString("\n错误数: ")
	buffer.WriteString(strconv.FormatInt(own.ErrorCount, 10))
	buffer.WriteString("\nQPS: ")
	buffer.WriteString(strconv.FormatFloat(float64(own.SuccessCount)/duration.Seconds(), 'f', 2, 64))

	// 计算延迟统计
	if own.Latencies.Len() > 0 {
		sort.Sort(own.Latencies)
		totalLatency := own.Latencies.CountTotalLatency()
		avgLatency := totalLatency / time.Duration(own.Latencies.Len())
		latencies := own.Latencies.GetLatencies()

		buffer.WriteString("\n最小延迟：")
		buffer.WriteString(latencies[0].String())
		buffer.WriteString("\n最大延迟：")
		buffer.WriteString(latencies[len(latencies)-1].String())
		buffer.WriteString("\n平均延迟：")
		buffer.WriteString(avgLatency.String())
		buffer.WriteString("\nP90延迟：")
		buffer.WriteString(own.Latencies.GetPercentile(90).String())
		buffer.WriteString("\nP95延迟：")
		buffer.WriteString(own.Latencies.GetPercentile(95).String())
		buffer.WriteString("\nP99延迟：")
		buffer.WriteString(own.Latencies.GetPercentile(99).String())
	}

	buffer.WriteString("\n状态码分布：")
	for code, count := range own.StatusCodes {
		_, _ = fmt.Fprintf(buffer, "\n  %d: %d (%.1f%%)\n", code, count, float64(count)/float64(own.TotalRequests)*100)
	}

	return buffer.String()
}
