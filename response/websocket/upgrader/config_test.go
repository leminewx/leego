package upgrader

import (
	"net/http"
	"net/http/httptest"
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

	// Origin 缺失（非浏览器客户端）时放行。
	if !config.OriginCheckFunc(&http.Request{}) {
		t.Fatal("无 Origin 的请求应被允许")
	}

	// 默认只允许同源，避免跨站 WebSocket 劫持（CSWSH）。
	crossSite := httptest.NewRequest(http.MethodGet, "/ws", nil)
	crossSite.Host = "example.com"
	crossSite.Header.Set("Origin", "https://evil.example.net")
	if config.OriginCheckFunc(crossSite) {
		t.Fatal("跨站 Origin 必须被拒绝")
	}

	sameSite := httptest.NewRequest(http.MethodGet, "/ws", nil)
	sameSite.Host = "example.com"
	sameSite.Header.Set("Origin", "https://EXAMPLE.com")
	if !config.OriginCheckFunc(sameSite) {
		t.Fatal("同源 Origin 应被允许（主机名大小写不敏感）")
	}

	if len(config.Subprotocols) != 0 {
		t.Fatalf("Subprotocols = %v, want 空", config.Subprotocols)
	}
}

// TestNewUsesDefaultsWhenConfigIsNil 验证 nil 配置回退为默认值。
func TestNewUsesDefaultsWhenConfigIsNil(t *testing.T) {
	instance := New(nil)

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

// TestNewNormalizesMaxReadLimit 验证非正上限被修正为默认值。
func TestNewNormalizesMaxReadLimit(t *testing.T) {
	instance := New(&Config{MaxReadLimit: -1})

	if instance.MaxReadLimit != _DEFAULT_MAX_READ_LIMIT {
		t.Fatalf("MaxReadLimit = %d, want %d", instance.MaxReadLimit, int64(_DEFAULT_MAX_READ_LIMIT))
	}
}

// TestNewCopiesConfig 验证创建后修改原配置不影响升级器。
func TestNewCopiesConfig(t *testing.T) {
	config := NewDefaultConfig()
	instance := New(config)

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
