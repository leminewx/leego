package leego

import (
	"fmt"
	"net/http"
	"time"

	"gitee.com/leminewx/leego/middleware/metric"
)

const (
	_FORMAT_DATETIME                  = "2006-01-02 15:04:05.000"
	_FORMAT_PRINT_START_HTTP_SERVICE  = "\033[32mINFO \033[0m -  %s  -  HTTP server is starting %s ...\n\n"
	_FORMAT_PRINT_START_HTTPS_SERVICE = "\033[32mINFO \033[0m -  %s  -  HTTPS server is starting %s ...\n\n"
)

// Options 定义引擎配置参数的结构
type Options struct {
	Addr        string
	TLSKeyFile  string
	TLSCertFile string
}

// Engine 定义引擎的结构
type Engine struct {
	*group // 继承路由组，所以引擎本身也是一个路由组

	service  string              // 服务名
	server   *http.Server        // 服务器
	router   *router             // 路由器
	handlers map[string]*handler // 处理器
}

// NewEngine 新建一个引擎
func NewEngine(service string) *Engine {
	engine := &Engine{
		service:  service,
		handlers: make(map[string]*handler),
	}

	engine.group = &group{engine: engine, parent: nil}
	engine.router = newRouter(engine)
	return engine
}

// GetServiceName 获取当前服务的名称
func (own *Engine) GetServiceName() string {
	return own.service
}

// GetCollectors 获取接口指标数据的采集器
func (own *Engine) GetCollectors() []*metric.Collector {
	collectors := make([]*metric.Collector, 0, len(own.handlers))
	for _, handler := range own.handlers {
		collectors = append(collectors, handler.collector.Clone())
	}

	return collectors
}

// ListenAndServe 启动一个 HTTP 服务
func (own *Engine) ListenAndServe(opts *Options) error {
	if err := own.initBeforeServe(opts); err != nil {
		return err
	}

	// 打印并启动 HTTP 服务
	fmt.Printf(_FORMAT_PRINT_START_HTTP_SERVICE, time.Now().Format(_FORMAT_DATETIME), opts.Addr)
	return own.server.ListenAndServe()
}

// ListenAndServeTLS 启动一个 HTTPS 服务
func (own *Engine) ListenAndServeTLS(opts *Options) error {
	if err := own.initBeforeServe(opts); err != nil {
		return err
	}

	// 打印并启动 HTTPS 服务
	fmt.Printf(_FORMAT_PRINT_START_HTTPS_SERVICE, time.Now().Format(_FORMAT_DATETIME), opts.Addr)
	return own.server.ListenAndServeTLS(opts.TLSCertFile, opts.TLSKeyFile)
}

func (own *Engine) initBeforeServe(opts *Options) error {
	// 配置服务器
	own.server = &http.Server{
		Addr:    opts.Addr,
		Handler: own.router,
	}

	return nil
}
