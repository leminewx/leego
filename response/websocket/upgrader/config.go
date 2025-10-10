package upgrader

import (
	"net/http"
	"time"
)

// Config 定义 WebSocket 配置的数据结构
type Config struct {
	ReadTimeout     time.Duration                // 读超时时间
	WriteTimeout    time.Duration                // 写超时事件
	OriginCheckFunc func(req *http.Request) bool // 来源检查函数
}

// NewDefaultConfig 创建默认配置
func NewDefaultConfig() *Config {
	return &Config{
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 10 * time.Second,
		OriginCheckFunc: func(req *http.Request) bool {
			return true // 默认允许所有来源
		},
	}
}
