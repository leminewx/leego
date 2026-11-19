package stress

import (
	"testing"
	"time"
)

// TestConfigNormalizeTrimsValues 验证方法名与地址被裁剪并补默认值。
func TestConfigNormalizeTrimsValues(t *testing.T) {
	config := &Config{
		Method: " post ",
		URL:    "  http://example.com/items  ",
	}

	if err := config.normalize(); err != nil {
		t.Fatalf("normalize 失败: %v", err)
	}

	if config.Method != "POST" {
		t.Fatalf("方法 = %q, want POST", config.Method)
	}

	if config.URL != "http://example.com/items" {
		t.Fatalf("地址 = %q", config.URL)
	}

	if config.Concurrent != _DEFAULT_CONCURRENT || config.TotalRequests != _DEFAULT_TOTAL_REQUESTS {
		t.Fatalf("并发或请求数未补默认值: %+v", config)
	}
}

// TestConfigNormalizeRejectsMissingHost 验证缺少主机的地址被拒绝。
func TestConfigNormalizeRejectsMissingHost(t *testing.T) {
	config := &Config{URL: "http:/only-path"}
	if err := config.normalize(); err == nil {
		t.Fatal("缺少主机的地址应返回错误")
	}
}

// TestConfigNormalizeInitializesHeaders 验证请求头集合与超时被补齐。
func TestConfigNormalizeInitializesHeaders(t *testing.T) {
	config := &Config{URL: "http://example.com"}

	if err := config.normalize(); err != nil {
		t.Fatalf("normalize 失败: %v", err)
	}

	if config.Headers == nil {
		t.Fatal("Headers 应被初始化")
	}

	if config.Timeout != _DEFAULT_TIMEOUT {
		t.Fatalf("超时 = %s, want %s", config.Timeout, _DEFAULT_TIMEOUT)
	}
}

// TestConfigClonePreservesTimeout 验证副本保留超时等标量配置。
func TestConfigClonePreservesTimeout(t *testing.T) {
	config := &Config{URL: "http://example.com", Timeout: time.Second}
	cloned := config.Clone()

	if cloned.Timeout != time.Second || cloned.URL != config.URL {
		t.Fatalf("副本配置异常: %+v", cloned)
	}
}
