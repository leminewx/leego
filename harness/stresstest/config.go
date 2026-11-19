// Package stress 提供基于 HTTP 的轻量级压力测试工具。
//
// 该包只依赖标准库，适合在本地或 CI 中快速验证服务的吞吐与延迟分布。
// 使用方式：
//
//	config := stress.NewDefaultConfig("http://127.0.0.1:8080/examples")
//	config.Concurrent = 50
//	config.TotalRequests = 5000
//	client, err := stress.NewClient(config)
//	if err != nil { ... }
//	fmt.Println(client.Run().GetReport())
//
// 设计取舍：
//   - 每个 worker 串行发起请求，并发度由 Config.Concurrent 控制，
//     这样延迟统计不会因协程调度而失真；
//   - 所有连接复用同一个 http.Transport，避免压测客户端自身成为瓶颈；
//   - 指标采集使用互斥锁而非原子字段，便于一次性生成一致的快照。
package stress

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// 默认配置常量。
//
// 这些值保证“零值配置”也能安全运行：不会出现除零、负数并发或永久阻塞。
const (
	// _DEFAULT_METHOD 为默认请求方法。
	_DEFAULT_METHOD = "GET"
	// _DEFAULT_CONCURRENT 为默认并发数。
	_DEFAULT_CONCURRENT = 1
	// _DEFAULT_TOTAL_REQUESTS 为默认总请求数。
	_DEFAULT_TOTAL_REQUESTS = 100
	// _DEFAULT_TIMEOUT 为默认单请求超时时间。
	_DEFAULT_TIMEOUT = 30 * time.Second
)

// Config 定义压力测试的输入参数。
type Config struct {
	// Method 为 HTTP 请求方法，空字符串使用 GET。
	Method string
	// URL 为被测地址，必须是包含协议与主机的绝对地址。
	URL string
	// Headers 为附加的请求头，每次请求都会重新设置。
	Headers map[string]string
	// Body 为请求体，非空时所有请求使用同一份内容。
	Body string
	// Concurrent 为并发 worker 数量，小于 1 时取 1。
	Concurrent int
	// TotalRequests 为总请求数，小于 1 时取 100。
	TotalRequests int
	// Timeout 为单个请求的超时时间，非正数时取 30 秒。
	Timeout time.Duration
	// KeepAlive 表示是否复用 TCP 连接，默认 false 以便观察建连开销。
	KeepAlive bool
	// InsecureSkipVerify 表示是否跳过 TLS 证书校验，仅供内网自签证书场景使用。
	InsecureSkipVerify bool
}

// NewDefaultConfig 基于被测地址创建一份带默认值的配置。
//
// 参数 target 为被测 URL；其余字段取默认值，调用方可按需覆盖。
func NewDefaultConfig(target string) *Config {
	return &Config{
		Method:        _DEFAULT_METHOD,
		URL:           target,
		Headers:       make(map[string]string),
		Concurrent:    _DEFAULT_CONCURRENT,
		TotalRequests: _DEFAULT_TOTAL_REQUESTS,
		Timeout:       _DEFAULT_TIMEOUT,
	}
}

// Clone 返回配置的深拷贝，避免压测过程中外部修改导致数据竞争。
func (own *Config) Clone() *Config {
	if own == nil {
		return nil
	}

	clone := *own
	clone.Headers = make(map[string]string, len(own.Headers))
	for key, value := range own.Headers {
		clone.Headers[key] = value
	}

	return &clone
}

// normalize 校验配置并补齐默认值。
//
// 返回的错误只描述不可自动修复的问题（例如缺少 URL 或协议不受支持）；
// 可修复的问题（负数并发、零超时等）会被就地纠正。
func (own *Config) normalize() error {
	if own == nil {
		return errors.New("stress: nil config")
	}

	own.Method = strings.ToUpper(strings.TrimSpace(own.Method))
	if own.Method == "" {
		own.Method = _DEFAULT_METHOD
	}

	if own.URL = strings.TrimSpace(own.URL); own.URL == "" {
		return errors.New("stress: empty target url")
	}

	parsed, err := url.Parse(own.URL)
	if err != nil {
		return fmt.Errorf("stress: invalid target url: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("stress: unsupported scheme %q, want http or https", parsed.Scheme)
	}

	if parsed.Host == "" {
		return errors.New("stress: target url must contain host")
	}

	if own.Concurrent < 1 {
		own.Concurrent = _DEFAULT_CONCURRENT
	}

	if own.TotalRequests < 1 {
		own.TotalRequests = _DEFAULT_TOTAL_REQUESTS
	}

	if own.Timeout <= 0 {
		own.Timeout = _DEFAULT_TIMEOUT
	}

	if own.Headers == nil {
		own.Headers = make(map[string]string)
	}

	return nil
}
