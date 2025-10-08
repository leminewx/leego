package leego

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitee.com/leminewx/leego/middleware/metric"
)

const (
	_FORMAT_DATETIME                       = "2006-01-02 15:04:05.000"
	_FORMAT_PRINT_START_HTTP_SERVICE       = "\033[32mINFO \033[0m -  %s  -  HTTP server '%s' is starting %s ...\n\n"
	_FORMAT_PRINT_START_HTTPS_SERVICE      = "\033[32mINFO \033[0m -  %s  -  HTTPS server '%s' is starting %s ...\n\n"
	_FORMAT_PRINT_FAILED_SHUTDOWN_SERVICE  = "\033[35mINFO \033[0m -  %s  -  Failed to shutdown server '%s': %v\n\n"
	_FORMAT_PRINT_SUCCESS_SHUTDOWN_SERVICE = "\033[32mINFO \033[0m -  %s  -  Server '%s' shutdown successfully.\n\n"
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

	name     string              // 服务名
	router   *router             // 路由器
	server   *http.Server        // 服务器
	handlers map[string]*handler // 处理器
}

// NewEngine 新建一个引擎
func NewEngine(service string) *Engine {
	engine := &Engine{
		name:     service,
		handlers: make(map[string]*handler),
	}

	engine.group = &group{engine: engine, parent: nil}
	engine.router = newRouter(engine)
	return engine
}

// GetServiceName 获取当前服务的名称
func (own *Engine) GetServiceName() string {
	return own.name
}

// GetCollectors 获取接口的指标采集数据
func (own *Engine) GetCollectors() []*metric.Collector {
	collectors := make([]*metric.Collector, 0, len(own.handlers))
	for _, handler := range own.handlers {
		collectors = append(collectors, handler.collector.Clone())
	}

	return collectors
}

// GetMetadatas 获取接口的元数据信息
func (own *Engine) GetMetadatas() []*metric.Metadata {
	metadatas := make([]*metric.Metadata, 0, len(own.handlers))
	for _, handler := range own.handlers {
		metadatas = append(metadatas, handler.metadata)
	}

	return metadatas
}

// WithURLBlacklist 添加 URL 黑名单
func (own *Engine) WithURLBlacklist(urls ...string) *Engine {
	own.router.WithURLBlacklist(urls...)
	return own
}

// ListenAndServe 启动一个 HTTP 服务
func (own *Engine) ListenAndServe(opts *Options) error {
	if err := own.initBeforeServe(opts); err != nil {
		return err
	}

	// 异步启动 HTTP 服务
	fmt.Printf(_FORMAT_PRINT_START_HTTP_SERVICE, time.Now().Format(_FORMAT_DATETIME), own.name, opts.Addr)
	return own.server.ListenAndServe()
}

// ListenAndServeTLS 启动一个 HTTPS 服务
func (own *Engine) ListenAndServeTLS(opts *Options) error {
	if err := own.initBeforeServe(opts); err != nil {
		return err
	}

	// 异步启动 HTTPS 服务
	fmt.Printf(_FORMAT_PRINT_START_HTTPS_SERVICE, time.Now().Format(_FORMAT_DATETIME), own.name, opts.Addr)

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

// Shutdown 优雅关闭服务
func (own *Engine) Shutdown(timeout time.Duration) error {
	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 创建一个超时 context.Context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 优雅关闭服务器
	if err := own.server.Shutdown(ctx); err != nil {
		fmt.Printf(_FORMAT_PRINT_FAILED_SHUTDOWN_SERVICE, time.Now().Format(_FORMAT_DATETIME), own.name, err)
		return err
	}

	fmt.Printf(_FORMAT_PRINT_SUCCESS_SHUTDOWN_SERVICE, time.Now().Format(_FORMAT_DATETIME), own.name)
	return nil
}
