package breaker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

// createTestBreaker 构造一个用于测试的熔断器。
//
// 参数 minimumRequests 控制触发评估所需的最小样本数，
// 其余参数取较小值以便在测试中快速完成状态迁移。
func createTestBreaker(minimumRequests int64, openTimeout time.Duration) *CircuitBreaker {
	config := NewDefaultConfig()
	config.MinimumRequests = minimumRequests
	config.FailureRate = 0.5
	config.WindowSize = time.Minute
	config.OpenTimeout = openTimeout
	config.HalfOpenMaxRequests = 1
	config.HalfOpenSuccessThreshold = 3
	return NewCircuitBreaker(config)
}

// TestNewCircuitBreaker 验证默认配置与初始状态。
func TestNewCircuitBreaker(t *testing.T) {
	standard := NewCircuitBreaker(NewDefaultConfig())

	if !standard.IsEnabled() {
		t.Fatal("默认配置必须启用熔断")
	}

	if standard.GetState() != STATE_CLOSED {
		t.Fatalf("初始状态 = %v, want %v", standard.GetState(), STATE_CLOSED)
	}

	zero := NewCircuitBreaker(Config{})
	if zero.GetConfig().MinimumRequests != 1 {
		t.Fatalf("零值配置的最小样本数 = %d, want 1", zero.GetConfig().MinimumRequests)
	}
}

// TestExecuteSuccess 验证成功调用不会打开熔断器。
func TestExecuteSuccess(t *testing.T) {
	gate := createTestBreaker(2, time.Minute)

	for index := 0; index < 10; index++ {
		if err := gate.Execute(func() error { return nil }); err != nil {
			t.Fatalf("第 %d 次 Execute() error = %v", index+1, err)
		}
	}

	if gate.GetState() != STATE_CLOSED {
		t.Fatalf("全部成功后状态 = %v, want %v", gate.GetState(), STATE_CLOSED)
	}

	snapshot := gate.GetMetricsSnapshot()
	if snapshot.TotalRequests != 10 || snapshot.FailedRequests != 0 {
		t.Fatalf("指标快照 = %+v, want total=10 failed=0", snapshot)
	}
}

// TestExecuteNilCall 验证 nil 回调返回错误而非 panic。
func TestExecuteNilCall(t *testing.T) {
	gate := createTestBreaker(1, time.Minute)

	if err := gate.Execute(nil); err == nil {
		t.Fatal("Execute(nil) 必须返回错误")
	}

	if err := gate.ExecuteWithContext(nil, nil); err == nil {
		t.Fatal("ExecuteWithContext(nil, nil) 必须返回错误")
	}
}

// TestOpenAfterFailureRateExceeded 验证失败率达标后进入打开状态。
func TestOpenAfterFailureRateExceeded(t *testing.T) {
	gate := createTestBreaker(4, time.Minute)
	failure := errors.New("downstream failure")

	for index := 0; index < 4; index++ {
		if err := gate.Execute(func() error { return failure }); !errors.Is(err, failure) {
			t.Fatalf("第 %d 次 Execute() error = %v, want %v", index+1, err, failure)
		}
	}

	if gate.GetState() != STATE_OPEN {
		t.Fatalf("状态 = %v, want %v", gate.GetState(), STATE_OPEN)
	}

	if err := gate.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitBreakerIsOpen) {
		t.Fatalf("打开后 Execute() error = %v, want ErrCircuitBreakerIsOpen", err)
	}

	snapshot := gate.GetMetricsSnapshot()
	if snapshot.BlockedRequests != 1 {
		t.Fatalf("被拒绝请求数 = %d, want 1", snapshot.BlockedRequests)
	}

	if snapshot.WindowFailures != 4 || snapshot.WindowFailureRate != 1 {
		t.Fatalf("窗口统计 = (%d, %v), want (4, 1)", snapshot.WindowFailures, snapshot.WindowFailureRate)
	}
}

// TestHalfOpenRecoversToClosed 验证半开状态连续成功后恢复闭合。
func TestHalfOpenRecoversToClosed(t *testing.T) {
	gate := createTestBreaker(1, 20*time.Millisecond)

	if err := gate.Execute(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("期望执行失败")
	}

	if gate.GetState() != STATE_OPEN {
		t.Fatalf("状态 = %v, want %v", gate.GetState(), STATE_OPEN)
	}

	time.Sleep(40 * time.Millisecond)

	for index := 0; index < 3; index++ {
		if err := gate.Execute(func() error { return nil }); err != nil {
			t.Fatalf("半开第 %d 次探测 error = %v", index+1, err)
		}
	}

	if gate.GetState() != STATE_CLOSED {
		t.Fatalf("连续成功后状态 = %v, want %v", gate.GetState(), STATE_CLOSED)
	}
}

// TestHalfOpenFailureReopens 验证半开状态一旦失败立即重新打开。
func TestHalfOpenFailureReopens(t *testing.T) {
	gate := createTestBreaker(1, 20*time.Millisecond)

	_ = gate.Execute(func() error { return errors.New("boom") })
	time.Sleep(40 * time.Millisecond)

	if err := gate.Execute(func() error { return errors.New("still broken") }); err == nil {
		t.Fatal("半开探测必须返回原始错误")
	}

	if gate.GetState() != STATE_OPEN {
		t.Fatalf("状态 = %v, want %v", gate.GetState(), STATE_OPEN)
	}
}

// TestHalfOpenLimitsProbes 验证半开状态下的并发探测上限。
func TestHalfOpenLimitsProbes(t *testing.T) {
	gate := createTestBreaker(1, 10*time.Millisecond)

	_ = gate.Execute(func() error { return errors.New("boom") })
	time.Sleep(30 * time.Millisecond)

	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)

	go func() {
		defer waitGroup.Done()
		_ = gate.Execute(func() error {
			close(probeStarted)
			<-probeRelease
			return nil
		})
	}()

	<-probeStarted

	if err := gate.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitBreakerIsOpen) {
		t.Fatalf("超出探测上限时 error = %v, want ErrCircuitBreakerIsOpen", err)
	}

	close(probeRelease)
	waitGroup.Wait()
}

// TestGetMiddlewareBlocksWhenOpen 验证中间件在打开状态返回 503。
func TestGetMiddlewareBlocksWhenOpen(t *testing.T) {
	gate := createTestBreaker(1, time.Minute)
	_ = gate.Execute(func() error { return errors.New("boom") })

	called := false
	handle := gate.GetMiddleware()(func(ctx *leego.Context) {
		called = true
	})

	recorder := httptest.NewRecorder()
	ctx := leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	handle(ctx)

	if called {
		t.Fatal("熔断打开时不应执行业务处理函数")
	}

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, want 503", recorder.Code)
	}
}

// TestGetMiddlewareRecordsStatus 验证中间件按响应状态码记录成功/失败。
func TestGetMiddlewareRecordsStatus(t *testing.T) {
	gate := createTestBreaker(2, time.Minute)

	handle := gate.GetMiddleware()(func(ctx *leego.Context) {
		ctx.ResponseError(http.StatusBadGateway, "upstream error")
	})

	recorder := httptest.NewRecorder()
	ctx := leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	handle(ctx)

	snapshot := gate.GetMetricsSnapshot()
	if snapshot.FailedRequests != 1 {
		t.Fatalf("失败请求数 = %d, want 1", snapshot.FailedRequests)
	}

	if snapshot.WindowFailures != 1 {
		t.Fatalf("窗口失败数 = %d, want 1", snapshot.WindowFailures)
	}
}

// TestGetMiddlewareBypassesWhenDisabled 验证禁用后中间件直接放行。
func TestGetMiddlewareBypassesWhenDisabled(t *testing.T) {
	gate := createTestBreaker(1, time.Minute)
	_ = gate.Execute(func() error { return errors.New("boom") })
	gate.Disable()

	called := false
	handle := gate.GetMiddleware()(func(ctx *leego.Context) {
		called = true
		ctx.ResponseText(http.StatusOK, "ok")
	})

	recorder := httptest.NewRecorder()
	ctx := leego.NewContext(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	handle(ctx)

	if !called {
		t.Fatal("禁用熔断后必须执行业务处理函数")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	gate.Enable()
	if !gate.IsEnabled() {
		t.Fatal("Enable 后必须处于启用状态")
	}
}

// TestSetSuccessChecker 验证自定义成功判定与恢复默认。
func TestSetSuccessChecker(t *testing.T) {
	gate := createTestBreaker(2, time.Minute)

	// 自定义：只要状态码为 200 才算成功。
	gate.SetSuccessChecker(func(status int, err error) bool {
		return err == nil && status == http.StatusOK
	})

	_ = gate.Execute(func() error { return nil })
	_ = gate.Execute(func() error { return nil })

	snapshot := gate.GetMetricsSnapshot()
	if snapshot.FailedRequests != 0 {
		t.Fatalf("自定义判定下失败数 = %d, want 0", snapshot.FailedRequests)
	}

	// 恢复默认判定：status >= 500 视为失败。
	gate.SetSuccessChecker(nil)
	gate.afterExecute(http.StatusInternalServerError, nil)

	if got := gate.GetMetricsSnapshot().FailedRequests; got != 1 {
		t.Fatalf("恢复默认判定后失败数 = %d, want 1", got)
	}
}

// TestResetMetrics 验证指标重置不影响状态。
func TestResetMetrics(t *testing.T) {
	gate := createTestBreaker(1, time.Minute)
	_ = gate.Execute(func() error { return errors.New("boom") })

	stateBefore := gate.GetState()
	gate.ResetMetrics()

	snapshot := gate.GetMetricsSnapshot()
	if snapshot.TotalRequests != 0 || snapshot.FailedRequests != 0 || snapshot.BlockedRequests != 0 || snapshot.StateChanges != 0 {
		t.Fatalf("重置后指标 = %+v, want 全零", snapshot)
	}

	if gate.GetState() != stateBefore {
		t.Fatal("ResetMetrics 不应改变熔断状态")
	}
}

// TestConcurrentExecute 验证并发调用下计数准确。
func TestConcurrentExecute(t *testing.T) {
	config := NewDefaultConfig()
	config.MinimumRequests = 1 << 30 // 避免测试过程中被熔断
	gate := NewCircuitBreaker(config)

	const workers = 32
	const iterations = 100

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for index := 0; index < iterations; index++ {
				_ = gate.Execute(func() error { return nil })
			}
		}()
	}

	waitGroup.Wait()

	if snapshot := gate.GetMetricsSnapshot(); snapshot.TotalRequests != workers*iterations {
		t.Fatalf("总请求数 = %d, want %d", snapshot.TotalRequests, workers*iterations)
	}
}
