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
	hasStatic   bool
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

// WithStatics 给当前路由组添加静态文件服务接口
func (own *group) WithStatics(relativePath string, root string, nameAndType ...string) *group {
	if !own.hasStatic {
		handler := own.createStaticHandler(relativePath, http.Dir(root))
		own.GET(path.Join(relativePath, "/*"), handler, nameAndType...)
		own.hasStatic = true
	}

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
func (own *group) GET(part string, handle HandleFunc, nameAndType ...string) {
	own.addRoute(http.MethodGet, part, handle, nameAndType...)
}

// POST 给当前路由组添加一个处理 POST 方法的处理器
func (own *group) POST(part string, handle HandleFunc, nameAndType ...string) {
	own.addRoute(http.MethodPost, part, handle, nameAndType...)
}

// PUT 给当前路由组添加一个处理 PUT 方法的处理器
func (own *group) PUT(part string, handle HandleFunc, nameAndType ...string) {
	own.addRoute(http.MethodPut, part, handle, nameAndType...)
}

// DELETE 给当前路由组添加一个处理 DELETE 方法的处理器
func (own *group) DELETE(part string, handle HandleFunc, nameAndType ...string) {
	own.addRoute(http.MethodDelete, part, handle, nameAndType...)
}

// addRoute 添加处理器到当前路由组
func (own *group) addRoute(method, pattern string, handle HandleFunc, nameAndType ...string) {
	pattern = own.prefix + pattern
	key := method + " " + pattern

	// 重复添加接口，则直接返回
	if _, ok := own.engine.apis[key]; ok {
		return
	}

	// 写入 API 接口基本信息
	switch len(nameAndType) {
	case 0:
		own.engine.apis[key] = &api{Service: own.engine.service, Method: method, Pattern: pattern}
	case 1:
		own.engine.apis[key] = &api{Service: own.engine.service, Name: nameAndType[0], Method: method, Pattern: pattern}
	default:
		own.engine.apis[key] = &api{Service: own.engine.service, Name: nameAndType[0], Type: nameAndType[1], Method: method, Pattern: pattern}
	}

	// 创建处理器
	handler := newHandler(own.engine, method, handle)
	handler.WithMiddlewares(own.middlewares...).InitBeforeServe() // 初始化处理器中间件
	own.engine.router.Handle(key, handler)
	loggo.Info(fmt.Sprintf("Add route: %7s %s", method, pattern))
}
