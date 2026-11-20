package bench

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/config"
	"gitee.com/leminewx/leego/dashboard"
	"gitee.com/leminewx/leego/gateway"
	"gitee.com/leminewx/leego/metrics"
	"gitee.com/leminewx/leego/middlewares/collector"
	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/validate"
)

// benchValidated 是被校验的目标结构。
type benchValidated struct {
	Name  string `validate:"required,min=3,max=32"`
	Email string `validate:"required,email"`
	Age   int    `validate:"min=18,max=99"`
}

// BenchmarkResponseWriterPool 测量响应包装器的取用与归还。
func BenchmarkResponseWriterPool(b *testing.B) {
	pool := response.NewWriterPool()
	writer := newDiscardWriter()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		pool.Release(pool.Acquire(writer))
	}
}

// BenchmarkCollector 测量路由采集器的计数与快照开销。
func BenchmarkCollector(b *testing.B) {
	b.Run("IncreaseDecrease", func(b *testing.B) {
		monitor := collector.NewCollector(http.MethodGet, "/users/:id", "get-user")
		cost := 120 * time.Microsecond

		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			monitor.Increase()
			monitor.Decrease(http.StatusOK, cost)
		}
	})

	b.Run("Snapshot", func(b *testing.B) {
		monitor := collector.NewCollector(http.MethodGet, "/users/:id", "get-user")
		for index := 0; index < 8; index++ {
			monitor.Increase()
			monitor.Decrease(200+index*10, time.Duration(index)*time.Microsecond)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			_ = monitor.GetSnapshot()
		}
	})
}

// BenchmarkValidateCheck 测量结构体标签校验（元信息已缓存）。
func BenchmarkValidateCheck(b *testing.B) {
	target := benchValidated{Name: "leego-bench", Email: "user@example.com", Age: 30}
	if err := validate.Check(&target); err != nil {
		b.Fatalf("校验失败: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		_ = validate.Check(&target)
	}
}

// BenchmarkConfig 测量配置快照的读取开销。
func BenchmarkConfig(b *testing.B) {
	source := config.NewMapSource(map[string]any{
		"host":    "127.0.0.1",
		"port":    8080,
		"enabled": true,
		"ratio":   0.75,
	})
	settings, err := config.Load(source)
	if err != nil {
		b.Fatalf("加载配置失败: %v", err)
	}
	b.Cleanup(func() { _ = settings.Close() })

	b.Run("GetString", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			_ = settings.GetString("host", "")
		}
	})

	b.Run("GetSnapshot", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			_ = settings.GetSnapshot()
		}
	})
}

// BenchmarkMetricsStore 测量指标存储的写入与区间查询。
func BenchmarkMetricsStore(b *testing.B) {
	store := metrics.NewMemoryStore(time.Hour, 60)
	b.Cleanup(func() { _ = store.Close() })

	b.Run("Append", func(b *testing.B) {
		snapshot := collector.Snapshot{Total: 1, Failed: 0, TotalCostMicro: 120}

		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			_ = store.Append(snapshot)
		}
	})

	b.Run("Query", func(b *testing.B) {
		now := time.Now()
		_ = store.AppendPoint(metrics.Point{At: now, Total: 100, Failed: 1, AvgMicro: 120, MaxMicro: 450})

		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			_, _ = store.Query(now.Add(-time.Hour), now, time.Minute)
		}
	})
}

// BenchmarkDashboard 测量指标后台的页面与 JSON 报告开销。
//
// 这类端点的调用频率远低于业务路由，但仍应避免在页面上发生
// 与路由数量成正比的重复序列化。
func BenchmarkDashboard(b *testing.B) {
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.GET("/users/:id", func(ctx *leego.Context) {
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": ctx.GetPathValue("id")})
		}, "get-user")
	})
	// 产生一条指标，避免报告为空。
	engine.ServeHTTP(newDiscardWriter(), newRequest("/users/42"))

	board, err := dashboard.NewDashboard(dashboard.UseProvider(func() collector.Report {
		return engine.CollectReport()
	}))
	if err != nil {
		b.Fatalf("创建指标后台失败: %v", err)
	}

	b.Run("Page", func(b *testing.B) {
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)

		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			board.ServeHTTP(httptest.NewRecorder(), request)
		}
	})

	b.Run("Report", func(b *testing.B) {
		request := httptest.NewRequest(http.MethodGet, "/metrics/api", nil)

		b.ReportAllocs()
		b.ResetTimer()

		for index := 0; index < b.N; index++ {
			board.ServeHTTP(httptest.NewRecorder(), request)
		}
	})
}

// BenchmarkGatewayProxy 测量反向代理转发到本机上游的端到端开销。
//
// 该基准包含一次回环 TCP 往返，用于观察代理层的相对成本，
// 不与纯内存的引擎基准直接相加。
func BenchmarkGatewayProxy(b *testing.B) {
	upstream := httptest.NewServer(http.HandlerFunc(func(resp http.ResponseWriter, _ *http.Request) {
		resp.Header().Set("Content-Type", "application/json")
		_, _ = resp.Write([]byte(`{"ok":true}`))
	}))
	b.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL)
	if err != nil {
		b.Fatalf("解析上游地址失败: %v", err)
	}

	balancer, err := gateway.NewBalancer(gateway.Upstream{URL: target, Weight: 1})
	if err != nil {
		b.Fatalf("创建负载均衡器失败: %v", err)
	}

	proxy := gateway.NewProxy(balancer)
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.GET("/proxy/*path", proxy, "proxy")
	})

	benchRequest(b, engine, newRequest("/proxy/users/42"))
}
