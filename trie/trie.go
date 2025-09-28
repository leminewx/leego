package trie

import (
	"sort"
	"strings"
)

// Trie 定义路由树的数据类型
type Trie[T any] Node[T]

// NewTrie 新建一个路由树
func NewTrie[T any]() *Trie[T] { return &Trie[T]{} }

// Insert 插入路由
func (own *Trie[T]) Insert(method, pattern string, route T) {
	segments := SplitPattern(pattern)
	own.insert((*Node[T])(own), segments, route, 0)
}

// insert 递归插入路由到前缀树
func (own *Trie[T]) insert(node *Node[T], segments []string, route T, depth int) {
	// 如果到达路径末端，设置路由处理器
	if depth == len(segments) {
		node.route = route
		return
	}

	// 获取当前路径片段
	segment := segments[depth]

	// 查找匹配的子节点
	var child *Node[T]
	for _, c := range node.children {
		if c.segment == segment {
			child = c
			break
		}
	}

	// 如果没有找到匹配的子节点，创建新节点
	if child == nil {
		child = NewNode[T](segment)
		node.children = append(node.children, child)

		// 按优先级排序：静态路径 > 参数路径 > 通配符
		sort.Slice(node.children, func(i, j int) bool {
			return node.children[i].GetPriority() > node.children[j].GetPriority()
		})
	}

	// 递归插入
	own.insert(child, segments, route, depth+1)
}

// Search 搜索匹配的路由
func (own *Trie[T]) Search(path string) (T, map[string]string, bool) {
	segments := SplitPattern(path)
	params := make(map[string]string)
	if node, ok := own.search((*Node[T])(own), segments, params, 0); ok {
		return node.route, params, ok
	}

	return *new(T), nil, false
}

// search 递归搜索匹配的路由
func (own *Trie[T]) search(node *Node[T], segments []string, params map[string]string, depth int) (*Node[T], bool) {
	// 当前深度与路径深度匹配，则返回当前节点
	if depth == len(segments) {
		return node, true
	}

	// 优先尝试精确匹配
	segment := segments[depth]
	for _, child := range node.children {
		if !child.isWildcard && child.segment == segment {
			if n, ok := own.search(child, segments, params, depth+1); ok {
				return n, true
			}
		}
	}

	// 尝试参数匹配 :param 或 *param 或 *
	for _, child := range node.children {
		if child.isWildcard && child.segment != "*" {
			params[child.paramName] = segment
			if n, ok := own.search(child, segments, params, depth+1); ok {
				return n, true
			}

			// 回溯时删除参数
			delete(params, child.paramName)
		}
	}

	// 尝试通配符匹配 *
	for _, child := range node.children {
		if child.segment == "*" {
			// 通配符匹配，收集剩余路径
			if depth < len(segments) {
				remaining := strings.Join(segments[depth:], "/")
				params[child.paramName] = remaining
			}

			return child, true
		}
	}

	return nil, false
}

// SplitPattern 分割路由模式
func SplitPattern(pattern string) []string {
	segments := strings.Split(strings.Trim(pattern, "/"), "/")
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg != "" {
			result = append(result, seg)
		}
	}

	return result
}
