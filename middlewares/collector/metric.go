package collector

import (
	"sort"
	"time"
)

// Totals 描述全部路由的汇总指标。
type Totals struct {
	// Routes 为参与统计的路由数量。
	Routes int `json:"routes"`
	// Requests 为累计响应次数。
	Requests uint64 `json:"requests"`
	// Flights 为当前在途请求数。
	Flights int64 `json:"flights"`
	// Failed 为 5xx 响应次数。
	Failed uint64 `json:"failed"`
	// TotalCostMicro 为累计响应耗时（微秒）。
	TotalCostMicro int64 `json:"total_cost_micro"`
}

// GetAverageCostMicro 返回全部路由的平均响应耗时（微秒）。
func (own Totals) GetAverageCostMicro() int64 {
	if own.Requests == 0 {
		return 0
	}

	return own.TotalCostMicro / int64(own.Requests)
}

// GetFailureRate 返回全部路由的失败率（0~1）。
func (own Totals) GetFailureRate() float64 {
	if own.Requests == 0 {
		return 0
	}

	return float64(own.Failed) / float64(own.Requests)
}

// Report 描述一次指标汇总结果。
//
// Report 是不可变数据，可直接序列化为 JSON 交给后台页面渲染。
type Report struct {
	// GeneratedAt 为汇总生成时间。
	GeneratedAt time.Time `json:"generated_at"`
	// Routes 为按“方法 + 模式”排序后的路由快照。
	Routes []Snapshot `json:"routes"`
	// Totals 为全局汇总。
	Totals Totals `json:"totals"`
}

// NewReport 基于给定快照生成汇总报告。
//
// 参数 snapshots 会被复制并排序，调用方传入的切片不会被修改；
// 返回的 Routes 始终为非 nil 切片，保证 JSON 输出为 [] 而不是 null。
func NewReport(snapshots []Snapshot) Report {
	routes := make([]Snapshot, len(snapshots))
	copy(routes, snapshots)

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Method != routes[j].Method {
			return routes[i].Method < routes[j].Method
		}

		return routes[i].Pattern < routes[j].Pattern
	})

	report := Report{
		GeneratedAt: time.Now(),
		Routes:      routes,
	}

	report.Totals.Routes = len(routes)
	for _, route := range routes {
		report.Totals.Requests += route.Total
		report.Totals.Flights += route.Flights
		report.Totals.Failed += route.Failed
		report.Totals.TotalCostMicro += route.TotalCostMicro
	}

	return report
}

// NewEmptyReport 生成一个不含任何路由的空报告。
//
// 该函数用于指标后台在引擎尚未注册路由时的兜底响应，
// 避免前端收到 null 而需要额外判空。
func NewEmptyReport() Report {
	return NewReport(nil)
}
