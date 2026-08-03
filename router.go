package leego

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/tree"
)

type (
	// Router 定义路由器的接口类型
	Router interface {
		// ServeHTTP 实现 http.Handler 接口
		ServeHTTP(resp http.ResponseWriter, req *http.Request)
		// Insert 插入路由
		Insert(method, pattern string, handler Handler)
		// Search 搜索路由
		Search(method, path string) (Handler, map[string]string, bool)
		// AddBlacklist 添加路由黑名单
		AddBlacklist(urls ...string)
	}

	// router 定义路由器的实现类型
	router struct {
		engine *Engine

		roots    map[string]tree.Tree[Handler] // 不同方法对应不同的路由树
		backlist sync.Map                      // 路由黑名单

		insertLock sync.Mutex
	}
)

// newRouter 创建一个新的路由器
func newRouter(engine *Engine) Router {
	return &router{
		engine:   engine,
		roots:    make(map[string]tree.Tree[Handler]),
		backlist: sync.Map{},
	}
}

// Insert 插入新增的路由
func (own *router) Insert(method, pattern string, handler Handler) {
	own.insertLock.Lock()
	defer own.insertLock.Unlock()

	// 若方法对应的路由树不存在，则创建
	root, ok := own.roots[method]
	if !ok {
		root = tree.NewTrie[Handler]()
		own.roots[method] = root
	}

	// 插入路由
	root.Insert(pattern, handler)
}

// Search 搜索匹配的路由
func (own *router) Search(method, path string) (Handler, map[string]string, bool) {
	// 找到路由根节点
	root, ok := own.roots[method]
	if !ok {
		return nil, nil, false
	}

	// 搜索路由
	return root.Search(path)
}

// AddBlacklist 添加路由黑名单
func (own *router) AddBlacklist(urls ...string) {
	for _, url := range urls {
		if url = strings.TrimSpace(url); url != "" {
			own.backlist.Store(url, true)
		}
	}
}

// ServeHTTP 实现 http.Handler 接口
func (own *router) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	// 黑名单检查
	if _, ok := own.backlist.Load(path); ok {
		http.Error(resp, response.GetStatusLowerText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	// 匹配路由
	handler, params, ok := own.Search(req.Method, path)
	if !ok || handler == nil {
		http.Error(resp, response.GetStatusLowerText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// 分配上下文，采集数据
	monitor := handler.GetCollector()
	monitor.Increase()
	ctx := newContext(own.engine, resp, req, params)

	// 禁用处理器
	if !handler.IsEnabled() {
		ctx.ResponseError(http.StatusLocked, response.GetStatusLowerText(http.StatusLocked))
		monitor.Decrease(http.StatusLocked, time.Since(ctx.GetStartTime()))
		return
	}

	// 启用处理器
	handler.ServeHTTP(ctx)
	monitor.Decrease(ctx.GetStatusCode(), time.Since(ctx.GetStartTime()))
}

// extractSegmentNames 从路径中提取所有的参数名称（:param 或 *param 或 *）
func extractSegmentNames(segments []string) []string {
	params := make([]string, 0)
	for _, seg := range segments {
		switch {
		case seg == "*":
			params = append(params, tree.WILDCARD_DEFAULT_NAME)
		case strings.HasPrefix(seg, ":"):
			params = append(params, seg[1:])
		case strings.HasPrefix(seg, "*"):
			params = append(params, seg[1:])
		}
	}

	return params
}
