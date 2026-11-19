package harness

import (
	"testing"
	"time"
)

// TestConfigNormalizeFillsDefaults 验证零值配置补齐全部默认值。
func TestConfigNormalizeFillsDefaults(t *testing.T) {
	defaults := NewDefaultConfig()
	config := (Config{}).normalize()

	if config.Addr != defaults.Addr || config.DataDir != defaults.DataDir {
		t.Fatalf("地址或数据目录未补齐: %+v", config)
	}

	if config.SessionTTL != defaults.SessionTTL || config.CacheTTL != defaults.CacheTTL {
		t.Fatalf("周期配置未补齐: %+v", config)
	}

	if config.CacheStale != defaults.CacheStale {
		t.Fatalf("陈旧期 = %s, want %s", config.CacheStale, defaults.CacheStale)
	}

	if config.RateLimit != defaults.RateLimit || config.RateBurst != defaults.RateBurst {
		t.Fatalf("限流配置未补齐: %+v", config)
	}

	if config.MetricsInterval != defaults.MetricsInterval || config.ChunkSize != defaults.ChunkSize {
		t.Fatalf("指标或分片配置未补齐: %+v", config)
	}

	if len(config.SessionSecret) == 0 {
		t.Fatal("会话密钥未补齐")
	}
}

// TestConfigNormalizeKeepsExplicitValues 验证显式配置被保留，负数陈旧期归零。
func TestConfigNormalizeKeepsExplicitValues(t *testing.T) {
	config := Config{
		Addr:            "127.0.0.1:9090",
		DataDir:         t.TempDir(),
		SessionTTL:      time.Minute,
		SessionSecret:   []byte("secret"),
		CacheTTL:        time.Second,
		CacheStale:      -time.Second,
		RateLimit:       7,
		RateBurst:       9,
		MetricsInterval: time.Second,
		ChunkSize:       1024,
		EnableLogging:   true,
	}

	normalized := config.normalize()
	if normalized.Addr != config.Addr || normalized.SessionTTL != config.SessionTTL {
		t.Fatalf("显式配置被改写: %+v", normalized)
	}

	if normalized.CacheStale != 0 {
		t.Fatalf("负数陈旧期应归零: %s", normalized.CacheStale)
	}

	if string(normalized.SessionSecret) != "secret" {
		t.Fatalf("会话密钥被改写: %q", normalized.SessionSecret)
	}
}

// TestConfigCloneCopiesSecret 验证副本修改不会影响原配置。
func TestConfigCloneCopiesSecret(t *testing.T) {
	config := NewDefaultConfig()
	cloned := config.clone()

	cloned.SessionSecret[0] ^= 0xFF
	if config.SessionSecret[0] == cloned.SessionSecret[0] {
		t.Fatal("clone 未深拷贝会话密钥")
	}
}

// TestServiceOptions 验证全部选项函数都写入对应字段。
func TestServiceOptions(t *testing.T) {
	config := NewDefaultConfig()
	for _, option := range []Option{
		UseAddr("127.0.0.1:9999"),
		UseDataDir("data"),
		UseSessionTTL(time.Minute),
		UseSessionSecret([]byte("secret")),
		UseCacheTTL(time.Second),
		UseRateLimit(3, 6),
		UseMetricsInterval(time.Second),
		UseChunkSize(1024),
		UseLogging(false),
	} {
		if option == nil {
			t.Fatal("选项函数不应为 nil")
		}

		option(&config)
	}

	if config.Addr != "127.0.0.1:9999" || config.DataDir != "data" {
		t.Fatalf("地址或数据目录选项未生效: %+v", config)
	}

	if config.SessionTTL != time.Minute || string(config.SessionSecret) != "secret" {
		t.Fatalf("会话选项未生效: %+v", config)
	}

	if config.CacheTTL != time.Second || config.RateLimit != 3 || config.RateBurst != 6 {
		t.Fatalf("缓存或限流选项未生效: %+v", config)
	}

	if config.MetricsInterval != time.Second || config.ChunkSize != 1024 {
		t.Fatalf("指标或分片选项未生效: %+v", config)
	}

	if config.EnableLogging {
		t.Fatal("日志开关未生效")
	}
}
