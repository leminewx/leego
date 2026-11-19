package leego

import (
	"net/http"
	"testing"
	"time"

	"gitee.com/leminewx/leego/metrics"
)

// TestMetricsConfigNormalize 验证零值配置回退到默认值。
func TestMetricsConfigNormalize(t *testing.T) {
	config := MetricsConfig{}.normalize()

	if config.Prefix != _DEFAULT_METRICS_PREFIX {
		t.Fatalf("Prefix = %q, want %q", config.Prefix, _DEFAULT_METRICS_PREFIX)
	}

	if config.Interval != _DEFAULT_METRICS_INTERVAL {
		t.Fatalf("Interval = %v, want %v", config.Interval, _DEFAULT_METRICS_INTERVAL)
	}

	if config.Step != _DEFAULT_METRICS_STEP {
		t.Fatalf("Step = %v, want %v", config.Step, _DEFAULT_METRICS_STEP)
	}
}

// TestMetricsOptions 验证选项只接受合法值。
func TestMetricsOptions(t *testing.T) {
	config := MetricsConfig{}
	UseMetricsInterval(3 * time.Second)(&config)
	UseMetricsInterval(-time.Second)(&config)
	UseMetricsStep(2 * time.Second)(&config)
	UseMetricsStep(0)(&config)
	DisableMetricsPrometheus()(&config)

	if config.Interval != 3*time.Second {
		t.Fatalf("Interval = %v, want 3s", config.Interval)
	}

	if config.Step != 2*time.Second {
		t.Fatalf("Step = %v, want 2s", config.Step)
	}

	if !config.DisablePrometheus {
		t.Fatal("DisablePrometheus = false, want true")
	}
}

// TestAppendMetricsWritesTotals 验证后台采集写入全局汇总。
func TestAppendMetricsWritesTotals(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	}, "ping")

	for range 3 {
		if recorder := performRequest(engine, http.MethodGet, "/ping", nil); recorder.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, want 200", recorder.Code)
		}
	}

	store := metrics.NewMemoryStore(time.Minute, 60)
	engine.appendMetrics(store)

	now := time.Now()
	points, err := store.Query(now.Add(-time.Minute), now, time.Second)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if len(points) == 0 {
		t.Fatal("appendMetrics() 未写入任何时间片")
	}

	if total := points[len(points)-1].Total; total < 3 {
		t.Fatalf("汇总 Total = %d, want >= 3", total)
	}
}
