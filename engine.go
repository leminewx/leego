package leego

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const _FORMAT_DATETIME = "2006-01-02 15:04:05.000"

// Engine 定义引擎的结构
type Engine struct {
	// 继承路由组，所以引擎本身也是一个路由组
	*group

	// Name 引擎名称
	Name string
	// MaxMultipartMemory 解析 multipart/form-data 多部分表单时最大使用的内存
	MaxMultipartMemory int64
	// UseH2C 是否使用 HTTP2
	UseH2C bool

	router       Router                // 路由器
	server       *http.Server          // 服务器
	handlers     map[string]Handler    // 处理器
	middlewares  map[string]Middleware // 中间件
	printBuilder *strings.Builder
}

// NewEngine 新建一个引擎
func NewEngine() *Engine {
	checkGoVersion()

	engine := &Engine{
		MaxMultipartMemory: 32 << 20, // 默认 32 MB
		handlers:           make(map[string]Handler),
		middlewares:        make(map[string]Middleware),
		printBuilder:       &strings.Builder{},
	}

	engine.group = &group{
		engine: engine,
		parent: nil,
	}

	engine.router = newRouter(engine)
	engine.printBuilder.WriteString("Creating routes:\n")
	return engine
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
	for _, url := range urls {
		if url = strings.TrimSpace(url); url != "" {
			own.router.AddBlacklist(url)
		}
	}
}

// EnableRoute 启用指定路由
func (own *Engine) EnableRoute(method, path string) {
	key := method + " " + path
	if handler, ok := own.handlers[key]; ok {
		handler.Enable()
	}
}

/*
DisableRoute 禁用指定路由
  - 禁用指定路由后，也不会调用该路由上的任一中间件，且响应状态码为 423
*/
func (own *Engine) DisableRoute(method, path string) {
	key := method + " " + path
	if handler, ok := own.handlers[key]; ok {
		handler.Disable()
	}
}

// Run 通过指定端口监听和服务 HTTP 请求
func (own *Engine) Run(addr string) error {
	// 创建 HTTP 服务
	own.server = &http.Server{
		Addr:    addr,
		Handler: own.GetHandler(),
	}

	// 打印路由信息
	own.printBuilder.WriteByte('\n')
	logging.INFO(own.printBuilder.String())
	own.printBuilder.Reset()

	// 启动 HTTP 服务
	logging.INFO("Listening and serving HTTP %s ...\n\n", own.server.Addr)
	return own.server.ListenAndServe()
}

// RunTLS 通过指定端口监听和服务 HTTPS 请求
func (own *Engine) RunTLS(addr, certFile, keyFile string) error {
	// 创建 HTTP 服务
	own.server = &http.Server{
		Addr:    addr,
		Handler: own.GetHandler(),
	}

	// 打印路由信息
	own.printBuilder.WriteByte('\n')
	logging.INFO(own.printBuilder.String())
	own.printBuilder.Reset()

	// 启动 HTTP 服务
	logging.INFO("Listening and serving HTTPS %s ...\n\n", own.server.Addr)
	return own.server.ListenAndServeTLS(certFile, keyFile)
}

// RunUnix 使用 Unix 套接字监听和服务 HTTP 请求
func (own *Engine) RunUnix(file string) error {
	// 创建监听套接字
	listener, err := net.Listen("unix", file)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(file)
	}()

	// 打印路由信息
	own.printBuilder.WriteByte('\n')
	logging.INFO(own.printBuilder.String())
	own.printBuilder.Reset()

	// 启动 HTTP 服务
	logging.INFO("Listening and serving HTTP on unix:%s ...\n\n", file)
	return http.Serve(listener, own.GetHandler())
}

// RunFd 通过指定的文件描述符监听和服务 HTTP 请求
func (own *Engine) RunFd(fd int) (err error) {
	// 创建监听套接字
	f := os.NewFile(uintptr(fd), fmt.Sprintf("fd@%d", fd))
	listener, err := net.FileListener(f)
	if err != nil {
		return
	}
	defer func() { _ = listener.Close() }()

	// 启动 HTTP 服务
	logging.INFO("Listening and serving HTTP on fd@%s ...\n\n", fd)
	return http.Serve(listener, own.GetHandler())
}

// RunListener 通过指定的网络监听和服务 HTTP 请求
func (own *Engine) RunListener(listener net.Listener) (err error) {
	logging.INFO("Listening and serving HTTP on listener what's bind with address@%s", listener.Addr())
	return http.Serve(listener, own.GetHandler())
}

// GetHandler 获取引擎处理器
func (own *Engine) GetHandler() http.Handler {
	if !own.UseH2C {
		return own
	}

	h2s := &http2.Server{}
	return h2c.NewHandler(own, h2s)
}

// ServeHTTP 继承 http.Handler 接口
func (own *Engine) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	own.router.ServeHTTP(resp, req)
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
	fmt.Println()
	if err := own.server.Shutdown(ctx); err != nil {
		logging.WARN("Failed to shutdown server : %v\n", err)
		return err
	}

	logging.INFO("Shutdown server successfully\n")
	return nil
}

// checkGoVersion 检查 Go 版本信息
func checkGoVersion() {
	// 获取 Go 版本信息
	ver := runtime.Version()
	if after, ok := strings.CutPrefix(ver, "go"); ok {
		ver = after
	}

	// 检查 Go 版本（1.25+）
	if m, s, _, e := getGoVersionParts(ver); e == nil && (m < 1 || s < 25) {
		logging.WARN("Leego requires Go 1.25+, but now Go version is %s.\n\n", ver)
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
