package tree

import "strings"

// WILDCARD_DEFAULT_NAME 定义通配符路由（模式中的 "*"）默认使用的参数名称。
//
// 当路由模式写作 "/static/*" 时，匹配到的剩余路径会以该名称写入参数表，
// 业务代码可通过 ctx.GetPathValue(tree.WILDCARD_DEFAULT_NAME) 读取。
const WILDCARD_DEFAULT_NAME = "filepath"

// Node 定义路由树节点的数据结构。
//
// 每一个节点对应路由模式中的一个“段”（以 "/" 分隔）。节点在构造时一次性
// 确定自己的优先级，避免在并发查找过程中惰性写入优先级而引发数据竞争。
//
// 泛型参数 T 表示路由节点上挂载的业务数据类型，框架中通常为 Handler 接口。
type Node[T any] struct {
	// segment 保存当前节点对应的路径段文本。
	segment string
	// paramName 保存参数名；仅当节点为参数段（":id"）或通配段（"*path"）时非空。
	paramName string
	// priority 保存节点匹配优先级：静态段 3 > 参数段 2 > 通配段 1。
	priority int
	// isWildcard 标记当前节点是否为参数段或通配段。
	isWildcard bool
	// isRoute 标记当前节点是否真实注册了路由。
	isRoute bool
	// route 保存该节点挂载的业务数据。
	route T
	// children 保存全部子节点；插入阶段可写，冻结后只读。
	children []*Node[T]
	// staticIndex 保存“静态段文本 -> 子节点”的索引，使静态匹配从线性扫描降为 O(1)。
	staticIndex map[string]*Node[T]
	// frozen 标记当前节点是否已冻结；冻结后禁止再插入。
	frozen bool
}

// NewNode 创建一个路由树节点。
//
// 参数 segment 为当前节点对应的路径段。函数会完成参数名提取与优先级计算，
// 并初始化子节点切片，保证后续插入无需再做 nil 判断。
func NewNode[T any](segment string) *Node[T] {
	node := &Node[T]{
		segment:    segment,
		isWildcard: IsWildcard(segment),
		children:   make([]*Node[T], 0, 4),
	}

	node.extractParamName(segment)
	node.priority = node.computePriority()
	return node
}

// extractParamName 从路径段中提取参数名。
//
//   - "*" 使用默认参数名 WILDCARD_DEFAULT_NAME；
//   - ":id" 与 "*path" 去掉首字符后作为参数名；
//   - 静态段不设置参数名。
func (own *Node[T]) extractParamName(segment string) {
	if segment == "*" {
		own.paramName = WILDCARD_DEFAULT_NAME
		return
	}

	if IsWildcard(segment) {
		own.paramName = segment[1:]
		return
	}

	own.paramName = ""
}

// computePriority 计算并返回节点优先级。
//
// 优先级在构造阶段一次性确定，查找阶段只读，因而无需额外同步。
// 取值约定：通配段 1、参数段 2、静态段 3，数值越大越优先匹配。
func (own *Node[T]) computePriority() int {
	switch {
	case strings.HasPrefix(own.segment, "*"):
		return 1
	case own.isWildcard:
		return 2
	default:
		return 3
	}
}

// GetPriority 返回节点的匹配优先级。
//
// 该值在构造阶段写入、运行阶段只读，可安全并发调用。
func (own *Node[T]) GetPriority() int {
	return own.priority
}

// IsStatic 判断当前节点是否为静态段。
//
// 静态段可直接通过父节点的 staticIndex 命中，是热路径上最快的分支。
func (own *Node[T]) IsStatic() bool {
	return own.priority == 3
}

// addChild 将子节点挂载到当前节点，并同步维护静态索引。
//
// 该函数只在注册阶段调用；若当前节点已冻结会触发 panic，
// 以避免运行期修改树结构造成数据竞争。
func (own *Node[T]) addChild(child *Node[T]) {
	if own.frozen {
		panic("tree: cannot insert a route after the tree has been frozen")
	}

	own.children = append(own.children, child)
	if child.IsStatic() {
		if own.staticIndex == nil {
			own.staticIndex = make(map[string]*Node[T], 8)
		}
		own.staticIndex[child.segment] = child
	}
}

// findChild 查找与指定段完全相同的子节点。
//
// 注册阶段使用该函数判断是否需要创建新节点；查找阶段使用静态索引，
// 因此该函数不是热路径的一部分。
func (own *Node[T]) findChild(segment string) *Node[T] {
	if child, ok := own.staticIndex[segment]; ok {
		return child
	}

	for _, child := range own.children {
		if child.segment == segment {
			return child
		}
	}

	return nil
}

// findWildcardChild 返回指定优先级的通配子节点。
//
// 参数 priority 仅接受 1（通配段）或 2（参数段）。同一层级最多允许一个
// 同名参数节点；重复注册不同参数名会在插入阶段被拒绝。
func (own *Node[T]) findWildcardChild(priority int) *Node[T] {
	for _, child := range own.children {
		if child.priority == priority {
			return child
		}
	}

	return nil
}

// freeze 冻结当前节点及其子树。
//
// 冻结后 children 不再变化，因此查找路径可以无锁读取。查找阶段按优先级
// 分三轮遍历（静态 → 参数 → 通配），不依赖 children 的物理顺序，因此这里
// 无需排序，也避免了每次插入都执行 sort.Slice 的开销。
func (own *Node[T]) freeze() {
	own.frozen = true
	for _, child := range own.children {
		child.freeze()
	}
}
