package upgrader

import (
	"net/http"
	"testing"
	"time"
)

// TestNewDefaultConfigValues 验证默认配置的关键取值。
func TestNewDefaultConfigValues(t *testing.T) {
	config := NewDefaultConfig()

	if config.ReadTimeout != 60*time.Second {
		t.Fatalf("ReadTimeout = %v, want 60s", config.ReadTimeout)
	}

	if config.WriteTimeout != 10*time.Second {
		t.Fatalf("WriteTimeout = %v, want 10s", config.WriteTimeout)
	}

	if config.MaxReadLimit != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("MaxReadLimit = %d, want %d", config.MaxReadLimit, int64(_DEFAULT_MAX_READ_LIMIT))
	}

	if config.OriginCheckFunc == nil {
		t.Fatal("OriginCheckFunc 不应为 nil")
	}

	if !config.OriginCheckFunc(&http.Request{}) {
		t.Fatal("默认应允许任意来源")
	}

	if len(config.Subprotocols) != 0 {
		t.Fatalf("Subprotocols = %v, want 空", config.Subprotocols)
	}
}

// TestNewUpgraderUsesDefaultsWhenConfigIsNil 验证 nil 配置回退为默认值。
func TestNewUpgraderUsesDefaultsWhenConfigIsNil(t *testing.T) {
	instance := NewUpgrader(nil)

	if instance.ReadTimeout != 60*time.Second {
		t.Fatalf("ReadTimeout = %v, want 60s", instance.ReadTimeout)
	}

	if instance.WriteTimeout != 10*time.Second {
		t.Fatalf("WriteTimeout = %v, want 10s", instance.WriteTimeout)
	}

	if instance.MaxReadLimit != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("MaxReadLimit = %d, want %d", instance.MaxReadLimit, int64(_DEFAULT_MAX_READ_LIMIT))
	}
}

// TestNewUpgraderNormalizesMaxReadLimit 验证非正上限被修正为默认值。
func TestNewUpgraderNormalizesMaxReadLimit(t *testing.T) {
	instance := NewUpgrader(&Config{MaxReadLimit: -1})

	if instance.MaxReadLimit != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("MaxReadLimit = %d, want %d", instance.MaxReadLimit, int64(_DEFAULT_MAX_READ_LIMIT))
	}
}

// TestNewUpgraderCopiesConfig 验证创建后修改原配置不影响升级器。
func TestNewUpgraderCopiesConfig(t *testing.T) {
	config := NewDefaultConfig()
	instance := NewUpgrader(config)

	config.ReadTimeout = time.Second
	config.MaxReadLimit = 1024
	config.Subprotocols = []string{"chat"}

	if instance.ReadTimeout != 60*time.Second {
		t.Fatalf("ReadTimeout 被外部修改影响: %v", instance.ReadTimeout)
	}

	if instance.MaxReadLimit != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("MaxReadLimit 被外部修改影响: %d", instance.MaxReadLimit)
	}

	if len(instance.Subprotocols) != 0 {
		t.Fatalf("Subprotocols 被外部修改影响: %v", instance.Subprotocols)
	}
}
