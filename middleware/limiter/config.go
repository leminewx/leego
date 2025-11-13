package limiter

import (
	"fmt"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/gokit/ggid"
)

type Config struct {
	Id         string        `json:"id"`
	Type       string        `json:"type"`
	Scope      string        `json:"scope"`
	Limit      int           `json:"limit"`
	Requests   int           `json:"requests"`
	IsEnabled  int32         `json:"is_enabled"`
	WindowSize time.Duration `json:"window_size"`
}

// newConfig 新建一个限流器的配置
func newConfig(limit int) *Config {
	return &Config{
		Id:         ggid.New().GetShort(),
		Type:       "limiter",
		Limit:      limit,
		IsEnabled:  1,
		WindowSize: time.Second,
	}
}

// Sync 同步配置
func (own *Config) Sync(config any) error {
	conf, ok := config.(*Config)
	if !ok {
		return fmt.Errorf("leego: limiter: failed to sync config, config is not *limiter.Config")
	}
	if config == nil {
		return fmt.Errorf("leego: limiter: failed to sync config, config is nil")
	}

	own.Limit = conf.Limit
	own.WindowSize = conf.WindowSize
	return nil
}

// Clone 克隆配置信息
func (own *Config) Clone() *Config {
	return &Config{
		Id:        own.Id,
		Type:      "limiter",
		Scope:     own.Scope,
		Limit:     own.Limit,
		IsEnabled: atomic.LoadInt32(&own.IsEnabled),
	}
}
