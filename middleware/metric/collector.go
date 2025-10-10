package metric

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector 定义指标收集器的数据结构
type Collector struct {
	// 基本信息
	Name    string `json:"name" xml:"name"`       // 路由名称
	Method  string `json:"method" xml:"method"`   // 路由方法
	Pattern string `json:"pattern" xml:"pattern"` // 路由模式

	// 请求指标
	Flights int64 `json:"flights" xml:"flights"` // 正在处理的请求数量

	// 响应指标
	Responses map[int]*Response `json:"responses" xml:"responses"` // 不同状态码对应的响应指标数据

	lock sync.RWMutex
}

// NewCollector 新建一个指标收集器
func NewCollector(name, method, pattern string) *Collector {
	return &Collector{
		Name:      name,
		Method:    method,
		Pattern:   pattern,
		Responses: make(map[int]*Response),
	}
}

// Increase 请求时调用
func (own *Collector) Increase() {
	atomic.AddInt64(&own.Flights, 1) // 正在处理的请求量+1
}

// Decrease 响应时调用
func (own *Collector) Decrease(code int, cost time.Duration) {
	// 更新正在处理的请求量
	atomic.AddInt64(&own.Flights, -1) // 正在处理的请求量-1

	// 更新响应指标数据
	own.lock.Lock()
	resp, ok := own.Responses[code]
	if !ok {
		resp = NewResponse(code)
		own.Responses[code] = resp
	}
	own.lock.Unlock()

	// 更新响应指标数据
	resp.Decrease(cost)
}

// Clone 克隆一个指标收集器
func (own *Collector) Clone() *Collector {
	data := &Collector{
		Name:    own.Name,
		Method:  own.Method,
		Pattern: own.Pattern,
		Flights: atomic.LoadInt64(&own.Flights),
	}

	own.lock.RLock()
	data.Responses = make(map[int]*Response, len(own.Responses))
	for _, resp := range own.Responses {
		data.Responses[resp.Code] = resp.Clone()
	}
	own.lock.RUnlock()

	return data
}
