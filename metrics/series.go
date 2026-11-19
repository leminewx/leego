// Package metrics 提供指标的时间序列聚合、持久化与 Prometheus 导出。
//
// 设计要点：
//   - 只依赖标准库与仓库内的 collector，不引入任何第三方客户端；
//   - 环形桶按固定宽度划分时间片，写入是 O(1)，查询按 step 聚合；
//   - 文件存储使用 JSONL，逐行追加并立即 Flush，牺牲吞吐换崩溃一致性；
//   - collector 只提供 min/max/avg，没有直方图，因此 Point 用窗口内最大耗时
//     作为 P95 的保守上界，字段名如实命名为 MaxMicro。
package metrics

import (
	"sort"
	"sync"
	"time"

	"gitee.com/leminewx/leego/middleware/collector"
)

const (
	// _DEFAULT_BUCKETS 为默认环形桶数量。
	_DEFAULT_BUCKETS = 60
	// _MIN_BUCKET_WIDTH 为桶宽下限，避免窗口过小导致除零。
	_MIN_BUCKET_WIDTH = time.Millisecond
)

// Point 是一个时间片上的聚合指标。
type Point struct {
	// At 为时间片起点。
	At time.Time `json:"at"`
	// Total 为该时间片内的响应次数。
	Total uint64 `json:"total"`
	// Failed 为该时间片内的 5xx 响应次数。
	Failed uint64 `json:"failed"`
	// AvgMicro 为该时间片内的平均耗时（微秒）。
	AvgMicro int64 `json:"avg_micro"`
	// MaxMicro 为该时间片内的最大耗时（微秒）。
	MaxMicro int64 `json:"max_micro"`
}

// bucket 是环形缓冲中的一个时间片。
type bucket struct {
	// start 为时间片起点；零值表示该槽尚未使用。
	start time.Time
	// total 为响应次数。
	total uint64
	// failed 为 5xx 响应次数。
	failed uint64
	// totalCostMicro 为累计耗时（微秒）。
	totalCostMicro int64
	// maxMicro 为最大耗时（微秒）。
	maxMicro int64
}

// TimeSeries 是固定容量的时间序列。
//
// 并发模型：Append 与 Query 都在同一把互斥锁内执行。
// 指标写入频率是"每采集周期一次"，锁竞争可以忽略。
type TimeSeries struct {
	// mu 保护 buckets 与 filled。
	mu sync.Mutex
	// width 为单个时间片的宽度。
	width time.Duration
	// buckets 为环形缓冲，buckets[0] 始终是最早的时间片。
	buckets []bucket
	// filled 标记缓冲是否已经开始滚动。
	filled bool
}

// NewTimeSeries 创建时间序列。
//
// window 为覆盖的时间跨度，buckets 为时间片数量；
// 桶宽 = window / buckets，且不小于 1ms。
func NewTimeSeries(window time.Duration, buckets int) *TimeSeries {
	if buckets <= 0 {
		buckets = _DEFAULT_BUCKETS
	}

	if window <= 0 {
		window = time.Minute
	}

	width := window / time.Duration(buckets)
	if width < _MIN_BUCKET_WIDTH {
		width = _MIN_BUCKET_WIDTH
	}

	return &TimeSeries{
		width:   width,
		buckets: make([]bucket, buckets),
	}
}

// GetWidth 返回单个时间片的宽度。
func (own *TimeSeries) GetWidth() time.Duration {
	return own.width
}

// Append 记录一次采集快照。
func (own *TimeSeries) Append(at time.Time, snapshot collector.Snapshot) {
	point := Point{
		At:       at,
		Total:    snapshot.Total,
		Failed:   snapshot.Failed,
		AvgMicro: snapshot.GetAverageCostMicro(),
	}

	for _, metric := range snapshot.Metrics {
		if metric.MaxCostMicro > point.MaxMicro {
			point.MaxMicro = metric.MaxCostMicro
		}
	}

	own.AppendPoint(point)
}

// AppendPoint 记录一个聚合点。
//
// 早于窗口起点的样本会被丢弃：环形缓冲只保留最近的数据。
func (own *TimeSeries) AppendPoint(point Point) {
	if point.At.IsZero() {
		return
	}

	own.mu.Lock()
	own.append(point)
	own.mu.Unlock()
}

// append 写入一个聚合点；调用方必须已持有锁。
func (own *TimeSeries) append(point Point) {
	start := point.At.Truncate(own.width)

	if !own.filled {
		own.buckets[0].start = start
		own.filled = true
	}

	origin := own.buckets[0].start
	diff := start.Sub(origin)
	if diff < 0 {
		// 早于窗口：直接丢弃，等待它自然滚出没有意义。
		return
	}

	index := int(diff / own.width)
	if index >= len(own.buckets) {
		own.advance(index)
		index = len(own.buckets) - 1
	}

	current := &own.buckets[index]
	if !current.start.Equal(start) {
		// 槽里是上一轮的旧数据，重新开一个时间片。
		*current = bucket{start: start}
	}

	current.total += point.Total
	current.failed += point.Failed
	current.totalCostMicro += point.AvgMicro * int64(point.Total)
	if point.MaxMicro > current.maxMicro {
		current.maxMicro = point.MaxMicro
	}
}

// advance 向前滚动到指定下标，并丢弃已经滚出窗口的旧时间片。
func (own *TimeSeries) advance(index int) {
	shift := index - len(own.buckets) + 1
	if shift >= len(own.buckets) {
		// 跨度超过整个窗口：全部清空，只保留当前位置。
		for i := range own.buckets {
			own.buckets[i] = bucket{}
		}

		return
	}

	copy(own.buckets, own.buckets[shift:])
	for i := len(own.buckets) - shift; i < len(own.buckets); i++ {
		own.buckets[i] = bucket{}
	}
}

// Query 按 step 聚合 [from, to] 区间内的指标。
//
// 返回结果按时间升序排列，并且始终非 nil，便于直接 JSON 序列化。
func (own *TimeSeries) Query(from, to time.Time, step time.Duration) []Point {
	step = own.normalizeStep(step)

	own.mu.Lock()
	grouped := make(map[int64]*Point, len(own.buckets))
	for _, item := range own.buckets {
		if item.start.IsZero() || item.total == 0 {
			continue
		}

		if item.start.Before(from) || item.start.After(to) {
			continue
		}

		key := item.start.Truncate(step).UnixNano()
		point, ok := grouped[key]
		if !ok {
			point = &Point{At: item.start.Truncate(step)}
			grouped[key] = point
		}

		point.Total += item.total
		point.Failed += item.failed
		point.AvgMicro += item.totalCostMicro
		if item.maxMicro > point.MaxMicro {
			point.MaxMicro = item.maxMicro
		}
	}
	own.mu.Unlock()

	points := make([]Point, 0, len(grouped))
	for _, point := range grouped {
		if point.Total > 0 {
			point.AvgMicro /= int64(point.Total)
		}

		points = append(points, *point)
	}

	sort.Slice(points, func(i, j int) bool {
		return points[i].At.Before(points[j].At)
	})

	return points
}

// normalizeStep 修正聚合步长。
func (own *TimeSeries) normalizeStep(step time.Duration) time.Duration {
	if step <= 0 {
		return own.width
	}

	return step
}
