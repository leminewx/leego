package leego

import (
	"net/http"

	"gitee.com/leminewx/leego/response"
)

type (
	// HandleFunc 定义处理函数的类型
	HandleFunc func(ctx *Context)

	// MiddlewareFunc 定义中间件函数的类型
	MiddlewareFunc func(HandleFunc) HandleFunc

	// InterceptorFunc 定义拦截器处理函数的类型
	InterceptorFunc func(http.Handler) http.Handler
)

// handler 定义处理器的数据结构
type handler struct {
	engine *Engine

	method      string           // 请求方法
	handle      HandleFunc       // 处理函数
	middlewares []MiddlewareFunc // 中间件函数
	initialized bool             // 标记是否已经初始化
}

// newHandler 新建一个处理器
func newHandler(engine *Engine, method string, handle HandleFunc) *handler {
	return &handler{
		engine:      engine,
		method:      method,
		handle:      handle,
		middlewares: make([]MiddlewareFunc, 0),
	}
}

// WithMiddlewares 添加处理器中间件
func (own *handler) WithMiddlewares(middlewares ...MiddlewareFunc) *handler {
	// 如果已经初始化，需要重新初始化
	if own.initialized {
		own.initialized = false
	}

	// 添加中间件
	own.middlewares = append(own.middlewares, middlewares...)
	return own
}

// ServeHTTP 实现 http.Handler 接口
func (own *handler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	// 检查请求方法
	if req.Method != own.method {
		http.Error(resp, response.STATUS_METHOD_NOT_ALLOWED, http.StatusMethodNotAllowed)
		return
	}

	// 延迟初始化
	if !own.initialized {
		own.initialize()
	}

	// 创建上下文
	ctx := newContext(own.engine, resp, req)

	// 调用处理函数
	own.handle(ctx)
}

// initialize （在服务前）初始化处理器
func (own *handler) initialize() {
	if own.initialized {
		return
	}

	for i := len(own.middlewares); i > 0; i-- {
		own.handle = own.middlewares[i-1](own.handle)
	}

	own.initialized = true
}
