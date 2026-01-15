package breaker

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
)

var ErrCircuitBreakerIsOpen = errors.New("breaker: circuit breaker is open, refuse to execute the request")

// CircuitBreaker 定义熔断器的数据结构
type CircuitBreaker struct {
	isEnabled        int32                            // 是否启用
	state            State                            // 当前状态
	failureCount     int                              // 失败计数
	successCount     int                              // 成功计数
	failureThreshold int                              // 失败阈值：超过此值触发熔断
	successThreshold int                              // 成功阈值：半开状态下超过此值恢复闭合
	breakTimeout     time.Duration                    // 熔断超时时间：打开状态持续时间
	lastFailure      time.Time                        // 最后一次失败时间
	isSuccessFunc    func(status int, err error) bool // 判断请求是否成功的函数
	metrics          *metrics                         // 指标统计
	lock             sync.RWMutex                     // 互斥锁保证并发安全
}

// NewCircuitBreaker 新建一个熔断器
func NewCircuitBreaker(failureThreshold, successThreshold int, breakTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            STATE_CLOSED,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		breakTimeout:     breakTimeout,
		metrics:          &metrics{},

		// 默认判断逻辑：状态码 >= 500 或 err != nil 视为失败
		isSuccessFunc: func(status int, err error) bool {
			return err == nil && status < http.StatusInternalServerError
		},
	}
}

// GetMiddleware 获取中间件函数（若请求被熔断，则响应503）
func (own *CircuitBreaker) GetMiddleware() leego.MiddlewareFunc {
	const content = "service is temporarily unavailable and please try again later"
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 未启用，直接跳过
			if atomic.LoadInt32(&own.isEnabled) == 0 {
				next(ctx)
				return
			}

			// 记录总请求数
			own.metrics.totalRequests.Add(1)

			// 执行前检查熔断器状态
			if err := own.callBeforeExecute(); err != nil {
				// 记录被熔断的请求
				own.metrics.blockedRequests.Add(1)
				ctx.ResponseError(http.StatusServiceUnavailable, content)
				return
			}

			// 调用下一个处理器
			next(ctx)

			// 执行后检查熔断器状态
			own.callAfterExecute(ctx.GetStatusCode(), nil)
		}
	}
}

// Execute 执行函数并进行熔断控制
func (own *CircuitBreaker) Execute(call func() error) (err error) {
	// 执行前调用
	if err = own.callBeforeExecute(); err != nil {
		return err
	}

	// 执行函数
	err = call()

	// 执行后调用
	if err == nil {
		own.callAfterExecute(http.StatusOK, nil)
	} else {
		own.callAfterExecute(http.StatusInternalServerError, err)
	}

	return err
}

// ExecuteWithContext 执行带上下文的函数，并进行熔断控制
func (own *CircuitBreaker) ExecuteWithContext(ctx *leego.Context, call func(ctx *leego.Context) error) (err error) {
	// 执行前调用
	if err = own.callBeforeExecute(); err != nil {
		return err
	}

	// 执行函数
	err = call(ctx)

	// 执行后调用
	own.callAfterExecute(ctx.GetStatusCode(), err)

	return
}

// callBeforeExecute 执行前调用，决定是否允许执行
func (own *CircuitBreaker) callBeforeExecute() (err error) {
	own.metrics.totalRequests.Add(1)

	own.lock.Lock()

	// 打开状态，需要检查，其他状态可以正常执行
	if own.state == STATE_OPEN {
		if time.Since(own.lastFailure) >= own.breakTimeout {
			// 检查打开状态是否超时，若打开超时，则进入半开状态
			own.state = STATE_HALF_OPEN
			own.successCount = 0
			own.metrics.stateChanges.Add(1)
		} else {
			// 断开未超时，拒绝执行
			err = ErrCircuitBreakerIsOpen
			own.metrics.blockedRequests.Add(1)
		}
	}

	own.lock.Unlock()
	return
}

// callAfterExecute 执行后调用，更新熔断器状态
func (own *CircuitBreaker) callAfterExecute(status int, err error) {
	own.lock.Lock()

	// 保存旧状态用于比较
	oldState := own.state

	// 判断是否成功
	isSuccess := own.isSuccessFunc(status, err)
	if !isSuccess {
		own.metrics.failedRequests.Add(1)
	}

	switch own.state {
	// 闭合状态
	case STATE_CLOSED:
		if !isSuccess {
			// 执行失败，增加失败计数
			own.failureCount++
			own.lastFailure = time.Now()

			// 检查是否达到失败阈值，若超过失败阈值，则触发熔断机制
			if own.failureCount >= own.failureThreshold {
				own.state = STATE_OPEN
			}
		} else {
			// 执行成功，重置失败计数
			own.failureCount = 0
		}

	// 半开状态
	case STATE_HALF_OPEN:
		if !isSuccess {
			// 执行失败，回到打开状态
			own.state = STATE_OPEN
			own.lastFailure = time.Now()
			own.failureCount = 1
			own.successCount = 0
		} else {
			// 执行成功，增加成功计数
			own.successCount++
			// 检查是否达到成功阈值，若达到成功阈值，则关闭熔断机制
			if own.successCount >= own.successThreshold {
				own.state = STATE_CLOSED
				own.failureCount = 0
				own.successCount = 0
			}
		}

	// 打开状态
	// 理论上不会进入这里，因为打开状态下 callBeforeExecute 会阻止执行
	case STATE_OPEN:
	}

	// 如果状态发生变化，记录状态变化次数
	if oldState != own.state {
		own.metrics.stateChanges.Add(1)
	}

	own.lock.Unlock()
}
