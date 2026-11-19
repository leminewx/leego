// Package breaker 提供基于失败率的滑动窗口熔断器。
//
// 与“连续失败次数”模型相比，失败率模型更贴近真实故障：
// 高流量接口偶发失败不会误触发，低流量接口也不会因为一次失败就长期熔断。
//
// 状态机：
//
//	Closed  --(窗口内失败率 >= 阈值且样本数达标)-->  Open
//	Open    --(等待 OpenTimeout)-->                 HalfOpen
//	HalfOpen --(连续成功达到阈值)-->                 Closed
//	HalfOpen --(任意失败)-->                         Open
//
// 并发约定：所有状态字段由 mu 保护；指标使用 atomic，避免与状态锁竞争。
package breaker

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
)

// ErrCircuitBreakerIsOpen 表示熔断器处于打开状态，拒绝执行请求。
var ErrCircuitBreakerIsOpen = errors.New("breaker: circuit breaker is open, refuse to execute the request")

// _BUCKET_COUNT 定义滑动窗口的桶数量。
//
// 桶越多，失败率对时间越敏感，但统计开销也越大；10 个桶在精度与开销之间较为均衡。
const _BUCKET_COUNT = 10

// CircuitBreaker 定义熔断器。
//
// 熔断器可被多协程并发调用；GetMiddleware 返回的中间件同样并发安全。
type CircuitBreaker struct {
	// isEnabled 表示熔断器是否启用（atomic，避免与状态锁竞争）。
	isEnabled atomic.Bool
	// metrics 为运行指标（atomic）。
	metrics metrics

	// mu 保护以下全部字段。
	mu sync.Mutex
	// config 为不可变配置。
	config Config
	// state 为当前状态。
	state State
	// deadline 为打开状态的结束时间。
	deadline time.Time
	// probes 为半开状态下已放行但未完成的探测请求数。
	probes int64
	// successes 为半开状态下连续成功次数。
	successes int64
	// buckets 为滑动窗口桶。
	buckets []windowBucket
	// bucketSize 为单个桶的时间跨度。
	bucketSize time.Duration
	// currentBucket 为当前桶下标。
	currentBucket int
	// lastAdvance 为上次推进窗口的时间。
	lastAdvance time.Time
	// isSuccessFunc 判断一次调用是否成功。
	isSuccessFunc func(status int, err error) bool
}

// windowBucket 保存一个时间桶内的成功与失败计数。
type windowBucket struct {
	// successes 为成功次数。
	successes int64
	// failures 为失败次数。
	failures int64
}

// NewCircuitBreaker 创建熔断器。
//
// 参数 config 中非法的数值会被修正为安全默认值（见 Config.Normalize），
// 因此调用方即使传入零值配置也能获得可用的熔断器。
func NewCircuitBreaker(config Config) *CircuitBreaker {
	config = config.Normalize()

	instance := &CircuitBreaker{
		config:      config,
		state:       STATE_CLOSED,
		buckets:     make([]windowBucket, _BUCKET_COUNT),
		bucketSize:  config.WindowSize / _BUCKET_COUNT,
		lastAdvance: time.Now(),
		isSuccessFunc: func(status int, err error) bool {
			return err == nil && status < http.StatusInternalServerError
		},
	}
	instance.isEnabled.Store(config.IsEnabled)
	return instance
}

// GetMiddleware 返回熔断中间件。
//
// 行为：
//   - 熔断器未启用时直接放行；
//   - 打开状态返回 503，不执行后续处理链；
//   - 其余状态正常执行，并按响应状态码记录成功/失败。
func (own *CircuitBreaker) GetMiddleware() leego.MiddlewareFunc {
	const message = "service is temporarily unavailable and please try again later"

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			if !own.IsEnabled() {
				next(ctx)
				return
			}

			if err := own.beforeExecute(); err != nil {
				ctx.ResponseError(http.StatusServiceUnavailable, message)
				return
			}

			next(ctx)
			own.afterExecute(ctx.GetStatusCode(), nil)
		}
	}
}

// Execute 在熔断保护下执行给定函数。
//
// 返回 ErrCircuitBreakerIsOpen 表示请求被拒绝，未执行 call。
func (own *CircuitBreaker) Execute(call func() error) error {
	if call == nil {
		return errors.New("breaker: nil call")
	}

	if err := own.beforeExecute(); err != nil {
		return err
	}

	err := call()
	status := http.StatusOK
	if err != nil {
		status = http.StatusInternalServerError
	}

	own.afterExecute(status, err)
	return err
}

// ExecuteWithContext 在熔断保护下执行带上下文的函数。
func (own *CircuitBreaker) ExecuteWithContext(ctx *leego.Context, call func(ctx *leego.Context) error) error {
	if call == nil {
		return errors.New("breaker: nil call")
	}

	if err := own.beforeExecute(); err != nil {
		return err
	}

	err := call(ctx)
	status := http.StatusOK
	if ctx != nil && ctx.GetStatusCode() != 0 {
		status = ctx.GetStatusCode()
	} else if err != nil {
		status = http.StatusInternalServerError
	}

	own.afterExecute(status, err)
	return err
}

// beforeExecute 判断是否允许本次调用。
func (own *CircuitBreaker) beforeExecute() error {
	own.metrics.totalRequests.Add(1)

	own.mu.Lock()

	now := time.Now()
	switch own.state {
	case STATE_OPEN:
		if now.Before(own.deadline) {
			own.metrics.blockedRequests.Add(1)
			own.mu.Unlock()
			return ErrCircuitBreakerIsOpen
		}

		own.transitionToHalfOpenLocked(now)
		fallthrough
	case STATE_HALF_OPEN:
		if own.probes >= own.config.HalfOpenMaxRequests {
			own.metrics.blockedRequests.Add(1)
			own.mu.Unlock()
			return ErrCircuitBreakerIsOpen
		}

		own.probes++
	}

	own.mu.Unlock()
	return nil
}

// afterExecute 记录本次调用结果并推进状态机。
func (own *CircuitBreaker) afterExecute(status int, err error) {
	own.mu.Lock()

	isSuccess := own.isSuccessFunc(status, err)
	if !isSuccess {
		own.metrics.failedRequests.Add(1)
	}

	now := time.Now()

	switch own.state {
	case STATE_CLOSED:
		own.recordLocked(now, isSuccess)
		own.evaluateLocked(now)

	case STATE_HALF_OPEN:
		if own.probes > 0 {
			own.probes--
		}

		if !isSuccess {
			own.transitionToOpenLocked(now)
			own.mu.Unlock()
			return
		}

		own.successes++
		if own.successes >= own.config.HalfOpenSuccessThreshold {
			own.transitionToClosedLocked(now)
		}

	case STATE_OPEN:
		// 打开状态下正常不会执行到此处；保留分支以覆盖“执行中途被其他协程打开”的竞态。
	}

	own.mu.Unlock()
}

// recordLocked 将一次调用结果写入当前时间桶。
func (own *CircuitBreaker) recordLocked(now time.Time, success bool) {
	own.advanceLocked(now)

	if success {
		own.buckets[own.currentBucket].successes++
	} else {
		own.buckets[own.currentBucket].failures++
	}
}

// advanceLocked 按流逝时间推进窗口，并清空过期桶。
func (own *CircuitBreaker) advanceLocked(now time.Time) {
	if own.bucketSize <= 0 {
		return
	}

	steps := int(now.Sub(own.lastAdvance) / own.bucketSize)
	if steps <= 0 {
		return
	}

	if steps >= len(own.buckets) {
		for index := range own.buckets {
			own.buckets[index] = windowBucket{}
		}
		own.currentBucket = 0
		own.lastAdvance = now
		return
	}

	for step := 0; step < steps; step++ {
		own.currentBucket = (own.currentBucket + 1) % len(own.buckets)
		own.buckets[own.currentBucket] = windowBucket{}
	}

	own.lastAdvance = now
}

// evaluateLocked 根据窗口统计判断是否需要打开熔断器。
func (own *CircuitBreaker) evaluateLocked(now time.Time) {
	successes, failures := own.countLocked()
	total := successes + failures
	if total < own.config.MinimumRequests {
		return
	}

	if float64(failures)/float64(total) >= own.config.FailureRate {
		own.transitionToOpenLocked(now)
	}
}

// countLocked 汇总窗口内的成功与失败次数。
func (own *CircuitBreaker) countLocked() (successes, failures int64) {
	for _, bucket := range own.buckets {
		successes += bucket.successes
		failures += bucket.failures
	}

	return
}

// transitionToOpenLocked 切换到打开状态。
func (own *CircuitBreaker) transitionToOpenLocked(now time.Time) {
	if own.state != STATE_OPEN {
		own.metrics.stateChanges.Add(1)
	}

	own.state = STATE_OPEN
	own.deadline = now.Add(own.config.OpenTimeout)
	own.probes = 0
	own.successes = 0
}

// transitionToHalfOpenLocked 切换到半开状态。
func (own *CircuitBreaker) transitionToHalfOpenLocked(now time.Time) {
	own.state = STATE_HALF_OPEN
	own.probes = 0
	own.successes = 0
	own.deadline = time.Time{}
	own.metrics.stateChanges.Add(1)

	// 重置窗口，使半开阶段的统计不受历史失败影响。
	for index := range own.buckets {
		own.buckets[index] = windowBucket{}
	}
	own.currentBucket = 0
	own.lastAdvance = now
}

// transitionToClosedLocked 切换到闭合状态。
func (own *CircuitBreaker) transitionToClosedLocked(now time.Time) {
	own.state = STATE_CLOSED
	own.probes = 0
	own.successes = 0
	own.deadline = time.Time{}
	own.metrics.stateChanges.Add(1)

	for index := range own.buckets {
		own.buckets[index] = windowBucket{}
	}
	own.currentBucket = 0
	own.lastAdvance = now
}
