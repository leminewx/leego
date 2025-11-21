package collector

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Collector 定义收集器的数据结构
type Collector struct {
	// 基本信息
	Name    string   `json:"name" xml:"name"`       // 路由名称
	Method  string   `json:"method" xml:"method"`   // 路由方法
	Pattern string   `json:"pattern" xml:"pattern"` // 路由模式
	Tags    []string `json:"tags" xml:"tags"`       // 路由标签

	// 启用状态
	Enabled int32 `json:"enabled" xml:"enabled"` // 是否启用

	// 请求指标
	Flights int64 `json:"flights" xml:"flights"` // 正在处理的请求数量

	// 响应指标
	Responses map[int]*response `json:"responses" xml:"responses"` // 不同状态码对应的响应指标数据

	lock sync.RWMutex
}

/*
New 新建一个收集器
  - method：请求方法
  - pattern：路由模式
  - tags：标签，第1个参数为路由名称，后续参数为路由标签（最多只接受前5路由标签）
*/
func New(method, pattern string, tags ...string) *Collector {
	m := &Collector{
		Method:    method,
		Pattern:   pattern,
		Enabled:   1,
		Responses: make(map[int]*response),
	}

	// 过滤标签，若为空，则直接返回
	if tags = TrimAndFilterSpaceString(tags...); len(tags) == 0 {
		return m
	}

	// 获取路由名称
	m.Name = tags[0]

	// 获取路由标签
	if len(tags[1:]) > 5 {
		m.Tags = tags[1:6]
		return m
	}

	m.Tags = tags[1:]
	return m
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
		resp = &response{Code: code}
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
		Tags:    own.Tags, // 标签创建时就确定，后续只读，所有线程安全
		Enabled: atomic.LoadInt32(&own.Enabled),
		Flights: atomic.LoadInt64(&own.Flights),
	}

	own.lock.RLock()
	data.Responses = make(map[int]*response, len(own.Responses))
	for _, resp := range own.Responses {
		data.Responses[resp.Code] = resp.Clone()
	}
	own.lock.RUnlock()

	return data
}

// TrimAndFilterSpaceString 去除前后空格，并删除空字符串
func TrimAndFilterSpaceString(items ...string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}

		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}

	return result
}
