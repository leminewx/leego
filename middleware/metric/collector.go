package metric

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector 定义收集器的数据结构
type Collector struct {
	Method  string `json:"method" xml:"method"`   // 路由方法
	Pattern string `json:"pattern" xml:"pattern"` // 路由模式

	// 请求量
	Total   uint64 `json:"total" xml:"total"`     // 总请求量
	Flights int64  `json:"flights" xml:"flights"` // 正在处理的请求量

	// 响应指标
	ResponseTotalCost uint64         `json:"response_total_cost" xml:"response_total_cost"` // 响应总计耗时（单位：微秒）
	ResponseMaxCost   uint64         `json:"response_max_cost" xml:"response_max_cost"`     // 响应最大耗时（单位：微秒）
	ResponseMinCost   uint64         `json:"response_min_cost" xml:"response_min_cost"`     // 响应最小耗时（单位：微秒）
	StatusCodeMap     map[int]uint64 `json:"status_code_map" xml:"status_code_map"`         // 不同状态码对应的响应数量

	lock sync.RWMutex
}

// NewCollector 新建一个请求指标收集器
func NewCollector(method, pattern string) *Collector {
	return &Collector{
		Method:        method,
		Pattern:       pattern,
		StatusCodeMap: map[int]uint64{},
	}
}

// Increase 请求时调用
func (own *Collector) Increase() {
	atomic.AddUint64(&own.Total, 1)  // 接收的总请求量+1
	atomic.AddInt64(&own.Flights, 1) // 正在处理的请求量+1
}

// Decrease 响应时调用
func (own *Collector) Decrease(code int, cost time.Duration) {
	// 刷新正在处理的请求量
	atomic.AddInt64(&own.Flights, -1) // 正在处理的请求量-1

	// 记录响应时间
	responseCost := uint64(cost.Microseconds())
	atomic.AddUint64(&own.ResponseTotalCost, responseCost)

	// 记录最大/小响应时间
	own.lock.Lock()
	if responseCost > own.ResponseMaxCost {
		own.ResponseMaxCost = responseCost
	}
	if own.ResponseMinCost == 0 || responseCost < own.ResponseMinCost {
		own.ResponseMinCost = responseCost
	}

	// 记录状态码
	if status, ok := own.StatusCodeMap[code]; !ok {
		own.StatusCodeMap[code] = 1
	} else {
		status++
		own.StatusCodeMap[code] = status
	}
	own.lock.Unlock()
}

// Clone 克隆一个请求指标的收集器
func (own *Collector) Clone() *Collector {
	data := &Collector{
		Method:            own.Method,
		Pattern:           own.Pattern,
		Total:             atomic.LoadUint64(&own.Total),
		Flights:           atomic.LoadInt64(&own.Flights),
		ResponseMaxCost:   atomic.LoadUint64(&own.ResponseMaxCost),
		ResponseMinCost:   atomic.LoadUint64(&own.ResponseMinCost),
		ResponseTotalCost: atomic.LoadUint64(&own.ResponseTotalCost),
	}

	own.lock.RLock()
	data.StatusCodeMap = make(map[int]uint64, len(own.StatusCodeMap))
	for k, v := range own.StatusCodeMap {
		data.StatusCodeMap[k] = v
	}
	own.lock.RUnlock()

	return data
}
