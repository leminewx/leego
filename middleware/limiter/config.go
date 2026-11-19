package limiter

import "time"

// 限流器公共默认参数。
const (
	// _DEFAULT_WAIT_TIMEOUT 为中间件默认的等待配额时长（timeout 传负数时生效）。
	_DEFAULT_WAIT_TIMEOUT = 3 * time.Second
	// _DEFAULT_POLL_INTERVAL 为等待配额时的轮询间隔。
	_DEFAULT_POLL_INTERVAL = 50 * time.Millisecond
)

// Config 汇总限流器在「等待配额」路径上的可调参数。
//
// 三个限流器共享同一份配置：算法只决定「发不发配额」，而等待多久、
// 多久检查一次属于中间件行为。放在算法实现里会各自漂移，
// 收敛到 Config 后可以按进程统一调整（例如压测时调小轮询间隔）。
type Config struct {
	// WaitTimeout 为 GetMiddleware 收到负数超时时的回退等待时长。
	//
	// 取值为 0 时表示「不提供默认值」，此时 GetMiddleware 的 0 仍表示立即失败；
	// 只有在调用方显式传入负数时才会用到该字段。
	WaitTimeout time.Duration
	// PollInterval 为轮询等待配额的时间间隔，必须大于 0。
	//
	// 间隔越小，配额释放后的唤醒越及时，但空转的定时器开销越大；
	// 50ms 在常见 QPS 下已经足够平滑。
	PollInterval time.Duration
}

// NewDefaultConfig 返回一份带安全默认值的配置。
func NewDefaultConfig() Config {
	return Config{
		WaitTimeout:  _DEFAULT_WAIT_TIMEOUT,
		PollInterval: _DEFAULT_POLL_INTERVAL,
	}
}

// Clone 返回配置副本。
//
// Config 当前只含值类型字段，直接赋值即可；保留该方法是为了将来
// 增加引用字段时不破坏调用方语义。
func (own Config) Clone() Config {
	return own
}

// normalize 修正非法字段，保证内部使用安全值。
//
// 负数等待时长视为「未设置」，回退到默认值；非正轮询间隔同样回退，
// 避免 time.NewTimer(0) 造成忙循环。
func (own Config) normalize() Config {
	if own.WaitTimeout < 0 {
		own.WaitTimeout = _DEFAULT_WAIT_TIMEOUT
	}

	if own.PollInterval <= 0 {
		own.PollInterval = _DEFAULT_POLL_INTERVAL
	}

	return own
}

// resolveConfig 从可变参数中取出生效配置。
//
// 只接受第一个配置：多份配置合并会引入「谁覆盖谁」的优先级歧义，
// 而限流参数本就应该由一处统一管理。未传参数时返回默认配置。
func resolveConfig(configs ...Config) Config {
	if len(configs) == 0 {
		return NewDefaultConfig()
	}

	return configs[0].normalize()
}
