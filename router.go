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

const (
	_FORMAT_404    = "\033[33mWARN \033[0m -  %s  -  \"%s %v %s\"  |\033[43m\033[30m %d \033[0m|\033[43m\033[30m %v \033[0m|\n\n"
	_WILDCARD_NAME = "path"
)

type (
	// route 定义路由的数据结构
	route struct {
		Pattern    string   // 路由模式
		Handler    *handler // 处理函数
		ParamNames []string // 路由模式中所有的参数名列表
	}

	// router 定义路由器的数据结构
	router struct {
		engine *Engine
		roots  map[string]*trie.Trie[*route] // 不同方法对应不同的路由树
		mutex  sync.Mutex
	}
)

// newRouter 创建一个新的路由器
func newRouter(engine *Engine) *router {
	return &router{
		engine: engine,
		roots:  make(map[string]*trie.Trie[*route]),
	}
}

// Insert 插入新增的路由
func (own *router) Insert(method, pattern string, handler *handler) {
	own.mutex.Lock()
	defer own.mutex.Unlock()

	// 若方法对应的路由树不存在，则创建
	root, ok := own.roots[method]
	if !ok {
		root = trie.NewTrie[*route]()
		own.roots[method] = root
	}

	// 创建路由
	route := &route{
		Pattern:    pattern,
		Handler:    handler,
		ParamNames: extractSegmentNames(trie.SplitPattern(pattern)),
	}

	// 插入路由
	root.Insert(method, pattern, route)
}

// Search 搜索匹配的路由
func (own *router) Search(method, path string) (*route, map[string]string, bool) {
	// 找到路由根节点
	root, ok := own.roots[method]
	if !ok {
		return nil, nil, false
	}

	// 搜索路由
	return root.Search(path)
}

// ServeHTTP 实现http.Handler接口
func (own *router) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	// 匹配路由
	route, params, ok := own.Search(req.Method, req.URL.Path)
	if !ok {
		fmt.Printf(_FORMAT_404, time.Now().Format(_FORMAT_DATETIME), req.Method, req.URL.Path, req.Proto, http.StatusNotFound, "0s")
		http.Error(resp, response.STATUS_NOT_FOUND, http.StatusNotFound)
		return
	}

	// 创建上下文
	ctx := newContext(own.engine, resp, req, params)
	route.Handler.handle(ctx)
}

// extractSegmentNames 从路径中提取所有的参数名称（:param 或 *param 或 *）
func extractSegmentNames(segments []string) []string {
	params := make([]string, 0)
	for _, seg := range segments {
		switch {
		case seg == "*":
			params = append(params, _WILDCARD_NAME)
		case strings.HasPrefix(seg, ":"):
			params = append(params, seg[1:])
		case strings.HasPrefix(seg, "*"):
			params = append(params, seg[1:])
		}
	}

	return params
}
