package tree

import "strings"

// Tree 定义路由树必须实现的接口。
//
// 泛型参数 T 表示路由上挂载的业务数据类型。框架内置的 *Trie[T] 是唯一实现，
// 业务方也可以自行实现该接口并通过 Engine 的注入选项替换默认路由树。
type Tree[T any] interface {
	// Insert 注册一条路由；模式语法支持静态段、":param" 与 "*param"。
	Insert(pattern string, route T)
	// Search 查找匹配路径的路由；静态命中时参数表为 nil。
	Search(path string) (route T, params map[string]string, ok bool)
	// Freeze 冻结路由树，冻结后拒绝插入并允许无锁并发查找。
	Freeze() error
}

// SplitPattern 将路由模式切分为非空段列表。
//
// 该函数保留用于测试、文档生成与第三方路由实现；框架内部热路径使用
// 零分配的逐段游标扫描（见 nextSegment），不会调用本函数。
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

// IsWildcard 判断路径段是否为参数段或通配段。
//
// 以 ":" 开头表示单段参数，以 "*" 开头表示吞掉剩余路径的通配段。
func IsWildcard(segment string) bool {
	return strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*")
}
