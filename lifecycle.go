package leego

import (
	"context"
	"errors"
)

// ErrNilShutdownHook 表示注册了空的关闭钩子。
var ErrNilShutdownHook = errors.New("leego: shutdown hook must not be nil")

// ShutdownHook 定义进程关闭时需要执行的清理逻辑。
//
// 钩子用于释放框架外部资源（定时任务、指标落盘、连接池等），
// 应当尊重 ctx 的截止时间，超时后主动返回。
type ShutdownHook func(ctx context.Context) error

// AddShutdownHook 注册关闭钩子。
//
// 执行顺序为注册顺序的**逆序**：后注册的钩子通常依赖于先注册的资源，
// 因此需要先释放。重复注册同一函数不会去重，调用方自行保证幂等。
func (own *Engine) AddShutdownHook(hook ShutdownHook) error {
	if hook == nil {
		return ErrNilShutdownHook
	}

	own.shutdownMu.Lock()
	own.shutdownHooks = append(own.shutdownHooks, hook)
	own.shutdownMu.Unlock()

	return nil
}

// RunShutdownHooks 按逆序执行全部关闭钩子。
//
// 所有钩子都会被执行，即使其中某个失败；返回值用 errors.Join 汇总。
// 函数可重复调用，但钩子本身可能不幂等，正常场景应只由 Shutdown 触发一次。
func (own *Engine) RunShutdownHooks(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	own.shutdownMu.Lock()
	hooks := make([]ShutdownHook, len(own.shutdownHooks))
	copy(hooks, own.shutdownHooks)
	own.shutdownMu.Unlock()

	var errs []error
	for index := len(hooks) - 1; index >= 0; index-- {
		if err := hooks[index](ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// countShutdownHooks 返回已注册的钩子数量，供测试与诊断使用。
func (own *Engine) countShutdownHooks() int {
	own.shutdownMu.Lock()
	defer own.shutdownMu.Unlock()

	return len(own.shutdownHooks)
}
