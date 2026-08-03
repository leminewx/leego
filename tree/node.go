package tree

import (
	"strings"
)

const WILDCARD_DEFAULT_NAME = "filepath"

// Node 定义路由树节点的数据结构
type Node[T any] struct {
	segment    string     // 节点路径（包含公共前缀）
	paramName  string     // 参数名（如果是通配符）
	priority   int        // 优先级（1-通配符*  2-参数:  3-静态）
	isWildcard bool       // 是否是通配符段 (:param 或 *param 或 *)
	isRoute    bool       // 是否是一个路由节点
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
	node.priority = node.computePriority()
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

// computePriority 计算节点优先级（1-通配符*  2-参数:  3-静态），在构造时一次性确定，
// 避免 GetPriority 在并发路由查找中惰性写 priority 引发数据竞争
func (own *Node[T]) computePriority() int {
	switch {
	// *param 或 *
	case strings.HasPrefix(own.segment, "*"):
		return 1

	// :param
	case own.isWildcard:
		return 2

	// 静态路径优先级最高
	default:
		return 3
	}
}

// GetPriority 获取节点的优先级（值已构造时确定，此处只读，可并发安全访问）
func (own *Node[T]) GetPriority() int {
	return own.priority
}
