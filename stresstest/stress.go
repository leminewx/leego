package stress

import (
	"crypto/tls"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	config    *Config
	metrics   *Metrics
	client    *http.Client
	transport *http.Transport
}

func NewClient(cfg *Config) *Client {
	client := &Client{
		config: cfg,
		transport: &http.Transport{
			MaxIdleConns:        cfg.Concurrent,
			MaxIdleConnsPerHost: cfg.Concurrent,
			IdleConnTimeout:     100 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives:   !cfg.KeepAlive,
		},
	}

	client.client = &http.Client{
		Transport: client.transport,
		Timeout:   cfg.Timeout,
	}

	return client
}

func (own *Client) WithTransport(transport *http.Transport) *Client {
	return &Client{
		config:    own.config,
		metrics:   NewMetrics(),
		transport: transport,
		client: &http.Client{
			Transport: transport,
			Timeout:   own.config.Timeout,
		},
	}
}

// Run 运行负载测试
func (own *Client) Run() string {
	own.metrics = NewMetrics()
	requestsPerWorker := own.config.TotalRequests / own.config.Concurrent

	var wg sync.WaitGroup
	for i := 0; i < own.config.Concurrent; i++ {
		wg.Go(func() {
			for j := 0; j < requestsPerWorker; j++ {
				start := time.Now()

				req, err := http.NewRequest(own.config.Method, own.config.URL, nil)
				if err != nil {
					own.metrics.Record(0, time.Since(start), err)
					continue
				}

				// 设置请求头
				for key, value := range own.config.Headers {
					req.Header.Set(key, value)
				}

				resp, err := own.client.Do(req)
				latency := time.Since(start)

				var statusCode int
				if resp != nil {
					statusCode = resp.StatusCode
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}

				own.metrics.Record(statusCode, latency, err)
			}
		})
	}

	wg.Wait()
	own.metrics.EndTime = time.Now()
	own.metrics.Calculate()

	return own.metrics.Report()
}
