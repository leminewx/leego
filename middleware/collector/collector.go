// Package collector 提供路由级别的请求指标采集能力。
//
// 设计目标：
//   - 并发安全：所有可变字段要么由 atomic 保护，要么由互斥锁保护，二者不混用；
//   - 快照隔离：对外只暴露不可变的 Snapshot 拷贝，调用方无法读到半更新状态；
//   - 零竞态：导出字段全部只读或已移除，避免 json.Marshal 与写入并发导致 DATA RACE。
//
// 典型用法：
//
//	c := collector.NewCollector(http.MethodGet, "/users/:id", "get-user", "vip")
//	c.Increase()
//	// ... 处理请求 ...
//	c.Decrease(http.StatusOK, time.Since(start))
//	snapshot := c.GetSnapshot()
package collector

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// _MAX_TAG_COUNT 定义单个路由最多保留的标签数量。
//
// 标签用于在指标后台按业务分组筛选，数量过多反而降低可读性，
// 因此这里做上限约束；超出的标签会被静默忽略。
const _MAX_TAG_COUNT = 5

// Metric 描述单个响应状态码的聚合指标。
//
// 该结构体是纯数据拷贝，可安全地跨协程传递与序列化。
type Metric struct {
	// Code 为 HTTP 响应状态码。
	Code int `json:"code"`
	// Total 为该状态码的响应次数。
	Total uint64 `json:"total"`
	// MaxCostMicro 为最大响应耗时（微秒）。
	MaxCostMicro int64 `json:"max_cost_micro"`
	// MinCostMicro 为最小响应耗时（微秒）；从未记录时为 0。
	MinCostMicro int64 `json:"min_cost_micro"`
	// TotalCostMicro 为累计响应耗时（微秒）。
	TotalCostMicro int64 `json:"total_cost_micro"`
}

// GetAverageCostMicro 返回该状态码的平均响应耗时（微秒）。
//
// 当没有任何样本时返回 0，避免调用方做除零判断。
func (own Metric) GetAverageCostMicro() int64 {
	if own.Total == 0 {
		return 0
	}

	return own.TotalCostMicro / int64(own.Total)
}

// Snapshot 描述一条路由在某一时刻的只读指标快照。
//
// 快照与采集器完全解耦：采集器继续累加不会影响已经生成的快照，
// 因此可以安全地交给指标后台、Prometheus 导出器或日志协程。
type Snapshot struct {
	// Name 为注册路由时传入的名称（tags 的第一个元素）。
	Name string `json:"name"`
	// Method 为 HTTP 请求方法。
	Method string `json:"method"`
	// Pattern 为注册时的路由模式（含 :param / *param）。
	Pattern string `json:"pattern"`
	// Tags 为注册路由时传入的业务标签。
	Tags []string `json:"tags,omitempty"`
	// Enabled 表示该路由当前是否启用。
	Enabled bool `json:"enabled"`
	// Flights 表示当前正在处理的请求数。
	Flights int64 `json:"flights"`
	// Metrics 为按状态码升序排列的状态码指标。
	Metrics []Metric `json:"metrics"`
	// Total 为累计响应次数。
	Total uint64 `json:"total"`
	// Failed 为 5xx 响应次数，便于快速计算错误率。
	Failed uint64 `json:"failed"`
	// TotalCostMicro 为累计响应耗时（微秒）。
	TotalCostMicro int64 `json:"total_cost_micro"`
}

// GetAverageCostMicro 返回该路由的平均响应耗时（微秒）。
func (own Snapshot) GetAverageCostMicro() int64 {
	if own.Total == 0 {
		return 0
	}

	return own.TotalCostMicro / int64(own.Total)
}

// GetFailureRate 返回该路由的失败率（0~1）。
//
// 失败定义为状态码大于等于 500 的响应。
func (own Snapshot) GetFailureRate() float64 {
	if own.Total == 0 {
		return 0
	}

	return float64(own.Failed) / float64(own.Total)
}

// metric 是采集器内部使用的可变指标。
//
// 全部字段由 Collector.mu 保护。之所以不再使用 atomic，
// 是因为 MaxCost/MinCost 的“读-比较-写”本身就不是单个原子操作，
// 与 atomic 混用反而会出现两种同步机制互相不互斥的问题。
type metric struct {
	code           int
	total          uint64
	maxCostMicro   int64
	minCostMicro   int64
	totalCostMicro int64
}

// update 记录一次响应耗时。
func (own *metric) update(costMicro int64) {
	own.total++
	own.totalCostMicro += costMicro
	if costMicro > own.maxCostMicro {
		own.maxCostMicro = costMicro
	}
	if own.minCostMicro == 0 || costMicro < own.minCostMicro {
		own.minCostMicro = costMicro
	}
}

// Collector 采集单条路由的请求指标。
//
// 并发约定：
//   - enabled 只由 atomic.Bool 访问，保证启停判断无锁；
//   - flights 只由 atomic.Int64 访问，保证进出计数准确；
//   - metrics 只由 mu 保护，保证最大/最小值的一致性。
type Collector struct {
	// name 为路由名称，注册时由 tags[0] 提供。
	name string
	// method 为 HTTP 请求方法。
	method string
	// pattern 为路由模式。
	pattern string
	// tags 为业务标签，创建后只读，因此可被多协程安全共享。
	tags []string
	// enabled 表示路由是否启用。
	enabled atomic.Bool
	// flights 表示当前正在处理的请求数。
	flights atomic.Int64
	// mu 保护 metrics 以及其中的所有字段。
	mu sync.RWMutex
	// metrics 保存“状态码 -> 指标”的映射。
	metrics map[int]*metric
}

// NewCollector 创建一条路由的指标采集器。
//
// 参数：
//   - method：HTTP 请求方法；
//   - pattern：路由模式；
//   - tags：可选标签，第一个非空标签作为路由名称，其余作为业务标签（最多 5 个）。
//
// 采集器创建后默认处于启用状态，与“注册即可观测”的默认策略一致。
func NewCollector(method, pattern string, tags ...string) *Collector {
	own := &Collector{
		method:  method,
		pattern: pattern,
		metrics: make(map[int]*metric, 4),
	}
	own.enabled.Store(true)

	normalized := normalizeTags(tags...)
	if len(normalized) == 0 {
		return own
	}

	own.name = normalized[0]
	if len(normalized) > 1 {
		own.tags = append([]string(nil), normalized[1:min(len(normalized), _MAX_TAG_COUNT+1)]...)
	}

	return own
}

// Enable 启用该路由的指标采集与请求处理。
func (own *Collector) Enable() {
	own.enabled.Store(true)
}

// Disable 禁用该路由的指标采集与请求处理。
func (own *Collector) Disable() {
	own.enabled.Store(false)
}

// IsEnabled 返回该路由当前是否启用。
func (own *Collector) IsEnabled() bool {
	return own.enabled.Load()
}

// GetName 返回路由名称。
func (own *Collector) GetName() string {
	return own.name
}

// GetMethod 返回 HTTP 请求方法。
func (own *Collector) GetMethod() string {
	return own.method
}

// GetPattern 返回路由模式。
func (own *Collector) GetPattern() string {
	return own.pattern
}

// GetTags 返回路由标签的副本。
//
// 返回副本而非内部切片，避免调用方误改导致并发问题。
func (own *Collector) GetTags() []string {
	if len(own.tags) == 0 {
		return nil
	}

	return append([]string(nil), own.tags...)
}

// Increase 在请求开始处理时调用，使在途请求数加一。
func (own *Collector) Increase() {
	own.flights.Add(1)
}

// Decrease 在请求处理结束时调用，记录状态码与耗时。
//
// 参数：
//   - code：响应状态码；为 0 时按 200 处理，避免未写响应体导致指标出现 0；
//   - cost：本次请求耗时。
func (own *Collector) Decrease(code int, cost time.Duration) {
	own.flights.Add(-1)
	if code == 0 {
		code = 200
	}

	costMicro := cost.Microseconds()
	if costMicro < 0 {
		costMicro = 0
	}

	own.mu.Lock()
	item, ok := own.metrics[code]
	if !ok {
		item = &metric{code: code}
		own.metrics[code] = item
	}
	item.update(costMicro)
	own.mu.Unlock()
}

// Reset 清空累计指标，但保留路由元信息与启停状态。
//
// 该函数用于运维后台的“重置统计”操作，调用后所有计数归零。
func (own *Collector) Reset() {
	own.flights.Store(0)

	own.mu.Lock()
	own.metrics = make(map[int]*metric, 4)
	own.mu.Unlock()
}

// GetSnapshot 生成当前时刻的只读快照。
//
// 返回的 Metric 按状态码升序排列，保证 JSON 输出稳定、便于前端展示与断言测试。
func (own *Collector) GetSnapshot() Snapshot {
	snapshot := Snapshot{
		Name:    own.name,
		Method:  own.method,
		Pattern: own.pattern,
		Tags:    own.GetTags(),
		Enabled: own.IsEnabled(),
		Flights: own.flights.Load(),
	}

	own.mu.RLock()
	snapshot.Metrics = make([]Metric, 0, len(own.metrics))
	for _, item := range own.metrics {
		snapshot.Metrics = append(snapshot.Metrics, Metric{
			Code:           item.code,
			Total:          item.total,
			MaxCostMicro:   item.maxCostMicro,
			MinCostMicro:   item.minCostMicro,
			TotalCostMicro: item.totalCostMicro,
		})
		snapshot.Total += item.total
		snapshot.TotalCostMicro += item.totalCostMicro
		if item.code >= 500 {
			snapshot.Failed += item.total
		}
	}
	own.mu.RUnlock()

	sort.Slice(snapshot.Metrics, func(i, j int) bool {
		return snapshot.Metrics[i].Code < snapshot.Metrics[j].Code
	})

	return snapshot
}

// Clone 生成采集器当前状态的独立副本。
//
// 副本不共享任何可变状态，可交给后台页面长期持有；
// 与原采集器之间的读写完全独立。
func (own *Collector) Clone() *Collector {
	clone := &Collector{
		name:    own.name,
		method:  own.method,
		pattern: own.pattern,
		tags:    own.GetTags(),
		metrics: make(map[int]*metric, 4),
	}
	clone.enabled.Store(own.IsEnabled())
	clone.flights.Store(own.flights.Load())

	own.mu.RLock()
	for code, item := range own.metrics {
		copied := *item
		clone.metrics[code] = &copied
	}
	own.mu.RUnlock()

	return clone
}

// normalizeTags 去除标签首尾空白并过滤空标签。
func normalizeTags(tags ...string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			result = append(result, tag)
		}
	}

	return result
}
