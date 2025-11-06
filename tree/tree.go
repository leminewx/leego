package tree

import "strings"

// Tree 定义路由树的接口类型
type Tree[T any] interface {
	// Insert 插入路由
	Insert(pattern string, route T)
	// Search 搜索路由
	Search(path string) (route T, params map[string]string, ok bool)
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

// IsWildcard 是否为通配字符串
func IsWildcard(segment string) bool {
	return strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*")
}
