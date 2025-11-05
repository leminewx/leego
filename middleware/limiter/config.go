package limiter

import (
	"time"

	"gitee.com/leminewx/gokit/ggid"
)

type Config struct {
	Id         string        `json:"id"`
	Type       string        `json:"type"`
	Scope      string        `json:"scope"`
	Limit      int           `json:"limit"`
	Requests   int           `json:"requests"`
	WindowSize time.Duration `json:"window_size"`
}

// newConfig 新建一个限流器的配置
func newConfig(limit int) *Config {
	return &Config{
		Id:         ggid.New().GetShort(),
		Type:       "limiter",
		Limit:      limit,
		WindowSize: time.Second,
	}
}

// Clone 克隆配置信息
func (own *Config) Clone() *Config {
	return &Config{
		Id:    own.Id,
		Type:  "limiter",
		Scope: own.Scope,
		Limit: own.Limit,
	}
}

// Sync 同步配置
func (own *Config) Sync(config *Config) {
	own.Limit = config.Limit
	own.WindowSize = config.WindowSize
}
