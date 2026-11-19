package breaker

// State 定义熔断器状态类型。
//
// 使用字符串而非整型，是为了让日志、指标导出与 JSON 序列化结果具备可读性；
// 取值带 "CircuitBreaker." 前缀，便于在多组件日志中区分来源。
type State string

const (
	// STATE_CLOSED 为闭合状态：请求正常通过，并持续统计成功率。
	STATE_CLOSED State = "CircuitBreaker.Closed"
	// STATE_OPEN 为打开状态：请求被直接拒绝，直到 OpenTimeout 到期。
	STATE_OPEN State = "CircuitBreaker.Open"
	// STATE_HALF_OPEN 为半开状态：仅放行少量探测请求，用于验证下游是否恢复。
	STATE_HALF_OPEN State = "CircuitBreaker.HalfOpen"
)

// String 返回状态的可读名称，使 State 满足 fmt.Stringer 接口。
func (own State) String() string {
	return string(own)
}
