package leego

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const _FORMAT_DATETIME = "2006-01-02 15:04:05.000"

// Options 定义引擎配置参数的结构
type Options struct {
	Addr        string
	TLSCertFile string
	TLSKeyFile  string
}

// Engine 定义引擎的结构
type Engine struct {
	*group // 继承路由组，所以引擎本身也是一个路由组

	service string              // 服务名
	server  *http.Server        // 服务器
	router  *router             // 路由器
	apis    map[string][]string // API接口
}

// NewEngine 新建一个引擎
func NewEngine(service string) *Engine {
	engine := &Engine{
		service: service,
		apis:    make(map[string][]string),
	}

	engine.group = &group{engine: engine, parent: nil}
	engine.router = newRouter(engine)
	return engine
}

// GetServiceName 获取当前服务的名称
func (own *Engine) GetServiceName() string {
	return own.service
}

// ListenAndServe 启动一个 HTTP 服务
func (own *Engine) ListenAndServe(opts *Options) error {
	if err := own.initBeforeServe(opts); err != nil {
		return err
	}

	fmt.Printf("\033[32mINFO \033[0m -  %s  -  HTTP server is starting %s ...\n\n", time.Now().Format(_FORMAT_DATETIME), opts.Addr)
	return own.server.ListenAndServe()
}

// ListenAndServeTLS 启动一个 HTTPS 服务
func (own *Engine) ListenAndServeTLS(opts *Options) error {
	if err := own.initBeforeServe(opts); err != nil {
		return err
	}

	fmt.Printf("\033[32mINFO \033[0m -  %s  -  HTTPS server is starting %s ...\n\n", time.Now().Format(_FORMAT_DATETIME), opts.Addr)
	return own.server.ListenAndServeTLS(opts.TLSCertFile, opts.TLSKeyFile)
}

func (own *Engine) initBeforeServe(opts *Options) error {
	// 保存接口的基本元信息
	if err := own.generateApiMetadata(); err != nil {
		return err
	}

	// 配置服务器
	own.server = &http.Server{
		Addr:    opts.Addr,
		Handler: own.router,
	}

	return nil
}

func (own *Engine) generateApiMetadata() error {
	// 打开文件
	file, err := os.OpenFile("APIS.csv", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("leego: failed to open the APIS.csv: %v", err)
	}
	defer func() { _ = file.Close() }()

	// 写入头部
	if _, err = file.Write([]byte("service,name,method,pattern\n")); err != nil {
		return fmt.Errorf("leego: failed to write the header of the APIS.csv: %v", err)
	}

	// 写入API信息
	for _, api := range own.apis {
		if _, err := fmt.Fprintf(file, "%s\n", strings.Join(api, ",")); err != nil {
			return fmt.Errorf("leego: failed to write a API info to the APIS.csv: %v", err)
		}
	}

	return nil
}
