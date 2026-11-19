package stress

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// errNilConfig 表示调用方未提供配置。
var errNilConfig = errors.New("stress: nil config")

// Client 是一个可复用的 HTTP 压测客户端。
//
// 一个 Client 只持有一个 http.Transport，因此多次调用 Run 会复用连接池；
// 如果需要隔离不同场景的连接，请分别创建 Client。
type Client struct {
	// config 为规范化后的配置副本，运行期只读。
	config Config
	// transport 为底层连接池，保存以便 Close 时释放空闲连接。
	transport *http.Transport
	// client 为执行请求的 HTTP 客户端。
	client *http.Client
}

// workerResult 汇总单个 worker 的采集结果。
//
// 每个 worker 独占一份结果，避免压测过程中的锁竞争；
// 全部 worker 结束后再合并为最终指标。
type workerResult struct {
	// total 为请求总数。
	total int64
	// success 为成功请求数。
	success int64
	// failed 为失败请求数。
	failed int64
	// errors 为网络错误数。
	errors int64
	// latencies 为延迟样本。
	latencies *Latencies
	// statusCodes 为状态码分布。
	statusCodes map[int]int64
}

// NewClient 基于配置创建压测客户端。
//
// 参数 config 允许为 nil，此时返回错误；配置中的非法值会被自动纠正，
// 不可修复的问题（缺少 URL、协议不支持等）通过 error 返回。
func NewClient(config *Config) (*Client, error) {
	if config == nil {
		return nil, errNilConfig
	}

	normalized := config.Clone()
	if err := normalized.normalize(); err != nil {
		return nil, err
	}

	transport := &http.Transport{
		MaxIdleConns:        normalized.Concurrent,
		MaxIdleConnsPerHost: normalized.Concurrent,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   !normalized.KeepAlive,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: normalized.InsecureSkipVerify,
			MinVersion:         tls.VersionTLS12,
		},
	}

	return &Client{
		config:    *normalized,
		transport: transport,
		client: &http.Client{
			Transport: transport,
			Timeout:   normalized.Timeout,
		},
	}, nil
}

// SetTransport 使用调用方提供的连接池替换内部连接池。
//
// 参数 transport 为 nil 时忽略，便于链式调用时传入可选值。
func (own *Client) SetTransport(transport *http.Transport) {
	if transport == nil {
		return
	}

	own.transport = transport
	own.client.Transport = transport
}

// GetConfig 返回客户端配置的副本。
func (own *Client) GetConfig() Config {
	return *own.config.Clone()
}

// GetHTTPClient 返回底层 HTTP 客户端，便于注入自定义 RoundTripper 或中间件。
func (own *Client) GetHTTPClient() *http.Client {
	return own.client
}

// Run 执行一次完整的压测并返回指标快照。
//
// 请求会按配置均匀分配到各个 worker，余数依次分配给前若干个 worker，
// 保证实际请求数等于 Config.TotalRequests。方法阻塞直到全部请求完成。
func (own *Client) Run() MetricsSnapshot {
	metrics := NewMetrics()

	requestsPerWorker := own.config.TotalRequests / own.config.Concurrent
	remainder := own.config.TotalRequests % own.config.Concurrent

	results := make([]workerResult, own.config.Concurrent)

	var waitGroup sync.WaitGroup
	for index := 0; index < own.config.Concurrent; index++ {
		count := requestsPerWorker
		if index < remainder {
			count++
		}

		if count == 0 {
			results[index].latencies = NewLatencies()
			results[index].statusCodes = make(map[int]int64)
			continue
		}

		waitGroup.Go(func() {
			result := &results[index]
			result.latencies = NewLatenciesWithCapacity(count)
			result.statusCodes = make(map[int]int64, 4)
			own.runWorker(count, result)
		})
	}

	waitGroup.Wait()

	for index := range results {
		metrics.merge(&results[index])
	}

	metrics.Finish()

	return metrics.GetSnapshot()
}

// runWorker 在单个 worker 内串行发起 count 次请求，并把结果写入 result。
func (own *Client) runWorker(count int, result *workerResult) {
	for index := 0; index < count; index++ {
		start := time.Now()

		request, err := own.newRequest()
		if err != nil {
			result.record(0, time.Since(start), err)
			continue
		}

		response, err := own.client.Do(request)
		latency := time.Since(start)

		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}

		result.record(statusCode, latency, err)
	}
}

// newRequest 构造一次请求；请求体每次重新创建，保证可重复发送。
func (own *Client) newRequest() (*http.Request, error) {
	var body io.Reader
	if own.config.Body != "" {
		body = strings.NewReader(own.config.Body)
	}

	request, err := http.NewRequest(own.config.Method, own.config.URL, body)
	if err != nil {
		return nil, err
	}

	for key, value := range own.config.Headers {
		request.Header.Set(key, value)
	}

	return request, nil
}

// record 记录单次请求结果。
func (own *workerResult) record(statusCode int, latency time.Duration, err error) {
	own.total++
	own.latencies.Append(latency)
	own.statusCodes[statusCode]++

	if err == nil && statusCode > 0 && statusCode < 400 {
		own.success++
		return
	}

	own.failed++
	if err != nil {
		own.errors++
	}
}

// merge 将 worker 结果合并到指标收集器。
func (own *Metrics) merge(result *workerResult) {
	if result == nil {
		return
	}

	own.lock.Lock()
	defer own.lock.Unlock()

	own.totalRequests += result.total
	own.successCount += result.success
	own.failedCount += result.failed
	own.errorCount += result.errors

	if own.latencies == nil {
		own.latencies = NewLatencies()
	}

	own.latencies.Merge(result.latencies)

	if own.statusCodes == nil {
		own.statusCodes = make(map[int]int64)
	}

	for code, count := range result.statusCodes {
		own.statusCodes[code] += count
	}
}

// Close 释放连接池中的空闲连接。
//
// 压测结束后调用可避免 TIME_WAIT 堆积；重复调用是安全的。
func (own *Client) Close() error {
	if own.transport != nil {
		own.transport.CloseIdleConnections()
	}

	return nil
}
