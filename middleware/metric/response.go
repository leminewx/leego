package metric

import (
	"sync"
	"sync/atomic"
	"time"
)

// Response 定义响应指标的数据结构
type Response struct {
	Code      int    `json:"code" xml:"code"`             // 响应状态码
	Total     uint64 `json:"total" xml:"total"`           // 总处理数量
	MaxCost   uint64 `json:"max_cost" xml:"max_cost"`     // 响应最大耗时（单位：微秒）
	MinCost   uint64 `json:"min_cost" xml:"min_cost"`     // 响应最小耗时（单位：微秒）
	TotalCost uint64 `json:"total_cost" xml:"total_cost"` // 响应总计耗时（单位：微秒）

	lock sync.RWMutex
}

// NewResponse 新建一个响应指标
func NewResponse(code int) *Response {
	return &Response{Code: code}
}

// Decrease 响应时调用
func (own *Response) Decrease(cost time.Duration) {
	// 完成响应
	atomic.AddUint64(&own.Total, 1)

	// 记录响应时间
	responseCost := uint64(cost.Microseconds())
	atomic.AddUint64(&own.TotalCost, responseCost)

	// 记录最大/小响应时间
	own.lock.Lock()
	if responseCost > own.MaxCost {
		own.MaxCost = responseCost
	}
	if own.MinCost == 0 || responseCost < own.MinCost {
		own.MinCost = responseCost
	}
	own.lock.Unlock()
}

// Clone 克隆一个响应指标
func (own *Response) Clone() *Response {
	return &Response{
		Code:      own.Code,
		Total:     atomic.LoadUint64(&own.Total),
		MaxCost:   atomic.LoadUint64(&own.MaxCost),
		MinCost:   atomic.LoadUint64(&own.MinCost),
		TotalCost: atomic.LoadUint64(&own.TotalCost),
	}
}
