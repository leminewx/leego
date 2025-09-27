package router

import (
	"fmt"
	"sort"
)

type Trie[T any] Node[T]

func New[T any]() *Trie[T] { return &Trie[T]{} }

func (own *Trie[T]) Insert(method, pattern string, handler T) error {
	segments := splitPattern(pattern)
	return own.insert((*Node[T])(own), segments, handler, 0)
}

// insert 递归插入路由到前缀树
func (own *Trie[T]) insert(node *Node[T], segments []string, handler T, depth int) error {
	// 如果到达路径末端，设置路由处理器
	if depth == len(segments) {
		if handler != nil {
			return fmt.Errorf("leego: trie: insert a nil handler")
		}

		node.Handler = handler
		return nil
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
			return node.children[i].GetPriority() > node.children[i].GetPriority()
		})
	}

	// 递归插入
	return own.insert(child, segments, handler, depth+1)
}
