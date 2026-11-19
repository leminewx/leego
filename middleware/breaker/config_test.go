package breaker

import (
	"testing"
	"time"
)

// TestNewDefaultConfig 验证默认配置取值。
func TestNewDefaultConfig(t *testing.T) {
	config := NewDefaultConfig()

	if !config.IsEnabled {
		t.Fatal("默认配置必须启用熔断")
	}

	if config.WindowSize != _DEFAULT_WINDOW_SIZE {
		t.Fatalf("WindowSize = %v, want %v", config.WindowSize, _DEFAULT_WINDOW_SIZE)
	}

	if config.MinimumRequests != _DEFAULT_MINIMUM_REQUESTS {
		t.Fatalf("MinimumRequests = %d, want %d", config.MinimumRequests, _DEFAULT_MINIMUM_REQUESTS)
	}

	if config.FailureRate != _DEFAULT_FAILURE_RATE {
		t.Fatalf("FailureRate = %v, want %v", config.FailureRate, _DEFAULT_FAILURE_RATE)
	}

	if config.OpenTimeout != _DEFAULT_OPEN_TIMEOUT {
		t.Fatalf("OpenTimeout = %v, want %v", config.OpenTimeout, _DEFAULT_OPEN_TIMEOUT)
	}

	if config.HalfOpenMaxRequests != _DEFAULT_HALF_OPEN_MAX_REQUESTS {
		t.Fatalf("HalfOpenMaxRequests = %d, want %d", config.HalfOpenMaxRequests, _DEFAULT_HALF_OPEN_MAX_REQUESTS)
	}

	if config.HalfOpenSuccessThreshold != _DEFAULT_HALF_OPEN_SUCCESS_THRESHOLD {
		t.Fatalf("HalfOpenSuccessThreshold = %d, want %d", config.HalfOpenSuccessThreshold, _DEFAULT_HALF_OPEN_SUCCESS_THRESHOLD)
	}
}

// TestNormalize 验证非法配置会被修正为安全值。
func TestNormalize(t *testing.T) {
	invalid := Config{
		WindowSize:               -time.Second,
		MinimumRequests:          0,
		FailureRate:              1.5,
		OpenTimeout:              -time.Minute,
		HalfOpenMaxRequests:      -1,
		HalfOpenSuccessThreshold: 0,
	}

	normalized := invalid.Normalize()

	if normalized.WindowSize != _DEFAULT_WINDOW_SIZE {
		t.Fatalf("WindowSize = %v, want %v", normalized.WindowSize, _DEFAULT_WINDOW_SIZE)
	}

	if normalized.MinimumRequests != 1 {
		t.Fatalf("MinimumRequests = %d, want 1", normalized.MinimumRequests)
	}

	if normalized.FailureRate != _DEFAULT_FAILURE_RATE {
		t.Fatalf("FailureRate = %v, want %v", normalized.FailureRate, _DEFAULT_FAILURE_RATE)
	}

	if normalized.OpenTimeout != _DEFAULT_OPEN_TIMEOUT {
		t.Fatalf("OpenTimeout = %v, want %v", normalized.OpenTimeout, _DEFAULT_OPEN_TIMEOUT)
	}

	if normalized.HalfOpenMaxRequests != 1 {
		t.Fatalf("HalfOpenMaxRequests = %d, want 1", normalized.HalfOpenMaxRequests)
	}

	if normalized.HalfOpenSuccessThreshold != 1 {
		t.Fatalf("HalfOpenSuccessThreshold = %d, want 1", normalized.HalfOpenSuccessThreshold)
	}

	// Normalize 不应修改原始值（值语义，副本返回）。
	if invalid.MinimumRequests != 0 {
		t.Fatal("Normalize 不应修改调用方配置")
	}
}

// TestNormalizeKeepsValidValues 验证合法配置保持不变。
func TestNormalizeKeepsValidValues(t *testing.T) {
	valid := Config{
		IsEnabled:                true,
		WindowSize:               3 * time.Second,
		MinimumRequests:          5,
		FailureRate:              0.25,
		OpenTimeout:              2 * time.Second,
		HalfOpenMaxRequests:      2,
		HalfOpenSuccessThreshold: 2,
	}

	normalized := valid.Normalize()
	if normalized != valid {
		t.Fatalf("Normalize(%+v) = %+v, want 原样返回", valid, normalized)
	}
}

// TestGetConfig 验证配置查询返回创建时的副本。
func TestGetConfig(t *testing.T) {
	config := NewDefaultConfig()
	config.MinimumRequests = 7

	gate := NewCircuitBreaker(config)
	if got := gate.GetConfig().MinimumRequests; got != 7 {
		t.Fatalf("MinimumRequests = %d, want 7", got)
	}
}
