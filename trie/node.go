package trie

import (
	"strings"
)

const WILDCARD_DEFAULT_NAME = "path"

// Node 定义路由树节点的数据结构
type Node[T any] struct {
	segment    string     // 当前路径段
	paramName  string     // 参数名（如果是通配符）
	priority   int        // 优先级（用于路由匹配顺序）
	isWildcard bool       // 是否是通配符段 (:param 或 *param 或 *)
	route      T          // 路由信息
	children   []*Node[T] // 子节点
}

// NewNode 新建一个路由树节点
func NewNode[T any](segment string) *Node[T] {
	node := &Node[T]{
		segment:    segment,
		isWildcard: strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*"),
		children:   make([]*Node[T], 0),
	}

	node.extractName(segment)
	return node
}

// extractName 尝试提取当前参数名称
func (own *Node[T]) extractName(segment string) {
	if segment == "*" {
		own.paramName = WILDCARD_DEFAULT_NAME
		return
	}

	if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
		own.paramName = segment[1:]
		return
	}

	own.paramName = ""
}

// GetPriority 获取节点的优先级
func (own *Node[T]) GetPriority() int {
	if own.priority != 0 {
		return own.priority
	}

	switch {
	// *param 或 *
	case strings.HasPrefix(own.segment, "*"):
		own.priority = 1

	// :param
	case own.isWildcard:
		own.priority = 2

	// 静态路径优先级最高
	default:
		own.priority = 3
	}

	return own.priority
}
