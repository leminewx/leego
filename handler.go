package leego

import (
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego/middleware/collector"
	"gitee.com/leminewx/leego/tree"
)

type (
	// HandleFunc 定义处理函数的类型
	HandleFunc func(ctx *Context)

	// MiddlewareFunc 定义中间件函数的类型
	MiddlewareFunc func(next HandleFunc) HandleFunc

	// Middleware 定义中间件的接口类型
	Middleware interface {
		// Handle 处理函数
		Handle(next HandleFunc) HandleFunc
		// Enable 启用中间件
		Enable()
		// Disable 禁用中间件
		Disable()
		// SetScope 设置中间件的作用域
		SetScope(scope string)
		// SyncConfig 同步中间件的配置
		SyncConfig(config any) error
		// GetConfig 获取中间件的配置
		GetConfig() any
		// GetID 获取中间件ID
		GetID() string
	}

	// Handler 定义处理器的接口类型
	Handler interface {
		// ServeHTTP 处理 HTTP 请求
		ServeHTTP(ctx *Context)
		// Enable 启用处理器
		Enable()
		/*Disable 禁用指定路由
		  - 禁用指定路由后，也不会调用该路由上的任一中间件，且响应状态码为 423
		*/
		Disable()
		// IsEnabled 是否启用该处理器
		IsEnabled() bool
		// GetCollector 获取指标采集器
		GetCollector() *collector.Collector
	}

	// handler 定义处理器的实现类型
	handler struct {
		group *group

		handle      HandleFunc           // 处理函数
		collector   *collector.Collector // 指标采集器
		paramNames  []string             // 模式中的参数名列表
		middlewares []MiddlewareFunc     // 中间件函数

		initialized     bool // 标记是否已经初始化
		initializedLock sync.Mutex
	}
)

// newHandler 新建一个处理器
func newHandler(group *group, method, pattern string, handle HandleFunc, tags ...string) Handler {
	return &handler{
		group:      group,
		handle:     handle,
		collector:  collector.New(method, pattern, tags...),
		paramNames: extractSegmentNames(tree.SplitPattern(pattern)),
	}
}

// Enable 启用处理器
func (own *handler) Enable() {
	atomic.StoreInt32(&own.collector.Enabled, 1)
}

/*
Disable 禁用处理器
  - 禁用指定路由后，也不会调用该路由上的任一中间件，且响应状态码为 423
*/
func (own *handler) Disable() {
	atomic.StoreInt32(&own.collector.Enabled, 0)
}

// IsEnabled 检查处理器是否启用
func (own *handler) IsEnabled() bool {
	return atomic.LoadInt32(&own.collector.Enabled) == 1
}

// GetCollector 获取指标监控器
func (own *handler) GetCollector() *collector.Collector {
	return own.collector
}

// ServeHTTP 请求处理函数
func (own *handler) ServeHTTP(ctx *Context) {
	// 延迟初始化
	own.initialize()

	// 调用处理函数
	own.handle(ctx)
}

// initialize （在服务前）初始化处理器
func (own *handler) initialize() {
	// 如果已经初始化，则返回
	if own.initialized {
		return
	}

	own.initializedLock.Lock()
	defer own.initializedLock.Unlock()

	// 如果已经初始化，则返回
	if own.initialized {
		return
	}

	// 标记已初始化
	own.initialized = true
	if own.group == nil {
		return
	}

	// 获取所有中间件
	own.middlewares = make([]MiddlewareFunc, 0, 30)
	own.group.getAllMiddlewares(&own.middlewares)
	if len(own.middlewares) == 0 {
		return
	}

	// 调用中间件
	for i := len(own.middlewares) - 1; i >= 0; i-- {
		own.handle = own.middlewares[i](own.handle)
	}
}
