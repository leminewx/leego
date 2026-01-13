package breaker

// State 定义熔断器的状态类型
type State string

const (
	STATE_OPEN      State = "CircuitBreaker.Open"     // 打开状态：拒绝请求，直接返回错误
	STATE_CLOSED    State = "CircuitBreaker.Closed"   // 闭合状态：正常请求服务
	STATE_HALF_OPEN State = "CircuitBreaker.HalfOpen" // 半开状态：允许部分请求试探服务是否恢复
)
