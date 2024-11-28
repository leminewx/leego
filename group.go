package leego

import (
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"gitee.com/leminewx/loggo"
)

// group 定义路由组的结构
type group struct {
	prefix      string
	engine      *Engine
	middlewares []MiddlewareFunc
}

// Group 在当前路由组的基础上创建一个新的路由组
func (own *group) Group(prefix string) *group {
	group := &group{
		prefix:      own.prefix + prefix, // join new group name
		engine:      own.engine,
		middlewares: append(make([]MiddlewareFunc, 0), own.engine.middlewares...),
	}

	own.engine.groups = append(own.engine.groups, group) // add new group
	return group
}

// WithMiddlewares 给当前路由组添加中间件
func (own *group) WithMiddlewares(middlewares ...MiddlewareFunc) *group {
	own.middlewares = append(own.middlewares, middlewares...)
	return own
}

// WithStatic 给当前路由组添加静态文件服务接口
func (own *group) WithStatic(relativePath string, root string) *group {
	handler := own.createStaticHandler(relativePath, http.Dir(root))
	own.GET(path.Join(relativePath, "/*"), handler)
	return own
}

func (own *group) createStaticHandler(relativePath string, fs http.FileSystem) HandleFunc {
	startIndex := len(strings.Split(own.prefix+relativePath, "/"))
	fileServer := http.StripPrefix(path.Join(own.prefix, relativePath), http.FileServer(fs))
	return func(ctx *Context) {
		paths := strings.Split(ctx.GetURL().Path, "/")
		if len(paths) < startIndex-1 {
			ctx.WriteCode(http.StatusNotFound)
			return
		}

		if _, err := fs.Open(filepath.Join(paths[startIndex:]...)); err != nil {
			ctx.WriteCode(http.StatusNotFound)
			return
		}

		fileServer.ServeHTTP(ctx.GetResponse(), ctx.GetRequest())
	}
}

// GET 给当前路由组添加一个处理 GET 方法的处理器
func (own *group) GET(part string, handle HandleFunc) {
	own.addRoute(http.MethodGet, part, handle)
}

// POST 给当前路由组添加一个处理 POST 方法的处理器
func (own *group) POST(part string, handle HandleFunc) {
	own.addRoute(http.MethodPost, part, handle)
}

// PUT 给当前路由组添加一个处理 PUT 方法的处理器
func (own *group) PUT(part string, handle HandleFunc) {
	own.addRoute(http.MethodPut, part, handle)
}

// DELETE 给当前路由组添加一个处理 DELETE 方法的处理器
func (own *group) DELETE(part string, handle HandleFunc) {
	own.addRoute(http.MethodDelete, part, handle)
}

// addRoute 添加处理器到当前路由组
func (own *group) addRoute(method, pattern string, handle HandleFunc) {
	handler := newHandler(method, handle)
	handler.WithMiddlewares(own.middlewares...).ServeInit()
	own.engine.router.Handle(method+" "+own.prefix+pattern, handler)
	loggo.Info(fmt.Sprintf("Add route: %7s %s", method, own.prefix+pattern))
}
