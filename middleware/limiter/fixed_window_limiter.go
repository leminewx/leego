package limiter

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
)

// _DEFAULT_FIXED_WINDOW_SIZE 为固定窗口的默认窗口长度。
const _DEFAULT_FIXED_WINDOW_SIZE = time.Second

/*
FixedWindowLimiter 实现固定窗口限流。

原理：为每个窗口维护一个计数器，窗口到期后整体清零。
优点是实现简单、单次判断只有一次加锁；缺点是窗口边界存在“双倍突发”：
若限额为 N，则在相邻窗口交界处的极短时间内可能通过最多 2N 个请求。

计数与窗口起点必须由同一把锁保护：
一旦拆成 atomic 快路径 + 锁慢路径，二者互不互斥，就会出现计数丢失，
实际放行量可能超过限额。
*/
type FixedWindowLimiter struct {
	// isEnabled 表示限流是否启用（atomic，避免与计数锁竞争）。
	isEnabled atomic.Bool
	// limit 为窗口内允许的最大请求数（atomic，支持热更新）。
	limit atomic.Int64

	// mu 保护以下全部字段。
	mu sync.Mutex
	// count 为当前窗口内已放行的请求数。
	count int64
	// windowStart 为当前窗口的起始时间。
	windowStart time.Time
	// windowSize 为窗口长度。
	windowSize time.Duration
	// options 为中间件等待行为配置（等待时长与轮询间隔）。
	options Config
}

// 编译期断言：固定窗口限流器必须满足 Limiter 接口。
var _ Limiter = (*FixedWindowLimiter)(nil)

/*
NewFixedWindowLimiter 创建固定窗口限流器。

参数：
  - limit：每个窗口允许的最大请求数；小于等于 0 表示不限流（始终放行）；
  - windowSize：窗口长度，小于等于 0 时使用默认值 1 秒；
  - configs：可选等待配置，未传时使用 NewDefaultConfig()。

限流器创建后即处于启用状态，无需再调用 Enable。
*/
func NewFixedWindowLimiter(limit int, windowSize time.Duration, configs ...Config) *FixedWindowLimiter {
	if windowSize <= 0 {
		windowSize = _DEFAULT_FIXED_WINDOW_SIZE
	}

	instance := &FixedWindowLimiter{
		windowStart: time.Now(),
		windowSize:  windowSize,
		options:     resolveConfig(configs...),
	}
	instance.limit.Store(int64(limit))
	instance.Enable()

	return instance
}

// Enable 启用限流。
func (own *FixedWindowLimiter) Enable() {
	own.isEnabled.Store(true)
}

// Disable 临时禁用限流，中间件将直接放行。
func (own *FixedWindowLimiter) Disable() {
	own.isEnabled.Store(false)
}

// IsEnabled 返回限流是否启用。
func (own *FixedWindowLimiter) IsEnabled() bool {
	return own.isEnabled.Load()
}

// SetLimit 更新窗口内允许的最大请求数，非正数表示不限流。
func (own *FixedWindowLimiter) SetLimit(limit int) {
	own.limit.Store(int64(limit))
}

// SetWindowSize 设置窗口长度并重置当前窗口。
//
// 重置而不是按比例折算历史计数：折算会让“限额”与“实际放行量”的对应关系
// 变得不可预测，而重置后的语义清晰——调用方改完配置，立即从新窗口重新计数。
func (own *FixedWindowLimiter) SetWindowSize(windowSize time.Duration) {
	if windowSize <= 0 {
		windowSize = _DEFAULT_FIXED_WINDOW_SIZE
	}

	own.mu.Lock()
	own.windowSize = windowSize
	own.windowStart = time.Now()
	own.count = 0
	own.mu.Unlock()
}

// GetLimit 返回窗口内允许的最大请求数。
func (own *FixedWindowLimiter) GetLimit() int {
	return int(own.limit.Load())
}

// GetWindowSize 返回窗口长度。
func (own *FixedWindowLimiter) GetWindowSize() time.Duration {
	own.mu.Lock()
	windowSize := own.windowSize
	own.mu.Unlock()
	return windowSize
}

/*
GetConfig 返回当前配置。

返回的 limit 与 windowSize 各自属于不同的同步域（原子变量、互斥锁），
因此二者不保证是“同一瞬间”的快照；对于限流配置这类低频变更项，该差异可以忽略。
*/
func (own *FixedWindowLimiter) GetConfig() (limit int, windowSize time.Duration) {
	return int(own.limit.Load()), own.GetWindowSize()
}

// GetMiddleware 返回固定窗口限流中间件。
//
// 参数 waitTimeout 表示“配额不足时最多等待多久”：
//   - 0：不等待，立即返回 429（推荐用于对外接口）；
//   - 负数：使用 Config.WaitTimeout；
//   - 正数：在超时内轮询等待，期间请求会被挂起。
func (own *FixedWindowLimiter) GetMiddleware(waitTimeout time.Duration) leego.MiddlewareFunc {
	if waitTimeout < 0 {
		waitTimeout = own.options.WaitTimeout
	}

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			if !own.IsEnabled() {
				next(ctx)
				return
			}

			var allowed bool
			if waitTimeout == 0 {
				allowed = own.Allow()
			} else {
				allowed = own.AllowWithTimeout(ctx.Request.Context(), waitTimeout)
			}

			if !allowed {
				writeTooManyRequests(ctx)
				return
			}

			next(ctx)
		}
	}
}

// Allow 非阻塞地判断是否放行，放行时消耗一个配额。
func (own *FixedWindowLimiter) Allow() bool {
	// 每个请求都会走这里，显式解锁比 defer 少一次调用开销。
	own.mu.Lock()

	now := time.Now()
	if now.Sub(own.windowStart) >= own.windowSize {
		own.windowStart = now
		own.count = 0
	}

	limit := own.limit.Load()
	if limit <= 0 {
		own.mu.Unlock()
		return true
	}

	if own.count >= limit {
		own.mu.Unlock()
		return false
	}

	own.count++
	own.mu.Unlock()

	return true
}

// AllowWithTimeout 在给定超时内等待配额。
func (own *FixedWindowLimiter) AllowWithTimeout(ctx context.Context, timeout time.Duration) bool {
	if timeout <= 0 {
		return own.Allow()
	}

	if ctx == nil {
		ctx = context.Background()
	}

	waitContext, cancel := context.WithTimeout(ctx, timeout)
	result := waitForQuotaWith(waitContext, own.Allow, own.options)
	cancel()

	return result == nil
}

// Wait 阻塞等待配额，直到放行或上下文结束。
func (own *FixedWindowLimiter) Wait(ctx context.Context) error {
	return waitForQuotaWith(ctx, own.Allow, own.options)
}
