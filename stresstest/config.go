package stress

import (
	"time"
)

// Config 定义性能测试配置的数据结构
type Config struct {
	Method        string            // 请求方法
	URL           string            // 请求URL
	Headers       map[string]string // 请求头
	Body          string            // 请求体
	Concurrent    int               // 并发请求数
	TotalRequests int               // 总请求数
	Timeout       time.Duration     // 请求超时时间
	KeepAlive     bool              // 是否保持连接
}
