package limiter

import "time"

// Limiter 定义限流器的接口类型
type Limiter interface {
	// Allow 是否运行请求
	Allow() bool
	//	WithLimit 设置流量阈值
	WithLimit(limit int) Limiter
	//	WithWindowSize 设置窗口大小
	WithWindowSize(size time.Duration) Limiter
}
