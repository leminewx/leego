package tree

import (
	"sort"
	"strings"
)

// Trie 定义路由树的数据类型
type Trie[T any] Node[T]

var _ Tree[int] = &Trie[int]{}

// NewTrie 新建一个路由树
func NewTrie[T any]() *Trie[T] {
	return &Trie[T]{}
}

// Insert 插入路由
func (own *Trie[T]) Insert(pattern string, route T) {
	segments := SplitPattern(pattern)
	own.insert((*Node[T])(own), segments, route, 0)
}

// insert 递归插入路由到前缀树
func (own *Trie[T]) insert(node *Node[T], segments []string, route T, depth int) {
	// 如果到达路径末端，设置路由处理器
	if depth == len(segments) {
		node.isRoute = true
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
		return node, node.isRoute
	}

	segment := segments[depth]
	children := node.children

	// children 已按优先级降序排列：静态(3) > 参数(2) > 通配符(1)
	// 任意一个分支匹配失败都必须继续尝试后续兄弟节点，实现完整回溯
	for i := 0; i < len(children); i++ {
		child := children[i]

		switch child.GetPriority() {
		// 静态路径：段必须完全相等
		case 3:
			if child.segment != segment {
				continue
			}

			if n, ok := own.search(child, segments, params, depth+1); ok {
				return n, true
			}

		// 参数路径 :param：匹配任意单个段
		case 2:
			params[child.paramName] = segment
			if n, ok := own.search(child, segments, params, depth+1); ok {
				return n, true
			}

			// 回溯时删除参数，避免污染后续分支
			delete(params, child.paramName)

		// 通配符 *param 或 *：吞掉剩余全部段
		case 1:
			// 只有该节点确实注册了路由才算匹配成功，否则继续回溯
			if !child.isRoute {
				continue
			}

			params[child.paramName] = strings.Join(segments[depth:], "/")
			return child, true
		}
	}

	return nil, false
}
