package collector

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector 定义收集器的数据结构
type Collector struct {
	// 请求量
	Total    int64 `json:"total" xml:"total"`         // 总请求量
	InFlight int64 `json:"in_flight" xml:"in_flight"` // 正在处理的请求量

	// 响应指标
	ResponseCount int64       `json:"response_count" xml:"response_count"` // 响应总数量
	ResponseCost  int64       `json:"response_cost" xml:"response_cost"`   // 响应总耗时
	StatusCodes   map[int]int `json:"status_codes" xml:"status_codes"`     // 不同状态码对应的响应数量

	lock sync.RWMutex
}

// New 新建一个收集器
func New() *Collector {
	return &Collector{StatusCodes: map[int]int{}}
}

// Increase 请求时调用
func (own *Collector) Increase() {
	atomic.AddInt64(&own.InFlight, 1)
}

// Decrease 响应时调用
func (own *Collector) Decrease(code int, cost time.Duration) {
	// 记录请求量
	atomic.AddInt64(&own.Total, 1)
	atomic.AddInt64(&own.InFlight, -1)

	// 记录响应时间
	responseCost := cost.Nanoseconds()
	atomic.AddInt64(&own.ResponseCost, responseCost)
	atomic.AddInt64(&own.ResponseCount, 1)

	// 记录状态码
	own.lock.Lock()
	if status, ok := own.StatusCodes[code]; !ok {
		own.StatusCodes[code] = 1
	} else {
		status++
		own.StatusCodes[code] = status
	}
	own.lock.Unlock()
}

// Clone 克隆一个收集器
func (own *Collector) Clone() *Collector {
	data := &Collector{
		Total:         atomic.LoadInt64(&own.Total),
		InFlight:      atomic.LoadInt64(&own.InFlight),
		ResponseCost:  atomic.LoadInt64(&own.ResponseCost),
		ResponseCount: atomic.LoadInt64(&own.ResponseCount),
	}

	own.lock.RLock()
	data.StatusCodes = make(map[int]int, len(own.StatusCodes))
	for k, v := range own.StatusCodes {
		data.StatusCodes[k] = v
	}
	own.lock.RUnlock()

	return data
}
