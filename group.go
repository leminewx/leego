package leego

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/trie"
)

const (
	_FORMAT_PRINT_EXISTS_ROUTE               = "\033[33mWARN \033[0m -  %s  -  Exists route: %7s: %s\n"
	_FORMAT_PRINT_CREATE_ROUTE               = "\033[32mINFO \033[0m -  %s  -  Create route: %7s: %s\n"
	_FORMAT_PRINT_CREATE_ROUTE_WITH_FILEPATH = "\033[32mINFO \033[0m -  %s  -  Create route: %7s: %s  -  filepath: %s\n"
)

// group 定义路由组的数据结构
type group struct {
	engine *Engine

	prefix      string           // 路由组前缀
	hasStatic   bool             // 是否是静态文件服务路由组
	parent      *group           // 父级路由组
	middlewares []MiddlewareFunc // 路由组中间件
}

// Group 在当前路由组的基础上创建一个新的路由组
func (own *group) Group(prefix string) *group {
	return &group{
		engine:      own.engine,
		parent:      own,
		prefix:      path.Join(own.prefix, prefix), // 使用 path.Join 确保路径正确
		middlewares: make([]MiddlewareFunc, 0, 10), // 预分配容量
	}
}

// WithMiddlewares 添加路由组中间件
func (own *group) WithMiddlewares(middlewares ...Middleware) *group {
	for _, middleware := range middlewares {
		if middleware != nil {
			middleware.SetScope(own.prefix)
			own.middlewares = append(own.middlewares, middleware.Handle)
		}
	}

	return own
}

// WithMiddlewareFuncs 添加路由组中间件处理函数
func (own *group) WithMiddlewareFuncs(funcs ...MiddlewareFunc) *group {
	for _, middleware := range funcs {
		if middleware != nil {
			own.middlewares = append(own.middlewares, middleware)
		}
	}

	return own
}

// getAllMiddlewares 获取所有中间件
func (own *group) getAllMiddlewares(middlewares *[]MiddlewareFunc) {
	// 先递归父组
	if own.parent != nil {
		own.parent.getAllMiddlewares(middlewares)
	}

	// 然后添加当前路由组的中间件
	*middlewares = append(*middlewares, own.middlewares...)
}

/*
SetStatics 设置路由组的静态文件服务接口
  - relativePath：URI 中的相对路径
  - rootPath：静态文件服务的根目录
  - cache：是否允许前端缓存（最大缓存1小时）
  - tags：第1个参数为路由名称，后续参数为路由标签
*/
func (own *group) SetStatics(relativePath, rootPath string, cache bool, tags ...string) (err error) {
	// 检查是否已经设置过静态文件服务
	if own.hasStatic {
		return fmt.Errorf("leego: static: exists a has a static file service in group '%s'", own.prefix)
	}

	// 获取绝对路径
	if rootPath, err = filepath.Abs(rootPath); err != nil {
		return fmt.Errorf("leego: static: failed to get the absolute path for '%s': %w", rootPath, err)
	}

	// 检查目录是否存在
	if info, err := os.Stat(rootPath); err != nil {
		return fmt.Errorf("leego: static: failed to access static directory '%s': %w", rootPath, err)
	} else if !info.IsDir() {
		return fmt.Errorf("leego: static: static path is not a directory '%s'", rootPath)
	}

	// 创建静态文件服务的处理器，同时支持 GET 和 HEAD 方法
	relativePath = normalizePath(relativePath)
	handler := own.createStaticHandler(path.Join(own.prefix, relativePath), http.Dir(rootPath), cache)

	wildcardName := "*" + trie.WILDCARD_DEFAULT_NAME
	own.addRoute(http.MethodGet, path.Join(relativePath, wildcardName), rootPath, handler, tags...)
	own.addRoute(http.MethodHead, path.Join(relativePath, wildcardName), rootPath, handler, tags...)
	own.hasStatic = true

	return
}

// createStaticHandler 创建静态文件服务的处理器
func (own *group) createStaticHandler(relativePath string, fileSystem http.FileSystem, cache bool) HandleFunc {
	fileServer := http.StripPrefix(relativePath, http.FileServer(fileSystem))
	return func(ctx *Context) {
		// 获取请求的文件路径
		requestedPath := ctx.GetPathValue(trie.WILDCARD_DEFAULT_NAME)
		if requestedPath == "" {
			ctx.ResponseError(http.StatusNotFound, response.STATUS_404_NOT_FOUND)
			return
		}

		// 规范化路径：移除开头的斜杠，防止路径遍历攻击
		requestedPath = strings.TrimPrefix(requestedPath, "/")
		requestedPath = strings.TrimPrefix(requestedPath, "\\")

		// 安全检查：防止路径遍历攻击
		if containsPathTraversal(requestedPath) {
			ctx.ResponseError(http.StatusForbidden, response.STATUS_403_FORBIDDEN)
			return
		}

		// 检查文件是否存在且可访问
		file, err := fileSystem.Open(requestedPath)
		if err != nil {
			ctx.ResponseError(http.StatusNotFound, response.STATUS_404_NOT_FOUND)
			return
		}
		defer func() { _ = file.Close() }()

		// 设置缓存头
		if !cache {
			ctx.WithResponseHeader("Cache-Control", "no-store, no-cache, must-revalidate") // 不缓存
		} else {
			ctx.WithResponseHeader("Cache-Control", "public, max-age=3600") // 缓存1小时
		}

		// 设置安全头
		ctx.WithResponseHeader("X-Content-Type-Options", "nosniff") // 防止 MIME 类型嗅探攻击
		ctx.WithResponseHeader("X-Frame-Options", "DENY")           // 防止点击劫持（ClickJacking）攻击

		// 响应文件
		fileServer.ServeHTTP(ctx.Response, ctx.Request)
	}
}

// GET 添加一个处理 GET 方法的处理器
func (own *group) GET(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodGet, pattern, "", handle, tags...)
}

// PUT 添加一个处理 PUT 方法的处理器
func (own *group) PUT(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPut, pattern, "", handle, tags...)
}

// HEAD 添加一个处理 HEAD 方法的处理器
func (own *group) HEAD(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodHead, pattern, "", handle, tags...)
}

// POST 添加一个处理 POST 方法的处理器
func (own *group) POST(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPost, pattern, "", handle, tags...)
}

// PATCH 添加一个处理 PATCH 方法的处理器
func (own *group) PATCH(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPatch, pattern, "", handle, tags...)
}

// DELETE 添加一个处理 DELETE 方法的处理器
func (own *group) DELETE(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodDelete, pattern, "", handle, tags...)
}

// OPTIONS 添加一个处理 OPTIONS 方法的处理器
func (own *group) OPTIONS(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodOptions, pattern, "", handle, tags...)
}

// addRoute 添加处理器到当前路由组
func (own *group) addRoute(method, pattern, desc string, handle HandleFunc, tags ...string) {
	// 规范化路径
	pattern = path.Join(own.prefix, normalizePath(pattern))
	pattern = strings.ReplaceAll(pattern, "//", "/") // 移除可能出现的双斜杠

	// 检查接口是否已经创建
	key := method + " " + pattern
	if _, ok := own.engine.handlers[key]; ok {
		fmt.Printf(_FORMAT_PRINT_EXISTS_ROUTE, time.Now().Format(_FORMAT_DATETIME), method, pattern)
		return
	}

	// 创建处理器
	handler := newHandler(own, method, pattern, handle, tags...)
	// 写入到路由器
	own.engine.router.Insert(method, pattern, handler)
	// 关联到服务引擎
	own.engine.handlers[key] = handler

	// 输出信息
	if desc != "" {
		fmt.Printf(_FORMAT_PRINT_CREATE_ROUTE_WITH_FILEPATH, time.Now().Format(_FORMAT_DATETIME), method, pattern, desc)
	} else {
		fmt.Printf(_FORMAT_PRINT_CREATE_ROUTE, time.Now().Format(_FORMAT_DATETIME), method, pattern)
	}
}

// normalizePath 规范化路径，确保以 / 开头且不包含多余的斜杠
func normalizePath(p string) string {
	p = path.Clean("/" + strings.TrimSpace(p))
	if p == "/" {
		return ""
	}

	return p
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
