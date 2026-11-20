package leego

import (
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego/middlewares/collector"
	"gitee.com/leminewx/leego/tree"
)

type (
	// HandleFunc 定义路由处理函数的类型。
	//
	// 处理函数在请求进入时被调用，必须自行完成响应写入；
	// 未写入响应时由外层中间件补充状态码。
	HandleFunc func(ctx *Context)

	// MiddlewareFunc 定义中间件函数的类型。
	//
	// 中间件通过包装 next 实现“洋葱模型”：前置逻辑在 next 之前执行，
	// 后置逻辑在 next 返回之后执行。
	MiddlewareFunc func(next HandleFunc) HandleFunc

	// Handler 定义单条路由处理器的接口。
	Handler interface {
		// ServeHTTP 处理一次请求。
		ServeHTTP(ctx *Context)
		// Enable 启用该路由。
		Enable()
		// Disable 禁用该路由；禁用后不执行任何中间件，直接返回 423。
		Disable()
		// IsEnabled 返回该路由是否启用。
		IsEnabled() bool
		// GetCollector 返回该路由的指标采集器；不会返回 nil。
		GetCollector() *collector.Collector
		// Prepare 冻结中间件链；由 Engine.Prepare 在启动期统一调用。
		Prepare()
	}

	// handler 是 Handler 的唯一实现。
	//
	// 关键设计：
	//   - 中间件链在 Prepare 阶段折叠一次，结果存入 atomic.Pointer，
	//     运行期只做一次指针加载，避免每请求拼接与数据竞争；
	//   - collector 负责启停状态与指标，路由本身不再单独维护开关。
	handler struct {
		// group 为路由所属的路由组，用于收集链路上的中间件。
		group *group
		// base 为业务处理函数（未包装中间件）。
		base HandleFunc
		// chain 保存折叠后的完整处理链。
		chain atomic.Pointer[HandleFunc]
		// collector 为指标采集器。
		collector *collector.Collector
		// paramNames 为路由模式中的参数名列表，供文档生成与调试使用。
		paramNames []string
		// prepareOnce 保证中间件链只折叠一次。
		prepareOnce sync.Once
	}
)

// newHandler 创建一条路由的处理器。
//
// 参数：
//   - group：路由所属的路由组；
//   - method：HTTP 请求方法；
//   - pattern：路由模式；
//   - handle：业务处理函数；
//   - tags：可选标签，第一个非空标签作为路由名称。
func newHandler(group *group, method, pattern string, handle HandleFunc, tags ...string) Handler {
	return &handler{
		group:      group,
		base:       handle,
		collector:  collector.New(method, pattern, tags...),
		paramNames: extractSegmentNames(tree.SplitPattern(pattern)),
	}
}

// ServeHTTP 执行折叠后的处理链。
//
// 若 Prepare 尚未被调用（例如在单元测试中直接使用 handler），
// 这里会惰性完成一次折叠，保证行为与引擎路径一致。
func (own *handler) ServeHTTP(ctx *Context) {
	handle := own.chain.Load()
	if handle == nil {
		own.Prepare()
		handle = own.chain.Load()
	}

	if handle == nil {
		return
	}

	(*handle)(ctx)
}

// Enable 启用该路由。
func (own *handler) Enable() {
	own.collector.Enable()
}

// Disable 禁用该路由。
//
// 禁用后路由仍然参与匹配，但不会执行任何中间件与业务处理函数，
// 由引擎统一返回 423 Locked。
func (own *handler) Disable() {
	own.collector.Disable()
}

// IsEnabled 返回该路由是否启用。
func (own *handler) IsEnabled() bool {
	return own.collector.IsEnabled()
}

// GetCollector 返回该路由的指标采集器。
func (own *handler) GetCollector() *collector.Collector {
	return own.collector
}

// GetParamNames 返回路由模式中的参数名列表。
func (own *handler) GetParamNames() []string {
	return append([]string(nil), own.paramNames...)
}

// Prepare 折叠中间件链，使其在运行期只读。
//
// 折叠范围为“从当前路由组向上直到根路由组之前”的中间件：
// 根路由组上的中间件属于全局链，由 Engine 统一执行，避免重复调用。
func (own *handler) Prepare() {
	own.prepareOnce.Do(func() {
		handle := own.base
		if own.group != nil && own.group.engine != nil {
			middlewares := make([]MiddlewareFunc, 0, 8)
			own.group.collectMiddlewares(own.group.engine.group, &middlewares)

			for index := len(middlewares) - 1; index >= 0; index-- {
				handle = middlewares[index](handle)
			}
		}

		own.chain.Store(&handle)
	})
}
