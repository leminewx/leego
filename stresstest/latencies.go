package stress

import (
	"sort"
	"time"
)

// Latencies 记录一次压测中所有请求的延迟样本。
//
// 该结构不是并发安全的：每个 worker 持有独立的采集器，压测结束后再合并，
// 这样既避免了锁竞争，也保证样本不会因为加锁而失真。
type Latencies struct {
	// values 保存延迟样本，按记录顺序追加。
	values []time.Duration
	// sorted 标记 values 是否已按升序排列，避免重复排序。
	sorted bool
}

// NewLatencies 创建一个空的延迟采集器。
func NewLatencies() *Latencies {
	return &Latencies{values: make([]time.Duration, 0)}
}

// NewLatenciesWithCapacity 创建带有预分配容量的延迟采集器。
//
// 参数 capacity 为预估的样本数量，小于 1 时忽略。
func NewLatenciesWithCapacity(capacity int) *Latencies {
	if capacity < 1 {
		capacity = 0
	}

	return &Latencies{values: make([]time.Duration, 0, capacity)}
}

// Append 追加一个延迟样本。
func (own *Latencies) Append(latency time.Duration) {
	own.values = append(own.values, latency)
	own.sorted = false
}

// Merge 追加另一个采集器中的全部样本。
//
// 参数 other 为待合并的采集器，允许为 nil。
func (own *Latencies) Merge(other *Latencies) {
	if other == nil || len(other.values) == 0 {
		return
	}

	own.values = append(own.values, other.values...)
	own.sorted = false
}

// GetLatencies 返回延迟样本的副本，调用方修改不会影响采集器。
func (own *Latencies) GetLatencies() []time.Duration {
	return append([]time.Duration(nil), own.values...)
}

// GetLen 返回样本数量。
func (own *Latencies) GetLen() int {
	return len(own.values)
}

// CountTotal 返回所有样本的延迟总和。
func (own *Latencies) CountTotal() time.Duration {
	var total time.Duration
	for _, latency := range own.values {
		total += latency
	}

	return total
}

// GetAverage 返回平均延迟；没有样本时返回 0。
func (own *Latencies) GetAverage() time.Duration {
	if len(own.values) == 0 {
		return 0
	}

	return own.CountTotal() / time.Duration(len(own.values))
}

// GetMin 返回最小延迟；没有样本时返回 0。
func (own *Latencies) GetMin() time.Duration {
	if len(own.values) == 0 {
		return 0
	}

	own.Sort()
	return own.values[0]
}

// GetMax 返回最大延迟；没有样本时返回 0。
func (own *Latencies) GetMax() time.Duration {
	if len(own.values) == 0 {
		return 0
	}

	own.Sort()
	return own.values[len(own.values)-1]
}

// GetPercentile 返回指定分位数的延迟。
//
// 参数 percentile 取值区间为 (0, 100]；越界时会被裁剪到合法区间。
// 方法内部会保证样本有序，可安全重复调用；没有样本时返回 0。
func (own *Latencies) GetPercentile(percentile float64) time.Duration {
	if len(own.values) == 0 {
		return 0
	}

	if percentile <= 0 {
		percentile = 1
	}

	if percentile > 100 {
		percentile = 100
	}

	own.Sort()

	// 采用最近秩法（nearest-rank）：第 p 百分位对应下标 ceil(p/100*n)-1。
	index := int(float64(len(own.values))*percentile/100.0+0.999999) - 1
	if index < 0 {
		index = 0
	}

	if index >= len(own.values) {
		index = len(own.values) - 1
	}

	return own.values[index]
}

// Sort 将样本按升序排列；已排序时直接返回。
func (own *Latencies) Sort() {
	if own.sorted || len(own.values) < 2 {
		own.sorted = true
		return
	}

	sort.Slice(own.values, func(i, j int) bool {
		return own.values[i] < own.values[j]
	})

	own.sorted = true
}

// Reset 清空采集器，便于复用。
func (own *Latencies) Reset() {
	own.values = own.values[:0]
	own.sorted = true
}
