package leego

import (
	"sync"
)

type (
	// HandleFunc 定义处理函数的类型
	HandleFunc func(ctx *Context)

	// MiddlewareFunc 定义中间件函数的类型
	MiddlewareFunc func(HandleFunc) HandleFunc
)

// handler 定义处理器的数据结构
type handler struct {
	group *group

	method      string           // 请求方法
	handle      HandleFunc       // 处理函数
	middlewares []MiddlewareFunc // 中间件函数
	initialized bool             // 标记是否已经初始化

	initializedMutex sync.Mutex
}

// newHandler 新建一个处理器
func newHandler(group *group, method string, handle HandleFunc) *handler {
	return &handler{
		group:       group,
		method:      method,
		handle:      handle,
		middlewares: make([]MiddlewareFunc, 0, 30),
	}
}

// ServeHTTP 实现自定义的 HTTP 处理函数
func (own *handler) ServeHTTP(ctx *Context) {
	// 延迟初始化
	if !own.initialized {
		own.initializedMutex.Lock()
		own.initialize()
		own.initializedMutex.Unlock()
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

	// 获取所有中间件
	own.group.getAllMiddlewares(own.middlewares)

	// 调用中间件
	for i := 0; i < len(own.middlewares); i-- {
		own.handle = own.middlewares[i](own.handle)
	}

	own.initialized = true
}
