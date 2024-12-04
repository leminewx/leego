package leego

import (
	"fmt"
	"net/http"
	"os"
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
func (own *group) WithStatic(relativePath string, root string, name ...string) *group {
	handler := own.createStaticHandler(relativePath, http.Dir(root))
	own.GET(path.Join(relativePath, "/*"), handler, name...)
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
func (own *group) GET(part string, handle HandleFunc, name ...string) {
	own.addRoute(http.MethodGet, part, handle, name...)
}

// POST 给当前路由组添加一个处理 POST 方法的处理器
func (own *group) POST(part string, handle HandleFunc, name ...string) {
	own.addRoute(http.MethodPost, part, handle, name...)
}

// PUT 给当前路由组添加一个处理 PUT 方法的处理器
func (own *group) PUT(part string, handle HandleFunc, name ...string) {
	own.addRoute(http.MethodPut, part, handle, name...)
}

// DELETE 给当前路由组添加一个处理 DELETE 方法的处理器
func (own *group) DELETE(part string, handle HandleFunc, name ...string) {
	own.addRoute(http.MethodDelete, part, handle, name...)
}

var apiFile *os.File

func init() {
	var err error
	apiFile, err = os.OpenFile("APIS.csv", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		loggo.Error(err.Error())
	}
	if _, err = apiFile.Write([]byte("service,name,method,path\n")); err != nil {
		loggo.Error(err.Error())
	}
}

// addRoute 添加处理器到当前路由组
func (own *group) addRoute(method, pattern string, handle HandleFunc, name ...string) {
	handler := newHandler(own.engine, method, handle)
	handler.WithMiddlewares(own.middlewares...).ServeInit()

	path := own.prefix + pattern
	own.engine.router.Handle(method+" "+path, handler)
	loggo.Info(fmt.Sprintf("Add route: %7s %s", method, path))

	if len(name) > 0 {
		fmt.Fprintf(apiFile, "%s,%s,%s,%s\n", own.engine.service, name[0], method, path)
	} else {
		fmt.Fprintf(apiFile, "%s,,%s,%s\n", own.engine.service, method, path)
	}
}
