package tree

import "strings"

// Trie 是分段前缀树的具体实现。
//
// 通过“定义类型”而非结构体内嵌复用 Node 的内存布局，使 Trie 可以直接复用
// Node 的字段与方法，同时对外暴露独立的类型名称。泛型参数 T 表示挂在叶子
// 节点上的业务数据，框架中为 Handler。
type Trie[T any] Node[T]

// 编译期断言：Trie 必须实现 Tree 接口。
var _ Tree[int] = &Trie[int]{}

// NewTrie 创建一棵空的路由树。
func NewTrie[T any]() *Trie[T] {
	return &Trie[T]{}
}

// getRootNode 返回 Trie 对应的根节点视图。
//
// Trie 与 Node 具有完全相同的内存布局，因此该转换是零成本的。
func (own *Trie[T]) getRootNode() *Node[T] {
	return (*Node[T])(own)
}

// Insert 插入一条路由。
//
// 参数 pattern 为路由模式，支持静态段、":param" 参数段与 "*param" 通配段。
// 插入阶段会完成以下校验：
//
//   - 树已冻结时 panic，避免运行期结构变更；
//   - 同一位置出现不同参数名的参数段时 panic；
//   - 重复注册同一模式时 panic。
//
// 这些均属于编程错误，采用与标准库 http.ServeMux 一致的 fail-fast 策略。
func (own *Trie[T]) Insert(pattern string, route T) {
	segments := SplitPattern(pattern)
	own.insert(own.getRootNode(), segments, route, 0)
}

// insert 将路由递归写入指定节点。
func (own *Trie[T]) insert(node *Node[T], segments []string, route T, depth int) {
	if node.frozen {
		panic("tree: cannot insert a route after the tree has been frozen")
	}

	// 已到达模式末尾：写入业务数据。
	if depth == len(segments) {
		if node.isRoute {
			panic("tree: route already registered: " + strings.Join(segments, "/"))
		}

		node.isRoute = true
		node.route = route
		return
	}

	segment := segments[depth]

	// 静态段优先复用已存在的子节点。
	if !IsWildcard(segment) {
		child := node.findChild(segment)
		if child == nil {
			child = NewNode[T](segment)
			node.addChild(child)
		}

		own.insert(child, segments, route, depth+1)
		return
	}

	// 通配段按优先级复用同类型子节点，并校验参数名一致性。
	priority := 1
	if strings.HasPrefix(segment, ":") {
		priority = 2
	}

	child := node.findWildcardChild(priority)
	if child == nil {
		child = NewNode[T](segment)
		node.addChild(child)
	} else if child.segment != segment {
		panic("tree: conflicting route parameter: existing " + child.segment + ", new " + segment)
	}

	own.insert(child, segments, route, depth+1)
}

// Search 查找与给定路径匹配的路由。
//
// 返回值约定：
//   - 第三个返回值表示是否命中；
//   - 第二个返回值为路由参数表；**静态路由命中时返回 nil**，避免热路径分配；
//   - 未命中时第一个返回值为 T 的零值。
//
// 查找本身不加锁。框架在启动阶段调用 Freeze 冻结路由树，此后并发查找是安全的；
// 若需要“边注册边查找”，请由调用方自行加读写锁（Router 已内置）。
func (own *Trie[T]) Search(path string) (T, map[string]string, bool) {
	if node, params, ok := own.searchNode(own.getRootNode(), path, nil); ok {
		return node.route, params, true
	}

	return *new(T), nil, false
}

// searchNode 在给定节点上递归匹配剩余路径。
//
// 匹配顺序固定为：静态段 → 参数段 → 通配段，与优先级 3 > 2 > 1 一致。
// 任意分支失败都会继续尝试后续分支（完整回溯）；参数分支失败时会删除已写入
// 的参数，避免污染兄弟分支。
func (own *Trie[T]) searchNode(node *Node[T], rest string, params map[string]string) (*Node[T], map[string]string, bool) {
	segment, remain, hasNext := splitNextSegment(rest)

	// 路径已消费完：仅当当前节点真实注册了路由才算命中。
	if !hasNext {
		if node.isRoute {
			return node, params, true
		}

		return nil, nil, false
	}

	// 1. 静态段：优先走 O(1) 索引。
	if node.staticIndex != nil {
		if child, ok := node.staticIndex[segment]; ok {
			if matched, result, found := own.searchNode(child, remain, params); found {
				return matched, result, true
			}
		}
	} else {
		for _, child := range node.children {
			if child.priority == 3 && child.segment == segment {
				if matched, result, found := own.searchNode(child, remain, params); found {
					return matched, result, true
				}
			}
		}
	}

	// 2. 参数段：每条路径最多分配一次参数表，静态路由命中时为零分配。
	for _, child := range node.children {
		if child.priority != 2 {
			continue
		}

		table := params
		if table == nil {
			table = make(map[string]string, 2)
		}

		table[child.paramName] = segment
		if matched, result, found := own.searchNode(child, remain, table); found {
			return matched, result, true
		}

		delete(table, child.paramName)
	}

	// 3. 通配段：吞掉剩余全部路径，且只有真实注册过路由才允许命中。
	for _, child := range node.children {
		if child.priority != 1 || !child.isRoute {
			continue
		}

		table := params
		if table == nil {
			table = make(map[string]string, 1)
		}

		table[child.paramName] = strings.TrimPrefix(rest, "/")
		return child, table, true
	}

	return nil, nil, false
}

// Freeze 冻结路由树。
//
// 冻结后所有节点拒绝插入，查找路径只读，因而可以无锁并发执行。
// 重复调用 Freeze 是安全的。
func (own *Trie[T]) Freeze() error {
	own.getRootNode().freeze()
	return nil
}

// splitNextSegment 从剩余路径中取出第一个非空段。
//
// 返回值：
//   - segment：当前段文本；
//   - remain：剩余路径（保留前导 "/"）；
//   - ok：是否还存在有效的下一段。
//
// 该函数只做字符串切片，不产生堆分配，是热路径零分配的关键。
func splitNextSegment(rest string) (segment, remain string, ok bool) {
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return "", "", false
	}

	if index := strings.IndexByte(rest, '/'); index >= 0 {
		return rest[:index], rest[index:], true
	}

	return rest, "", true
}
