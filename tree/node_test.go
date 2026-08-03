package tree

import (
	"testing"
)

// TestNewNode 测试节点创建
func TestNewNode(t *testing.T) {
	tests := []struct {
		name     string
		segment  string
		expected struct {
			segment    string
			isWildcard bool
			paramName  string
		}
	}{
		{
			name:    "静态路径节点",
			segment: "users",
			expected: struct {
				segment    string
				isWildcard bool
				paramName  string
			}{
				segment:    "users",
				isWildcard: false,
				paramName:  "",
			},
		},
		{
			name:    "参数节点",
			segment: ":id",
			expected: struct {
				segment    string
				isWildcard bool
				paramName  string
			}{
				segment:    ":id",
				isWildcard: true,
				paramName:  "id",
			},
		},
		{
			name:    "通配符节点",
			segment: "*",
			expected: struct {
				segment    string
				isWildcard bool
				paramName  string
			}{
				segment:    "*",
				isWildcard: true,
				paramName:  WILDCARD_DEFAULT_NAME,
			},
		},
		{
			name:    "命名通配符节点",
			segment: "*path",
			expected: struct {
				segment    string
				isWildcard bool
				paramName  string
			}{
				segment:    "*path",
				isWildcard: true,
				paramName:  "path",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := NewNode[string](tt.segment)

			if node.segment != tt.expected.segment {
				t.Errorf("segment = %v, want %v", node.segment, tt.expected.segment)
			}

			if node.isWildcard != tt.expected.isWildcard {
				t.Errorf("isWildcard = %v, want %v", node.isWildcard, tt.expected.isWildcard)
			}

			if node.paramName != tt.expected.paramName {
				t.Errorf("paramName = %v, want %v", node.paramName, tt.expected.paramName)
			}
		})
	}
}

// TestGetPriority 测试优先级计算
func TestGetPriority(t *testing.T) {
	tests := []struct {
		name     string
		segment  string
		expected int
	}{
		{"静态路径", "users", 3},
		{"参数路径", ":id", 2},
		{"通配符", "*", 1},
		{"命名通配符", "*path", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := NewNode[string](tt.segment)
			priority := node.GetPriority()

			if priority != tt.expected {
				t.Errorf("GetPriority() = %v, want %v", priority, tt.expected)
			}

			// 测试缓存功能
			cachedPriority := node.GetPriority()
			if cachedPriority != priority {
				t.Errorf("Cached priority = %v, want %v", cachedPriority, priority)
			}
		})
	}
}

// TestNodeChildren 测试子节点管理
func TestNodeChildren(t *testing.T) {
	root := NewNode[string]("/")

	// 添加子节点
	child1 := NewNode[string]("users")
	child2 := NewNode[string](":id")
	child3 := NewNode[string]("*")

	root.children = append(root.children, child1, child2, child3)

	if len(root.children) != 3 {
		t.Errorf("Expected 3 children, got %d", len(root.children))
	}

	// 验证子节点属性
	if root.children[0].segment != "users" {
		t.Errorf("First child segment = %v, want 'users'", root.children[0].segment)
	}
}

// TestNodeRouteStorage 测试路由存储
func TestNodeRouteStorage(t *testing.T) {
	type RouteInfo struct {
		Method  string
		Handler string
	}

	node := NewNode[RouteInfo]("users")
	testRoute := RouteInfo{
		Method:  "GET",
		Handler: "getUsersHandler",
	}

	node.route = testRoute

	if node.route.Method != "GET" {
		t.Errorf("Route method = %v, want 'GET'", node.route.Method)
	}

	if node.route.Handler != "getUsersHandler" {
		t.Errorf("Route handler = %v, want 'getUsersHandler'", node.route.Handler)
	}
}

// TestComplexScenarios 测试复杂场景
func TestComplexScenarios(t *testing.T) {
	t.Run("参数名称提取边界情况", func(t *testing.T) {
		testCases := []struct {
			segment    string
			expectName string
		}{
			{":", ""},                    // 只有冒号
			{"*", WILDCARD_DEFAULT_NAME}, // 只有星号
			{":id", "id"},                // 正常参数
			{"*path", "path"},            // 命名通配符
			{"user", ""},                 // 静态路径
			{"", ""},                     // 空字符串
		}

		for _, tc := range testCases {
			node := NewNode[string](tc.segment)
			if node.paramName != tc.expectName {
				t.Errorf("segment %s: paramName = %s, want %s",
					tc.segment, node.paramName, tc.expectName)
			}
		}
	})

	t.Run("通配符标识测试", func(t *testing.T) {
		wildcardCases := []struct {
			segment    string
			isWildcard bool
		}{
			{":id", true},
			{"*", true},
			{"*path", true},
			{"users", false},
			{"api", false},
		}

		for _, tc := range wildcardCases {
			node := NewNode[string](tc.segment)
			if node.isWildcard != tc.isWildcard {
				t.Errorf("segment %s: isWildcard = %v, want %v",
					tc.segment, node.isWildcard, tc.isWildcard)
			}
		}
	})
}

// BenchmarkNodeCreation 测试节点创建性能
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		10991818	       101.7 ns/op	     480 B/op	       5 allocs/op
func BenchmarkNodeCreation(b *testing.B) {
	segments := []string{"users", ":id", "posts", ":postId", "*path"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, seg := range segments {
			_ = NewNode[string](seg)
		}
	}
}
