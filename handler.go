package leego

import (
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego/middleware/monitor"
	"gitee.com/leminewx/leego/tree"
)

type (
	// HandleFunc 定义处理函数的类型
	HandleFunc func(ctx *Context)

	// MiddlewareFunc 定义中间件函数的类型
	MiddlewareFunc func(HandleFunc) HandleFunc

	// Middleware 定义中间件的接口类型
	Middleware interface {
		// Handle 处理函数
		Handle(handle HandleFunc) HandleFunc
		// SetScope 设置中间件的作用域
		SetScope(scope string)
	}
)

// handler 定义处理器的数据结构
type handler struct {
	group *group

	handle      HandleFunc       // 处理函数
	monitor     *monitor.Monitor // 监控器
	paramNames  []string         // 模式中的参数名列表
	middlewares []MiddlewareFunc // 中间件函数

	initialized     bool // 标记是否已经初始化
	initializedLock sync.Mutex
}

// newHandler 新建一个处理器
func newHandler(group *group, method, pattern string, handle HandleFunc, tags ...string) *handler {
	return &handler{
		group:      group,
		handle:     handle,
		monitor:    monitor.New(method, pattern, tags...),
		paramNames: extractSegmentNames(tree.SplitPattern(pattern)),
	}
}

// Enable 启用
func (own *handler) Enable() *handler {
	atomic.StoreInt32(&own.monitor.Enabled, 1)
	return own
}

// Disenable 禁用
func (own *handler) Disenable() *handler {
	atomic.StoreInt32(&own.monitor.Enabled, 0)
	return own
}

// IsEnable 检查处理器是否启用
func (own *handler) IsEnable() bool {
	return atomic.LoadInt32(&own.monitor.Enabled) == 1
}

// Handle 请求处理函数
func (own *handler) Handle(ctx *Context) {
	// 延迟初始化
	if !own.initialized {
		own.initialize()
	}

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

	// 获取所有中间件
	own.middlewares = make([]MiddlewareFunc, 0, 30)
	own.group.getAllMiddlewares(&own.middlewares)

	// 调用中间件
	for i := len(own.middlewares) - 1; i >= 0; i-- {
		own.handle = own.middlewares[i](own.handle)
	}

	// 标记已初始化
	own.initialized = true
}
