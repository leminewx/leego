package leego

import (
	"fmt"
	"net/http"
	"os"

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

	service      string            // 服务名
	interceptors []InterceptorFunc // 拦截器
	groups       []*group          // 路由组
	server       *http.Server      // 服务器
	router       *http.ServeMux    // 路由器
	apis         map[string]*api   // API接口
}

// NewEngine 新建一个引擎
func NewEngine(service string) *Engine {
	engine := &Engine{
		service:      service,
		interceptors: make([]InterceptorFunc, 0),
		groups:       make([]*group, 0),
		router:       http.NewServeMux(),
		apis:         make(map[string]*api),
	}

	engine.group = &group{engine: engine}
	engine.groups = append(engine.groups, engine.group)
	return engine
}

// NewDefaultEngine 新建一个默认 HTTP/HTTPS 引擎，支持异常回复和日志
func NewDefaultEngine(service string) *Engine {
	engine := NewEngine(service)
	engine.WithInterceptors(Recover(), Log())
	return engine
}

// GetServiceName 获取当前服务的名称
func (own *Engine) GetServiceName() string {
	return own.service
}

// WithInterceptors 添加拦截器
func (own *Engine) WithInterceptors(interceptors ...InterceptorFunc) *Engine {
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
	// 保存基本的接口信息
	own.saveApis()

	// 加载拦截器
	var handler http.Handler = own.router
	for i := len(own.interceptors); i > 0; i-- {
		handler = own.interceptors[i-1](handler)
	}

	// 配置服务器
	own.server = &http.Server{
		Addr:    opts.Addr,
		Handler: handler,
	}
}

func (own *Engine) saveApis() {
	file, err := os.OpenFile("APIS.csv", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		loggo.Error(err.Error())
	}
	defer file.Close()

	if _, err = file.Write([]byte("service,type,name,method,pattern\n")); err != nil {
		loggo.Error(err.Error())
	}

	for _, api := range own.apis {
		if _, err := fmt.Fprintf(file, "%s,%s,%s,%s,%s\n", api.Service, api.Type, api.Name, api.Method, api.Pattern); err != nil {
			loggo.Error(err.Error())
		}
	}
}
