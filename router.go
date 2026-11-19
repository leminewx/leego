package leego

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"gitee.com/leminewx/leego/tree"
)

type (
	// Router 定义路由器必须实现的能力。
	//
	// 路由器负责“方法 + 路径 -> 处理器”的映射，并支持：
	//   - 注册期插入与运行期只读查询；
	//   - 冻结后无锁并发查询；
	//   - 405 场景的 Allow 方法收集；
	//   - 路径黑名单。
	Router interface {
		// Insert 注册一条路由；必须在 Freeze 之前调用。
		Insert(method, pattern string, handler Handler)
		// Search 查找匹配的路由；HEAD 请求未注册时会回退到 GET。
		Search(method, path string) (Handler, map[string]string, bool)
		// CollectAllowedMethods 返回该路径下已注册的全部请求方法（含自动 OPTIONS）。
		CollectAllowedMethods(path string) []string
		// AddBlacklist 添加路径黑名单。
		AddBlacklist(urls ...string)
		// IsBlacklisted 判断路径是否被拉黑。
		IsBlacklisted(path string) bool
		// Freeze 冻结路由器，使查询无需加锁。
		Freeze()
	}

	// router 是 Router 的唯一实现。
	//
	// 并发模型：
	//   - 注册期由 mu 保护 roots 的写入；
	//   - 冻结后 frozen 为 true，查询直接读取 roots，不再加锁；
	//   - 黑名单使用 sync.Map，注册期与运行期都可安全读写。
	router struct {
		// engine 指向所属引擎。
		engine *Engine
		// roots 保存“方法 -> 路由树”。
		roots map[string]tree.Tree[Handler]
		// blacklist 保存路径黑名单。
		blacklist sync.Map
		// mu 保护 roots 的写入与冻结前的读取。
		mu sync.RWMutex
		// frozen 标记路由是否已冻结。
		frozen atomic.Bool
	}
)

// newRouter 创建路由器。
func newRouter(engine *Engine) Router {
	return &router{
		engine: engine,
		roots:  make(map[string]tree.Tree[Handler], 8),
	}
}

// Insert 注册一条路由。
//
// 重复注册由上层 group.addRoute 拦截；此处只负责写入对应的路由树。
func (own *router) Insert(method, pattern string, handler Handler) {
	own.mu.Lock()
	defer own.mu.Unlock()

	root, ok := own.roots[method]
	if !ok {
		root = tree.NewTrie[Handler]()
		own.roots[method] = root
	}

	root.Insert(pattern, handler)
}

// Freeze 冻结全部路由树。
//
// 冻结后 Insert 会 panic（由 tree 层保证），查询路径不再加锁。
func (own *router) Freeze() {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.frozen.Load() {
		return
	}

	for _, root := range own.roots {
		_ = root.Freeze()
	}

	own.frozen.Store(true)
}

// Search 查找匹配的路由。
//
// HEAD 请求在未注册 HEAD 路由时会回退到 GET 路由，符合 HTTP 语义：
// HEAD 必须返回与 GET 相同的头，只是不返回消息体（由 net/http 自动处理）。
func (own *router) Search(method, path string) (Handler, map[string]string, bool) {
	if handler, params, ok := own.search(method, path); ok {
		return handler, params, true
	}

	if method == http.MethodHead {
		return own.search(http.MethodGet, path)
	}

	return nil, nil, false
}

// search 在指定方法的路由树中查找。
func (own *router) search(method, path string) (Handler, map[string]string, bool) {
	root, ok := own.getRoot(method)
	if !ok || root == nil {
		return nil, nil, false
	}

	return root.Search(path)
}

// getRoot 返回指定方法的路由树。
func (own *router) getRoot(method string) (tree.Tree[Handler], bool) {
	if own.frozen.Load() {
		root, ok := own.roots[method]
		return root, ok
	}

	own.mu.RLock()
	root, ok := own.roots[method]
	own.mu.RUnlock()
	return root, ok
}

// CollectAllowedMethods 返回指定路径下已注册的请求方法。
//
// 返回结果按字母序排列，并始终包含 OPTIONS（框架会自动响应预检请求）。
// 当路径不存在于任何方法下时返回空切片，调用方据此区分 404 与 405。
func (own *router) CollectAllowedMethods(path string) []string {
	methods := make([]string, 0, 8)

	own.mu.RLock()
	names := make([]string, 0, len(own.roots))
	for method := range own.roots {
		names = append(names, method)
	}
	own.mu.RUnlock()

	allowed := make(map[string]struct{}, len(names)+2)
	for _, method := range names {
		if _, _, ok := own.search(method, path); ok {
			allowed[method] = struct{}{}
		}
	}

	if len(allowed) == 0 {
		return methods
	}

	// GET 可用时，HEAD 必然可用（由 Search 回退实现）。
	if _, ok := allowed[http.MethodGet]; ok {
		allowed[http.MethodHead] = struct{}{}
	}

	allowed[http.MethodOptions] = struct{}{}

	for method := range allowed {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

// AddBlacklist 添加路径黑名单。
func (own *router) AddBlacklist(urls ...string) {
	for _, url := range urls {
		if url = strings.TrimSpace(url); url != "" {
			own.blacklist.Store(url, struct{}{})
		}
	}
}

// IsBlacklisted 判断路径是否被拉黑。
func (own *router) IsBlacklisted(path string) bool {
	_, ok := own.blacklist.Load(path)
	return ok
}

// extractSegmentNames 从路由模式中提取参数名。
//
// 通配段 "*" 使用框架约定的默认名称；":id" 与 "*path" 去掉首字符后作为名称。
func extractSegmentNames(segments []string) []string {
	params := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch {
		case segment == "*":
			params = append(params, tree.WILDCARD_DEFAULT_NAME)
		case strings.HasPrefix(segment, ":"):
			params = append(params, segment[1:])
		case strings.HasPrefix(segment, "*"):
			params = append(params, segment[1:])
		}
	}

	return params
}
