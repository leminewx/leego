package leego

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gitee.com/leminewx/leego/headers"
	"gitee.com/leminewx/leego/tree"
)

type (
	// Group 定义路由组的接口类型
	Group interface {
		// Group 路由分组
		Group(prefix string) Group
		// UseMiddlewares 使用路由中间件
		UseMiddlewares(middlewares ...Middleware)
		// UseMiddlewareFuncs 使用路由中间件函数
		UseMiddlewareFuncs(fns ...MiddlewareFunc)

		// GET 处理 GET 请求
		GET(pattern string, handle HandleFunc, tags ...string)
		// PUT 处理 PUT 请求
		PUT(pattern string, handle HandleFunc, tags ...string)
		// POST 处理 POST 请求
		POST(pattern string, handle HandleFunc, tags ...string)
		// HEAD 处理 HEAD 请求
		HEAD(pattern string, handle HandleFunc, tags ...string)
		// PATCH 处理 PATCH 请求
		PATCH(pattern string, handle HandleFunc, tags ...string)
		// DELETE 处理 DELETE 请求
		DELETE(pattern string, handle HandleFunc, tags ...string)
		// OPTIONS 处理 OPTIONS 请求
		OPTIONS(pattern string, handle HandleFunc, tags ...string)
		// SetStatic 设置静态文件服务
		SetStatic(relativePath, rootPath string, tags ...string)
		// SetStaticFS 设置静态文件服务
		SetStaticFS(relativePath string, fs http.FileSystem, tags ...string)
	}

	// group 定义路由组的实现类型
	group struct {
		engine *Engine

		prefix      string           // 路由组前缀
		hasStatic   bool             // 是否是静态文件服务路由组
		parent      *group           // 父级路由组
		middlewares []MiddlewareFunc // 路由组中间件
	}
)

// Group 在当前路由组的基础上创建一个新的路由组
func (own *group) Group(prefix string) Group {
	return &group{
		engine:      own.engine,
		parent:      own,
		prefix:      path.Join(own.prefix, prefix), // 使用 path.Join 确保路径正确
		middlewares: make([]MiddlewareFunc, 0, 10), // 预分配容量
	}
}

// UseMiddlewares 使用路由组中间件
func (own *group) UseMiddlewares(middlewares ...Middleware) {
	for _, middleware := range middlewares {
		if middleware == nil {
			continue
		}

		middleware.SetScope(own.prefix)
		own.middlewares = append(own.middlewares, middleware.Handle)
		own.engine.middlewares[middleware.GetID()] = middleware
	}
}

// UseMiddlewareFuncs 使用路由组中间件处理函数
func (own *group) UseMiddlewareFuncs(fns ...MiddlewareFunc) {
	for _, fn := range fns {
		if fn != nil {
			own.middlewares = append(own.middlewares, fn)
		}
	}

}

// getAllMiddlewares 获取链路上的所有中间件
func (own *group) getAllMiddlewares(middlewares *[]MiddlewareFunc) {
	// 先递归父组
	if own.parent != nil {
		own.parent.getAllMiddlewares(middlewares)
	}

	// 然后添加当前路由组的中间件
	*middlewares = append(*middlewares, own.middlewares...)
}

// GET 处理 GET 请求
func (own *group) GET(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodGet, pattern, handle, "", tags...)
}

// PUT 处理 PUT 请求
func (own *group) PUT(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPut, pattern, handle, "", tags...)
}

// POST 处理 POST 请求
func (own *group) POST(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPost, pattern, handle, "", tags...)
}

// HEAD 处理 HEAD 请求
func (own *group) HEAD(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodHead, pattern, handle, "", tags...)
}

// PATCH 处理 PATCH 请求
func (own *group) PATCH(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPatch, pattern, handle, "", tags...)
}

// DELETE 处理 DELETE 请求
func (own *group) DELETE(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodDelete, pattern, handle, "", tags...)
}

// OPTIONS 处理 OPTIONS 请求
func (own *group) OPTIONS(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodOptions, pattern, handle, "", tags...)
}

/*
SetStatic 设置静态文件服务

参数：
  - relativePath：URI 中的相对路径
  - rootPath：静态文件服务的根目录
  - tags：第1个参数为路由名称，后续参数为路由标签

注意：
  - 在处理 index.html 这类默认索引文件时，会尝试重定向到目录路径
*/
func (own *group) SetStatic(relativePath, rootPath string, tags ...string) {
	// 获取绝对路径
	var err error
	if rootPath, err = filepath.Abs(rootPath); err != nil {
		panic(fmt.Sprintf("Failed to get the absolute path for '%s': %v", rootPath, err))
	}

	// 检查目录是否存在
	if info, err := os.Stat(rootPath); err != nil {
		panic(fmt.Sprintf("Failed to access static directory '%s': %v", rootPath, err))
	} else if !info.IsDir() {
		panic(fmt.Sprintf("Static path is not a directory '%s'", rootPath))
	}

	own.setStaticFromFS(relativePath, rootPath, http.Dir(rootPath), tags...)
}

/*
SetStaticFS 设置静态文件服务

参数：
  - relativePath：URI 中的相对路径
  - fs：http.FileSystem 文件系统
  - tags：第1个参数为路由名称，后续参数为路由标签

注意：
  - 在处理 index.html 这类默认索引文件时，会尝试重定向到目录路径
*/
func (own *group) SetStaticFS(relativePath string, fs http.FileSystem, tags ...string) {
	own.setStaticFromFS(relativePath, "", fs, tags...)
}

func (own *group) setStaticFromFS(relativePath, rootPath string, fs http.FileSystem, tags ...string) {
	// 检查是否已经设置过静态文件服务
	if own.hasStatic {
		panic(fmt.Sprintf("Group '%s' is serving a static folder", own.prefix))
	}

	// 检查是否已经设置过静态文件服务
	if strings.Contains(relativePath, ":") || strings.Contains(relativePath, "*") {
		panic("URL parameters can not be used when serving a static folder")
	}

	// 创建处理器
	relativePath = path.Clean("/" + strings.TrimSpace(relativePath))
	handler := own.createStaticHandler(path.Join(own.prefix, relativePath), fs)

	// 创建 GET 和 HEAD 服务接口
	urlPattern := path.Join(relativePath, "/*"+tree.WILDCARD_DEFAULT_NAME)
	own.addRoute(http.MethodGet, urlPattern, handler, rootPath, tags...)
	own.addRoute(http.MethodHead, urlPattern, handler, rootPath, tags...)
	own.hasStatic = true
}

// createStaticHandler 创建静态文件服务的处理器
func (own *group) createStaticHandler(relativePath string, fileSystem http.FileSystem) HandleFunc {
	fileServer := http.StripPrefix(relativePath, http.FileServer(fileSystem))
	return func(ctx *Context) {
		// 获取请求文件
		requestedPath := ctx.GetPathValue(tree.WILDCARD_DEFAULT_NAME)
		if requestedPath == "" {
			ctx.ResponseError(http.StatusNotFound, http.StatusText(http.StatusNotFound))
			return
		}

		// 安全检查：防止路径遍历攻击
		requestedPath = strings.TrimPrefix(strings.TrimPrefix(requestedPath, "/"), "\\")
		requestedPath = path.Clean(requestedPath)
		if containsPathTraversal(requestedPath) {
			ctx.ResponseError(http.StatusForbidden, http.StatusText(http.StatusForbidden))
			return
		}

		// 设置安全头
		ctx.SetResponseHeader(headers.X_CONTENT_TYPE_OPTIONS, "nosniff") // 防止 MIME 类型嗅探攻击
		ctx.SetResponseHeader(headers.X_FRAME_OPTIONS, "DENY")           // 防止点击劫持（ClickJacking）攻击

		// 响应文件
		fileServer.ServeHTTP(ctx.Response, ctx.Request)
	}
}

// addRoute 添加处理器到当前路由组
func (own *group) addRoute(method, pattern string, handle HandleFunc, desc string, tags ...string) {
	// 规范化路径
	pattern = path.Join(own.prefix, path.Clean(strings.TrimSpace(pattern)))
	pattern = strings.ReplaceAll(pattern, "//", "/")

	// 检查接口是否已经创建
	key := method + " " + pattern
	if _, ok := own.engine.handlers[key]; ok {
		own.engine.printBuilder.WriteString(fmt.Sprintf("       - Repeat | %-7s %s\n", method, pattern))
		return
	}

	// 创建处理器
	handler := newHandler(own, method, pattern, handle, tags...)
	// 写入到路由器
	own.engine.router.Insert(method, pattern, handler)
	// 关联到服务引擎
	own.engine.handlers[key] = handler

	if desc == "" {
		own.engine.printBuilder.WriteString(fmt.Sprintf("       - Create | %-7s %s\n", method, pattern))
	} else {
		own.engine.printBuilder.WriteString(fmt.Sprintf("       - Create | %-7s %s  -  filepath: %s\n", method, pattern, desc))
	}
}

var pathTraversalPatterns = []string{"../", "..\\", "./", ".\\", "//", "\\"}

// containsPathTraversal 检查路径是否包含路径遍历攻击
func containsPathTraversal(filePath string) bool {
	// 检查路径中的特殊字符
	for _, pattern := range pathTraversalPatterns {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}

	// 检查绝对路径
	if path.IsAbs(filePath) || strings.Contains(filePath, ":") {
		return true
	}

	return false
}
