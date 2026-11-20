package bench

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
	stress "gitee.com/leminewx/leego/harness/stresstest"
	"gitee.com/leminewx/leego/middlewares"
	"gitee.com/leminewx/leego/middlewares/breaker"
	cacheMW "gitee.com/leminewx/leego/middlewares/cache"
	"gitee.com/leminewx/leego/middlewares/compress"
	"gitee.com/leminewx/leego/middlewares/cors"
	"gitee.com/leminewx/leego/middlewares/limiter"
)

const (
	// loadConcurrent 为端到端压测的并发连接数。
	loadConcurrent = 32
	// loadTotalRequests 为每个场景的请求总数。
	//
	// 取值较大是为了拉长采样窗口：进程启动、GC 与调度抖动在数百毫秒的
	// 窗口里会显著歪曲 QPS，而按请求数压测无法跳过前期的预热阶段。
	loadTotalRequests = 100000
)

// loadScenario 描述一组端到端压测配置。
type loadScenario struct {
	// name 为报告中的场景名。
	name string
	// headers 为每个请求附带的头。
	headers map[string]string
	// path 为请求路径，空值时使用 /ping。
	path string
	// h2c 为 true 时客户端只使用明文 HTTP/2。
	h2c bool
	// register 负责注册路由与中间件。
	register func(*leego.Engine)
	// options 为引擎选项（h2c 场景需要）。
	options []leego.EngineOption
}

// loadScenarios 返回待压测的场景列表。
//
// 场景按“从裸框架到真实链路”的顺序排列，便于观察每层能力的吞吐代价。
func loadScenarios() []loadScenario {
	jsonHandle := func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"message": "pong"})
	}

	return []loadScenario{
		{
			name:     "basic-json",
			register: func(engine *leego.Engine) { engine.GET("/ping", jsonHandle, "ping") },
		},
		{
			name: "middleware-stack",
			register: func(engine *leego.Engine) {
				logger := middlewares.NewLoggerWithConfig(&middlewares.LoggerConfig{Output: io.Discard})
				engine.UseMiddlewares(
					logger.AdaptToMiddleware(),
					middlewares.NewRecovery().AdaptToMiddleware(),
					cors.New().AdaptToMiddleware(),
					limiter.NewTokenBucketLimiter(10000000, 10000000).AdaptToMiddleware(),
					breaker.NewCircuitBreaker(breaker.NewDefaultConfig()).AdaptToMiddleware(),
				)
				engine.GET("/ping", jsonHandle, "ping")
			},
			headers: map[string]string{"Origin": "https://example.com"},
		},
		{
			name: "gzip-4kb",
			path: "/report",
			register: func(engine *leego.Engine) {
				payload := "leego performance benchmark payload line. "
				for index := 0; index < 7; index++ {
					payload += payload
				}

				engine.UseMiddlewares(compress.New().AdaptToMiddleware())
				engine.GET("/report", func(ctx *leego.Context) {
					_ = ctx.ResponseText(http.StatusOK, payload)
				}, "report")
			},
			headers: map[string]string{"Accept-Encoding": "gzip"},
		},
		{
			name: "cache-hit",
			register: func(engine *leego.Engine) {
				engine.UseMiddlewares(cacheMW.New().AdaptToMiddleware())
				engine.GET("/ping", jsonHandle, "ping")
			},
		},
		{
			name:    "h2c",
			h2c:     true,
			options: []leego.EngineOption{leego.WithH2C()},
			register: func(engine *leego.Engine) {
				engine.GET("/ping", jsonHandle, "ping")
			},
		},
	}
}

// TestEndToEndLoad 用内置压测客户端测量真实监听下的吞吐与延迟分位。
//
// 该测试需要真实网络与固定负载，使用 -short 时跳过；结果以 PERF 行输出，
// 便于与 docs/performance.md 中的表格对照。
//
// 需要注意两项测量局限：
//   - 压测客户端与服务器在同一进程内，两者共享 CPU，因此 QPS 是端到端参考值，
//     不等于服务端上限，且会随机器后台负载大幅波动；
//   - Windows 上 time.Now 的单调时钟粒度约为 0.5ms，亚毫秒级响应的延迟样本
//     会在 0 与一个粒度之间跳动，分位数只能按粒度倍数解读。
func TestEndToEndLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("端到端压测需要真实网络与固定负载，-short 下跳过")
	}

	for _, scenario := range loadScenarios() {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			options := append([]leego.EngineOption{leego.DisableStartupLog()}, scenario.options...)
			engine := leego.NewEngine(options...)
			scenario.register(engine)
			engine.Prepare()

			address, stop := startTestServer(t, engine)
			defer stop()

			targetPath := scenario.path
			if targetPath == "" {
				targetPath = "/ping"
			}

			client, err := stress.NewClient(&stress.Config{
				Method:        http.MethodGet,
				URL:           "http://" + address + targetPath,
				Headers:       scenario.headers,
				Concurrent:    loadConcurrent,
				TotalRequests: loadTotalRequests,
				Timeout:       5 * time.Second,
				KeepAlive:     true,
			})
			if err != nil {
				t.Fatalf("创建压测客户端失败: %v", err)
			}

			if scenario.h2c {
				client.SetTransport(newH2CTransport(t, address))
			}

			snapshot := client.Run()
			_ = client.Close()

			t.Logf("PERF scenario=%s concurrent=%d total=%d qps=%.0f min=%s avg=%s max=%s p50=%s p90=%s p95=%s p99=%s errors=%d codes=%v",
				scenario.name, loadConcurrent, snapshot.TotalRequests, snapshot.QPS,
				snapshot.MinLatency, snapshot.AvgLatency, snapshot.MaxLatency,
				snapshot.P50Latency, snapshot.P90Latency, snapshot.P95Latency, snapshot.P99Latency,
				snapshot.ErrorCount, snapshot.StatusCodes)

			if snapshot.ErrorCount != 0 {
				t.Fatalf("压测出现网络错误 %d 次", snapshot.ErrorCount)
			}

			if snapshot.SuccessRate < 1 {
				t.Fatalf("成功率 = %.4f，期望 100%%", snapshot.SuccessRate)
			}
		})
	}
}

// startTestServer 在随机端口启动引擎，返回监听地址与关闭函数。
func startTestServer(t *testing.T, engine *leego.Engine) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听测试端口失败: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- engine.RunListener(listener) }()

	// 等待端口可连接，避免把首次建连失败计入压测错误。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := engine.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("RunListener() error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("优雅关闭后 RunListener 未返回")
		}
	}

	return listener.Addr().String(), stop
}

// newH2CTransport 返回只使用明文 HTTP/2 的传输层，并确认协商结果。
func newH2CTransport(t *testing.T, address string) *http.Transport {
	t.Helper()

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols}

	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	response, err := client.Get("http://" + address + "/ping")
	if err != nil {
		t.Fatalf("h2c 探测请求失败: %v", err)
	}

	_ = response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatalf("h2c 协商到的协议 = %s，期望 HTTP/2", response.Proto)
	}

	return transport
}
