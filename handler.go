package leego

import (
	"net/http"
)

// HandleFunc 定义处理函数的类型
type HandleFunc func(ctx *Context)

// Handler 定义处理器的结构
type Handler struct {
	method      string
	engine      *Engine
	handle      HandleFunc
	middlewares []MiddlewareFunc
}

// newHandler 新建一个处理器
func newHandler(engine *Engine, method string, handle HandleFunc) *Handler {
	return &Handler{
		method:      method,
		engine:      engine,
		handle:      handle,
		middlewares: make([]MiddlewareFunc, 0),
	}
}

// WithMiddlewares 给处理器添加中间件
func (own *Handler) WithMiddlewares(middlewares ...MiddlewareFunc) *Handler {
	own.middlewares = append(own.middlewares, middlewares...)
	return own
}

// ServeInit （在启动服务之前）初始化处理器
func (own *Handler) ServeInit() {
	for i := len(own.middlewares); i > 0; i-- {
		own.handle = own.middlewares[i-1](own.handle)
	}
}

// ServeHTTP 实现 http.Handler 接口
func (own *Handler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.Method != own.method {
		http.Error(resp, _StatusMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}

	ctx := newContext(own.engine, resp, req)
	own.handle(ctx)
}
