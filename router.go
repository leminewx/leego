package leego

import (
	"fmt"
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

	roots map[string]*trie.Trie[*handler] // 不同方法对应不同的路由树
	mutex sync.Mutex
}

// newRouter 创建一个新的路由器
func newRouter(engine *Engine) *router {
	return &router{
		engine: engine,
		roots:  make(map[string]*trie.Trie[*handler]),
	}
}

// Insert 插入新增的路由
func (own *router) Insert(method, pattern string, _handler *handler) {
	own.mutex.Lock()
	defer own.mutex.Unlock()

	// 若方法对应的路由树不存在，则创建
	root, ok := own.roots[method]
	if !ok {
		root = trie.NewTrie[*handler]()
		own.roots[method] = root
	}

	// 插入路由
	root.Insert(method, pattern, _handler)
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
	// 匹配路由
	handler, params, ok := own.Search(req.Method, req.URL.Path)
	if !ok {
		fmt.Printf(_FORMAT_PRINT_404, time.Now().Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, http.StatusNotFound, "0s")
		http.Error(resp, response.STATUS_NOT_FOUND, http.StatusNotFound)
		return
	}

	// 创建上下文
	res := response.NewWriter(resp)
	ctx := newContext(own.engine, res, req, params)

	// 处理路由，并收集数据
	start := time.Now()
	handler.collector.Increase()
	handler.Handle(ctx)
	handler.collector.Decrease(ctx.GetStatusCode(), time.Since(start))
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
