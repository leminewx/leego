package leego

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"gitee.com/leminewx/leego/response"
	"gitee.com/leminewx/leego/trie"
)

// router 定义路由器的数据结构
type router struct {
	engine *Engine

	roots    map[string]*trie.Trie[*handler] // 不同方法对应不同的路由树
	backlist sync.Map                        // 路由黑名单

	insertLock sync.Mutex
}

// newRouter 创建一个新的路由器
func newRouter(engine *Engine) *router {
	return &router{
		engine:   engine,
		roots:    make(map[string]*trie.Trie[*handler]),
		backlist: sync.Map{},
	}
}

// WithURLBlacklist 添加 URL 黑名单
func (own *router) WithURLBlacklist(urls ...string) *router {
	for _, url := range urls {
		own.backlist.Store(url, true)
	}

	return own
}

// Insert 插入新增的路由
func (own *router) Insert(method, pattern string, _handler *handler) {
	own.insertLock.Lock()
	defer own.insertLock.Unlock()

	// 若方法对应的路由树不存在，则创建
	root, ok := own.roots[method]
	if !ok {
		root = trie.NewTrie[*handler]()
		own.roots[method] = root
	}

	// 插入路由
	root.Insert(pattern, _handler)
}

// Search 搜索匹配的路由
func (own *router) Search(method, path string) (*handler, map[string]string, bool) {
	// 找到路由根节点
	root, ok := own.roots[method]
	if !ok {
		return nil, nil, false
	}

	// 搜索路由
	return root.Search(path)
}

// ServeHTTP 实现 http.Handler 接口
func (own *router) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	// 黑名单检查
	if _, ok := own.backlist.Load(path); ok {
		http.Error(resp, response.STATUS_403_FORBIDDEN, http.StatusForbidden)
		return
	}

	// 匹配路由
	handler, params, ok := own.Search(req.Method, path)
	if !ok {
		http.Error(resp, response.STATUS_404_NOT_FOUND, http.StatusNotFound)
		return
	}

	// 创建上下文，采集数据
	handler.monitor.Increase()
	ctx := newContext(own.engine, resp, req, params)

	// 检查处理器是否禁用
	if !handler.IsEnable() {
		ctx.ResponseError(http.StatusLocked, response.STATUS_423_LOCKED)
		handler.monitor.Decrease(http.StatusLocked, time.Since(ctx.GetStartTime()))
		return
	}

	// 处理请求，采集数据
	handler.Handle(ctx)
	handler.monitor.Decrease(ctx.GetStatusCode(), time.Since(ctx.GetStartTime()))
}

// extractSegmentNames 从路径中提取所有的参数名称（:param 或 *param 或 *）
func extractSegmentNames(segments []string) []string {
	params := make([]string, 0)
	for _, seg := range segments {
		switch {
		case seg == "*":
			params = append(params, trie.WILDCARD_DEFAULT_NAME)
		case strings.HasPrefix(seg, ":"):
			params = append(params, seg[1:])
		case strings.HasPrefix(seg, "*"):
			params = append(params, seg[1:])
		}
	}

	return params
}
