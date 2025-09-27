package router

import (
	"net/http"
	"strings"
)

// Node 定义路由节点的数据结构
type Node[T any] struct {
	Pattern    string   // 路由模式
	Handler    T        // 当前节点所对应的路由
	ParamNames []string // 路由模式中所有的参数名列表

	segment    string     // 当前路径段
	paramName  string     // 参数名（如果是通配符）
	priority   int        // 优先级（用于路由匹配顺序）
	isWildcard bool       // 是否是通配符段 (:param 或 *param 或 *)
	children   []*Node[T] // 子节点
}

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
		own.paramName = "path"
	}

	if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
		own.paramName = segment[1:]
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

// GetParam 从请求中获取参数值
func GetParam(req *http.Request, key string) string {
	if value := req.Context().Value(key); value != nil {
		return value.(string)
	}
	return ""
}
