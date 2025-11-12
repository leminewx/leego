package leego

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gitee.com/leminewx/leego/logging"
	"gitee.com/leminewx/leego/middleware/monitor"
)

const (
	_FORMAT_DATETIME                       = "2006-01-02 15:04:05.000"
	_FORMAT_PRINT_START_HTTP_SERVICE       = "\033[32mINFO \033[0m -  %s  -  HTTP server '%s' is starting %s ...\n\n"
	_FORMAT_PRINT_START_HTTPS_SERVICE      = "\033[32mINFO \033[0m -  %s  -  HTTPS server '%s' is starting %s ...\n\n"
	_FORMAT_PRINT_FAILED_SHUTDOWN_SERVICE  = "\033[35mERROR\033[0m -  %s  -  Failed to shutdown server '%s': %v\n\n"
	_FORMAT_PRINT_SUCCESS_SHUTDOWN_SERVICE = "\033[32mINFO \033[0m -  %s  -  Server '%s' shutdown successfully.\n\n"
)

// Engine 定义引擎的结构
type Engine struct {
	*group // 继承路由组，所以引擎本身也是一个路由组

	name        string                // 服务名
	router      Router                // 路由器
	server      *http.Server          // 服务器
	handlers    map[string]Handler    // 处理器
	middlewares map[string]Middleware // 中间件
}

// NewEngine 新建一个引擎
func NewEngine() *Engine {
	checkGoVersion()

	engine := &Engine{
		handlers:    make(map[string]Handler),
		middlewares: make(map[string]Middleware),
	}

	engine.router = newRouter(engine)
	engine.group = &group{
		engine: engine,
		parent: nil,
	}

	return engine
}

// WithName 设置服务名称
func (own *Engine) WithName(name string) *Engine {
	own.name = name
	return own
}

// GetName 获取服务名称
func (own *Engine) GetName() string {
	return own.name
}

// GetMiddleware 获取指定的中间件
func (own *Engine) GetMiddleware(id string) (Middleware, bool) {
	m, ok := own.middlewares[id]
	return m, ok
}

// GetAllMiddlewares 获取所有中间件
func (own *Engine) GetAllMiddlewares() []Middleware {
	middlewares := make([]Middleware, 0, len(own.middlewares))
	for _, m := range own.middlewares {
		middlewares = append(middlewares, m)
	}

	return middlewares
}

// GetAllRouteMonitors 获取所有路由的监控器
func (own *Engine) GetAllRouteMonitors() []*monitor.Monitor {
	monitors := make([]*monitor.Monitor, 0, len(own.handlers))
	for _, handler := range own.handlers {
		monitors = append(monitors, handler.GetMonitor().Clone())
	}

	return monitors
}

// AddURLBlacklist 添加路由黑名单
func (own *Engine) AddURLBlacklist(urls ...string) {
	own.router.AddBlacklist(urls...)
}

// EnableRoute 启用指定路由
func (own *Engine) EnableRoute(method, path string) *Engine {
	key := method + " " + path
	if handler, ok := own.handlers[key]; ok {
		handler.Enable()
	}

	return own
}

/*
DisableRoute 禁用指定路由
  - 禁用指定路由后，也不会调用该路由上的任一中间件，且响应状态码为 423
*/
func (own *Engine) DisableRoute(method, path string) *Engine {
	key := method + " " + path
	if handler, ok := own.handlers[key]; ok {
		handler.Disable()
	}

	return own
}

// Run 启动 HTTP 服务
func (own *Engine) Run(addr string) error {
	// 创建 HTTP 服务
	own.server = &http.Server{
		Addr:    addr,
		Handler: own.router,
	}

	// 启动 HTTP 服务
	fmt.Printf(_FORMAT_PRINT_START_HTTP_SERVICE, time.Now().Format(_FORMAT_DATETIME), own.name, addr)
	return own.server.ListenAndServe()
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
		if own.name != "" {
			logging.WARN("Failed to shutdown server '%s': %v\n", own.name, err)
		} else {
			logging.WARN("Failed to shutdown server: %v\n", err)
		}
		return err
	}

	if own.name != "" {
		logging.INFO("Shutdown server '%s' successfully\n", own.name)
	} else {
		logging.INFO("Shutdown server successfully\n")
	}

	return nil
}

// checkGoVersion 检查 Go 版本信息
func checkGoVersion() {
	ver := runtime.Version()
	if strings.HasPrefix(ver, "go") {
		ver = strings.TrimPrefix(ver, "go")
	}

	if m, s, _, e := getGoVersionParts(ver); e == nil && (m < 1 || s < 25) {
		logging.WARN("Leego requires Go 1.25+, but now Go version is %s.\n", ver)
	}
}

// getGoVersionParts 获取 Go 版本信息
func getGoVersionParts(ver string) (main, sub, revised uint64, err error) {
	parts := strings.Split(ver, ".")
	switch len(parts) {
	case 2:
		main, err = strconv.ParseUint(parts[0], 10, 64)
		sub, err = strconv.ParseUint(parts[1], 10, 64)
	case 3:
		main, err = strconv.ParseUint(parts[0], 10, 64)
		sub, err = strconv.ParseUint(parts[1], 10, 64)
		revised, err = strconv.ParseUint(parts[2], 10, 64)
	default:
		err = errors.New("invalid version")
	}

	return
}
