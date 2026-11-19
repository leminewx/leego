package leego

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gitee.com/leminewx/leego/tree"
)

type (
	// Group 定义路由组的接口类型。
	//
	// 路由组用于共享前缀与中间件；Engine 本身也是根路由组，
	// 因此引擎可以直接注册路由，也可以继续派生子组。
	//
	// 命名说明：Group 兼有动词词性（“分组、把……归类”），因此 NewGroup
	// 并不属于“以名词开头”的违规；这里统一使用 NewGroup，是为了让
	// 所有构造类函数都落在同一命名族内。
	Group interface {
		// NewGroup 在当前路由组的基础上创建一个子路由组。
		NewGroup(prefix string) Group
		// UseMiddlewares 追加路由组中间件。
		UseMiddlewares(middlewares ...MiddlewareFunc)

		// GET 注册 GET 路由。
		GET(pattern string, handle HandleFunc, tags ...string)
		// PUT 注册 PUT 路由。
		PUT(pattern string, handle HandleFunc, tags ...string)
		// POST 注册 POST 路由。
		POST(pattern string, handle HandleFunc, tags ...string)
		// HEAD 注册 HEAD 路由。
		HEAD(pattern string, handle HandleFunc, tags ...string)
		// PATCH 注册 PATCH 路由。
		PATCH(pattern string, handle HandleFunc, tags ...string)
		// DELETE 注册 DELETE 路由。
		DELETE(pattern string, handle HandleFunc, tags ...string)
		// OPTIONS 注册 OPTIONS 路由。
		OPTIONS(pattern string, handle HandleFunc, tags ...string)
		// SetStatic 将本地目录挂载为静态文件服务。
		SetStatic(relativePath, rootPath string, tags ...string)
		// SetStaticFS 将自定义文件系统挂载为静态文件服务。
		SetStaticFS(relativePath string, fs http.FileSystem, tags ...string)
	}

	// group 是 Group 的唯一实现。
	//
	// 同一个 group 实例被其所有路由共享，因此除注册期外不得修改字段。
	group struct {
		// engine 指向所属引擎。
		engine *Engine
		// prefix 为路由组前缀。
		prefix string
		// parent 为父路由组；根路由组为 nil。
		parent *group
		// middlewares 为当前路由组的中间件，注册期可追加。
		middlewares []MiddlewareFunc
		// staticPaths 记录已挂载的静态目录，避免同一路径重复挂载。
		staticPaths map[string]struct{}
	}
)

// NewGroup 在当前路由组的基础上创建一个子路由组。
//
// 子组继承父组的前缀与中间件，并拥有独立的中间件列表。
func (own *group) NewGroup(prefix string) Group {
	return &group{
		engine:      own.engine,
		parent:      own,
		prefix:      joinRoutePath(own.prefix, prefix),
		middlewares: make([]MiddlewareFunc, 0, 4),
		staticPaths: make(map[string]struct{}, 2),
	}
}

// UseMiddlewares 追加路由组中间件。
//
// 中间件按追加顺序执行；nil 中间件会被忽略。
// 引擎进入运行期后再追加中间件会 panic，避免运行期修改调用链。
func (own *group) UseMiddlewares(middlewares ...MiddlewareFunc) {
	if own.engine != nil && own.engine.IsPrepared() {
		panic("leego: cannot append middleware after the engine has been prepared")
	}

	for _, middleware := range middlewares {
		if middleware != nil {
			own.middlewares = append(own.middlewares, middleware)
		}
	}
}

// collectMiddlewares 收集从根到当前路由组的中间件。
//
// 参数 stop 指定停止位置（不包含该组），用于排除属于全局链的根中间件。
func (own *group) collectMiddlewares(stop *group, middlewares *[]MiddlewareFunc) {
	if own == nil || own == stop {
		return
	}

	if own.parent != nil {
		own.parent.collectMiddlewares(stop, middlewares)
	}

	*middlewares = append(*middlewares, own.middlewares...)
}

// GET 注册 GET 路由。
func (own *group) GET(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodGet, pattern, handle, "", tags...)
}

// PUT 注册 PUT 路由。
func (own *group) PUT(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPut, pattern, handle, "", tags...)
}

// POST 注册 POST 路由。
func (own *group) POST(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPost, pattern, handle, "", tags...)
}

// HEAD 注册 HEAD 路由。
func (own *group) HEAD(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodHead, pattern, handle, "", tags...)
}

// PATCH 注册 PATCH 路由。
func (own *group) PATCH(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodPatch, pattern, handle, "", tags...)
}

// DELETE 注册 DELETE 路由。
func (own *group) DELETE(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodDelete, pattern, handle, "", tags...)
}

// OPTIONS 注册 OPTIONS 路由。
func (own *group) OPTIONS(pattern string, handle HandleFunc, tags ...string) {
	own.addRoute(http.MethodOptions, pattern, handle, "", tags...)
}

// SetStatic 将本地目录挂载为静态文件服务。
//
// 参数：
//   - relativePath：URL 中的挂载前缀；
//   - rootPath：本地目录，必须存在且为目录；
//   - tags：第一个非空标签作为路由名称，其余作为业务标签。
//
// 该函数会对目录做绝对路径转换，并在目录不存在时 panic，
// 使配置错误在启动阶段暴露，而不是在首次请求时才失败。
func (own *group) SetStatic(relativePath, rootPath string, tags ...string) {
	absolute, err := filepath.Abs(rootPath)
	if err != nil {
		panic(fmt.Sprintf("leego: failed to resolve static path %q: %v", rootPath, err))
	}

	info, err := os.Stat(absolute)
	if err != nil {
		panic(fmt.Sprintf("leego: failed to access static directory %q: %v", absolute, err))
	}

	if !info.IsDir() {
		panic(fmt.Sprintf("leego: static path %q is not a directory", absolute))
	}

	own.setStaticFromFS(relativePath, absolute, http.Dir(absolute), tags...)
}

// SetStaticFS 将自定义文件系统挂载为静态文件服务。
func (own *group) SetStaticFS(relativePath string, fs http.FileSystem, tags ...string) {
	if fs == nil {
		panic("leego: nil static file system")
	}

	own.setStaticFromFS(relativePath, "", fs, tags...)
}

// setStaticFromFS 在给定挂载点上注册 GET/HEAD 静态文件路由。
//
// 同一个路由组可以在不同挂载点上挂载多个静态目录；
// 但同一个挂载点重复挂载会 panic，避免后注册的规则静默覆盖前者。
func (own *group) setStaticFromFS(relativePath, rootPath string, fs http.FileSystem, tags ...string) {
	if strings.Contains(relativePath, ":") || strings.Contains(relativePath, "*") {
		panic("leego: URL parameters can not be used when serving a static folder")
	}

	cleaned := path.Clean("/" + strings.TrimSpace(relativePath))
	if own.staticPaths == nil {
		own.staticPaths = make(map[string]struct{}, 2)
	}

	if _, ok := own.staticPaths[cleaned]; ok {
		panic(fmt.Sprintf("leego: route group %q already serves a static folder on %q", own.prefix, cleaned))
	}

	fullPrefix := joinRoutePath(own.prefix, cleaned)
	handle := own.newStaticHandler(fullPrefix, fs)
	pattern := path.Join(cleaned, "*"+tree.WILDCARD_DEFAULT_NAME)

	own.addRoute(http.MethodGet, pattern, handle, rootPath, tags...)
	own.addRoute(http.MethodHead, pattern, handle, rootPath, tags...)
	own.staticPaths[cleaned] = struct{}{}
}

// newStaticHandler 创建静态文件处理函数。
//
// 安全策略：
//   - 在 path.Clean 之前检查原始路径，保留完整的穿越意图；
//   - 拒绝 ".." 段、NUL 字节、绝对路径与 Windows 盘符路径；
//   - 静态响应补充 nosniff 与 DENY 安全头。
func (own *group) newStaticHandler(prefix string, fileSystem http.FileSystem) HandleFunc {
	fileServer := http.StripPrefix(prefix, http.FileServer(fileSystem))
	return func(ctx *Context) {
		requested := ctx.GetPathValue(tree.WILDCARD_DEFAULT_NAME)
		if requested == "" {
			ctx.ResponseError(http.StatusNotFound, http.StatusText(http.StatusNotFound))
			return
		}

		if containsPathTraversal(requested) {
			ctx.ResponseError(http.StatusForbidden, http.StatusText(http.StatusForbidden))
			return
		}

		// 安全头在写响应之前设置，避免被 FileServer 的隐式 WriteHeader 抢先。
		ctx.SetResponseHeader("X-Content-Type-Options", "nosniff")
		ctx.SetResponseHeader("X-Frame-Options", "DENY")
		fileServer.ServeHTTP(ctx.Response, ctx.Request)
	}
}

// addRoute 注册一条路由。
//
// 该函数在注册期调用，完成以下工作：
//  1. 规范化模式；
//  2. 拒绝运行期注册与重复注册（fail-fast，与 http.ServeMux 一致）；
//  3. 创建处理器并写入路由树与索引。
func (own *group) addRoute(method, pattern string, handle HandleFunc, description string, tags ...string) {
	if own.engine == nil {
		panic("leego: route group is not bound to an engine")
	}

	if own.engine.IsPrepared() {
		panic("leego: cannot register route after the engine has been prepared")
	}

	if handle == nil {
		panic("leego: nil route handler")
	}

	pattern = joinRoutePath(own.prefix, pattern)
	key := method + " " + pattern

	own.engine.handlersMu.Lock()
	if _, ok := own.engine.handlers[key]; ok {
		own.engine.handlersMu.Unlock()
		panic("leego: duplicate route: " + key)
	}

	handler := newHandler(own, method, pattern, handle, tags...)
	own.engine.handlers[key] = handler
	if description == "" {
		own.engine.printBuilder.WriteString(fmt.Sprintf("       - Create | %-7s %s\n", method, pattern))
	} else {
		own.engine.printBuilder.WriteString(fmt.Sprintf("       - Create | %-7s %s  -  filepath: %s\n", method, pattern, description))
	}
	own.engine.handlersMu.Unlock()

	own.engine.router.Insert(method, pattern, handler)
}

// joinRoutePath 拼接路由前缀与模式，并做规范化。
//
// 使用 path.Join 而不是字符串拼接，保证不会出现 "//"、".." 等非法形态；
// 根模式 "/" 单独保留，避免被 path.Join 归一化为 "."。
func joinRoutePath(prefix, pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = "/"
	}

	joined := path.Join(prefix, path.Clean(pattern))
	if joined == "." {
		return "/"
	}

	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}

	return joined
}

// containsPathTraversal 判断路径是否包含路径穿越特征。
func containsPathTraversal(filePath string) bool {
	if filePath == "" {
		return false
	}

	// 反斜杠在 Windows 上是路径分隔符，统一转换后再判断，避免平台差异导致绕过。
	normalized := strings.ReplaceAll(filePath, "\\", "/")

	// NUL 字节可用于截断文件名，一律拒绝。
	if strings.ContainsRune(normalized, '\x00') {
		return true
	}

	// 只有独立的 ".." 段才是穿越，避免误伤 "..foo" 这类合法文件名。
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return true
		}
	}

	// 拒绝绝对路径（含 Windows 盘符形式，如 C:/ 或 //host/share）。
	if path.IsAbs(normalized) || strings.HasPrefix(normalized, "//") {
		return true
	}

	return len(normalized) >= 2 && normalized[1] == ':'
}
