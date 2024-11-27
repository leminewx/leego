package leego

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"

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

	groups []*group
	server *http.Server
	router *http.ServeMux
}

// NewEngine 新建一个引擎
func NewEngine() *Engine {
	router := http.NewServeMux()
	engine := &Engine{
		groups: make([]*group, 0),
		router: router,
	}

	engine.group = &group{engine: engine}
	engine.groups = append(engine.groups, engine.group)
	return engine
}

// ListenAndServe 启动一个 HTTP 服务
func (own *Engine) ListenAndServe(opts *Options) {
	own.newServer(opts)
	loggo.Info(fmt.Sprintf("Server is starting %s ...", opts.Addr))
	loggo.Fatal(fmt.Sprintf("Failed to start server: %v", own.server.ListenAndServe()))
}

// ListenAndServeTLS 启动一个 HTTPS 服务
func (own *Engine) ListenAndServeTLS(opts *Options) {
	own.newServer(opts)
	loggo.Info(fmt.Sprintf("Server is starting %s ...", opts.Addr))
	loggo.Fatal(fmt.Sprintf("Failed to start server: %v", own.server.ListenAndServeTLS(opts.TLSCertFile, opts.TLSKeyFile)))
}

func (own *Engine) newServer(opts *Options) {
	own.server = &http.Server{
		Addr:    opts.Addr,
		Handler: recovery(own.router),
	}
}

const _MethodInternalServerError = "500 internal server error"

// recovery 异常恢复处理
func recovery(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				loggo.Error(fmt.Sprintf("%s\n\n", trace(fmt.Sprintf("%v", err))))
				http.Error(resp, _MethodInternalServerError, http.StatusInternalServerError)
			}
		}()

		handler.ServeHTTP(resp, req)
	})
}

func trace(message string) string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:]) // skip first 3 caller

	var str strings.Builder
	str.WriteString(message + "\nTraceback:")
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		file, line := fn.FileLine(pc)
		str.WriteString(fmt.Sprintf("\n\t%s:%d", file, line))
	}

	return str.String()
}
