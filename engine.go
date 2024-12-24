package leego

import (
	"fmt"
	"net/http"

	"gitee.com/leminewx/loggo"
)

// Options 定义引擎配置参数的结构
type Options struct {
	Addr        string
	TLSCertFile string
	TLSKeyFile  string
}

// Engine 定义引擎的结构
type Engine struct {
	*group // 继承路由组，所以引擎本身也是一个路由组

	service      string
	interceptors []Interceptor
	groups       []*group
	server       *http.Server
	router       *http.ServeMux
}

// NewEngine 新建一个引擎
func NewEngine(service string) *Engine {
	engine := &Engine{
		service:      service,
		interceptors: make([]Interceptor, 0),
		groups:       make([]*group, 0),
		router:       http.NewServeMux(),
	}

	engine.group = &group{engine: engine}
	engine.groups = append(engine.groups, engine.group)
	return engine
}

func NewDefaultEngine(service string) *Engine {
	engine := NewEngine(service)
	engine.WithInterceptors(Recover()).WithMiddlewares(Log())
	return engine
}

func (own *Engine) GetServiceName() string {
	return own.service
}

func (own *Engine) WithInterceptors(interceptors ...Interceptor) *Engine {
	own.interceptors = append(own.interceptors, interceptors...)
	return own
}

// ListenAndServe 启动一个 HTTP 服务
func (own *Engine) ListenAndServe(opts *Options) {
	own.init(opts)
	loggo.Info(fmt.Sprintf("Server is starting %s ...", opts.Addr))
	loggo.Fatal(fmt.Sprintf("Failed to start server: %v.", own.server.ListenAndServe()))
}

// ListenAndServeTLS 启动一个 HTTPS 服务
func (own *Engine) ListenAndServeTLS(opts *Options) {
	own.init(opts)
	loggo.Info(fmt.Sprintf("Server is starting %s ...", opts.Addr))
	loggo.Fatal(fmt.Sprintf("Failed to start server: %v.", own.server.ListenAndServeTLS(opts.TLSCertFile, opts.TLSKeyFile)))
}

func (own *Engine) init(opts *Options) {
	if apiFile != nil {
		apiFile.Close()
	}

	var handler http.Handler = own.router
	for i := len(own.interceptors); i > 0; i-- {
		handler = own.interceptors[i-1](handler)
	}

	own.server = &http.Server{
		Addr:    opts.Addr,
		Handler: handler,
	}
}
