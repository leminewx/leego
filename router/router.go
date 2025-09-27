package router

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type (
	// Router 定义路由器的数据结构
	Router[T any] struct {
		roots map[string]*Node[T] // 不同方法的路由树根节点
		mutex sync.RWMutex
	}
)

// NewRouter 创建一个新的路由器
func NewRouter[T any]() *Router[T] {
	return &Router[T]{roots: make(map[string]*Node[T])}
}

// Insert 插入新增的路由
func (own *Router[T]) Insert(method, pattern string, handler http.HandlerFunc) {
	own.mutex.Lock()
	defer own.mutex.Unlock()

	// 构造路由节点
	segments := splitPattern(pattern)
	route := &Route{
		Pattern: pattern,
		Handler: handler,
		Names:   extractSegmentNames(segments),
	}

	// 寻找方法根节点
	root, ok := own.roots[method]
	if !ok {
		root = &Node{}
		own.roots[method] = root
	}

	// 插入路由
	own.insert(root, segments, route, 0)
}

// insert 递归插入路由到前缀树
func (own *Router) insert(node *Node, segments []string, route *Route, depth int) {
	// 如果到达路径末端，设置路由处理器
	if depth == len(segments) {
		node.route = route
		return
	}

	// 获取当前路径片段
	segment := segments[depth]

	// 查找匹配的子节点
	var child *Node
	for _, c := range node.children {
		if c.segment == segment {
			child = c
			break
		}
	}

	// 如果没有找到匹配的子节点，创建新节点
	isWild := segment == "*" || strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*")
	if child == nil {
		child = &Node{
			segment:  segment,
			name:     extractSegmentName(segment),
			isWild:   isWild,
			children: make([]*Node, 0),
		}
		node.priority = getPriority(node)
		node.children = append(node.children, child)

		// 按优先级排序：静态路径 > 参数路径 > 通配符
		sort.Slice(node.children, func(i, j int) bool {
			return node.children[i].priority > node.children[i].priority
		})
	}

	// 递归插入
	own.insert(child, segments, route, depth+1)
}

// Search 搜索匹配的路由
func (own *Router) Search(method, path string) (*Route, map[string]string, bool) {
	own.mutex.RLock()
	defer own.mutex.RUnlock()

	// 找到路由根节点
	root, ok := own.roots[method]
	if !ok {
		return nil, nil, false
	}

	// 搜索路由
	segments := splitPath(path)
	params := make(map[string]string)
	node, ok := own.search(root, segments, params, 0)

	return node.route, params, ok
}

// search 递归搜索匹配的路由
func (own *Router) search(node *Node, segments []string, params map[string]string, depth int) (*Node, bool) {
	// 当前深度与路径深度匹配，则返回当前节点
	if depth == len(segments) {
		return node, true
	}

	// 优先尝试精确匹配
	segment := segments[depth]
	for _, child := range node.children {
		if !child.isWild && child.segment == segment {
			if n, ok := own.search(child, segments, params, depth+1); ok {
				return n, true
			}
		}
	}

	// 尝试参数匹配 :param 或 *param
	for _, child := range node.children {
		if child.isWild && child.segment != "*" {
			params[child.name] = segment
			if n, ok := own.search(child, segments, params, depth+1); ok {
				return n, true
			}

			// 回溯时删除参数
			delete(params, child.name)
		}
	}

	// 尝试通配符匹配 *
	for _, child := range node.children {
		if child.segment == "*" {
			// 通配符匹配，收集剩余路径
			if depth < len(segments) {
				remaining := strings.Join(segments[depth:], "/")
				params[child.name] = remaining
			}

			return child, true
		}
	}

	return nil, false
}

// ServeHTTP 实现http.Handler接口
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route, params := r.FindRoute(req.Method, req.URL.Path)

	if route == nil {
		http.NotFound(w, req)
		return
	}

	// 将参数存入请求上下文
	if len(params) > 0 {
		ctx := req.Context()
		for key, value := range params {
			ctx = context.WithValue(ctx, key, value)
		}
		req = req.WithContext(ctx)
	}

	route.Handler(w, req)
}

// splitPattern 分割路由模式
func splitPattern(pattern string) []string {
	segments := strings.Split(strings.Trim(pattern, "/"), "/")
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg != "" {
			result = append(result, seg)
		}
	}

	return result
}

// extractSegmentName 提取路由段中的参数名称
func extractSegmentName(segment string) string {
	if segment == "*" {
		return "path"
	}

	if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
		return segment[1:]
	}

	return ""
}

// extractSegmentNames 从路径中提取所有的参数名称
func extractSegmentNames(segments []string) []string {
	params := make([]string, 0)
	for _, seg := range segments {
		switch {
		case seg == "*":
			params = append(params, "path")
		case strings.HasPrefix(seg, ":"):
			params = append(params, seg[1:])
		case strings.HasPrefix(seg, "*"):
			params = append(params, seg[1:])
		}
	}

	return params
}
