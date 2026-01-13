package breaker

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
)

type CircuitBreaker interface {
	// Execute 执行回调函数
	Execute(call func() error) error

	// Enable 启用熔断器
	Enable()
	// Disable 禁用熔断器
	Disable()
	// GetState 获取熔断器状态
	GetState() State
}

var errCircuitBreakerIsOpen = errors.New("circuit circuitBreaker is open, refuse to execute the request")

// circuitBreaker 定义熔断器的数据结构
type circuitBreaker struct {
	isEnabled        int32         // 是否启用
	state            State         // 当前状态
	failureCount     int           // 失败计数
	successCount     int           // 成功计数
	failureThreshold int           // 失败阈值：超过此值触发熔断
	successThreshold int           // 成功阈值：半开状态下超过此值恢复闭合
	breakTimeout     time.Duration // 熔断超时时间：打开状态持续时间
	lastFailure      time.Time     // 最后一次失败时间
	lock             sync.Mutex    // 互斥锁保证并发安全
}

var _ CircuitBreaker = (*circuitBreaker)(nil)

// NewCircuitBreaker 新建一个熔断器
func NewCircuitBreaker(failureThreshold, successThreshold int, breakTimeout time.Duration) CircuitBreaker {
	return &circuitBreaker{
		state:            STATE_CLOSED,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		breakTimeout:     breakTimeout,
	}
}

// Enable 启用
func (own *circuitBreaker) Enable() {
	atomic.StoreInt32(&own.isEnabled, 1)
}

// Disable 禁用
func (own *circuitBreaker) Disable() {
	atomic.StoreInt32(&own.isEnabled, 0)
}

// GetState 获取当前熔断器状态
func (own *circuitBreaker) GetState() State {
	own.lock.Lock()
	state := own.state
	own.lock.Unlock()
	return state
}

// GetMiddleware 获取中间件函数（若请求被熔断，则响应503）
func (own *circuitBreaker) GetMiddleware(call func(ctx *leego.Context) error) leego.MiddlewareFunc {
	const content = "service is temporarily unavailable and please try again later"
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			// 未启用，直接跳过
			if atomic.LoadInt32(&own.isEnabled) == 0 {
				next(ctx)
				return
			}

			// 处理请求
			if err := own.Execute(ctx, call); err != nil {
				ctx.ResponseError(http.StatusServiceUnavailable, content)
				return
			}

			// 调用下一个处理器
			next(ctx)
		}
	}
}

/*
Execute 执行函数并进行熔断控制
  - call: 回调函数返回 error 视为失败，返回 nil 视为成功
*/
func (own *circuitBreaker) Execute(ctx *leego.Context, call func(ctx *leego.Context) error) (err error) {
	// 执行前调用
	if err = own.callBeforeExecute(); err != nil {
		return err
	}

	// 执行函数
	err = call(ctx)

	// 执行后调用
	own.callAfterExecute(err)
	return
}

// callBeforeExecute 执行前调用，决定是否允许执行
func (own *circuitBreaker) callBeforeExecute() (err error) {
	own.lock.Lock()

	// 打开状态，需要检查，其他状态可以正常执行
	if own.state == STATE_OPEN {
		if time.Since(own.lastFailure) >= own.breakTimeout {
			// 检查打开状态是否超时，若打开超时，则进入半开状态
			own.state = STATE_HALF_OPEN
			own.successCount = 0
		} else {
			// 断开未超时，拒绝执行
			err = errCircuitBreakerIsOpen
		}
	}

	own.lock.Unlock()
	return
}

// callAfterExecute 执行后调用，更新熔断器状态
func (own *circuitBreaker) callAfterExecute(err error) {
	own.lock.Lock()

	switch own.state {
	// 闭合状态
	case STATE_CLOSED:
		if err != nil {
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
		if err != nil {
			// 执行失败，回到打开状态
			own.state = STATE_OPEN
			own.lastFailure = time.Now()
			own.failureCount = 1
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

	own.lock.Unlock()
}
