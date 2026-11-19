package tree

import (
	"reflect"
	"testing"
)

// TestSplitPattern 验证路由模式的切分行为。
func TestSplitPattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		want    []string
	}{
		{name: "空模式", pattern: "", want: []string{}},
		{name: "根路径", pattern: "/", want: []string{}},
		{name: "单段", pattern: "/users", want: []string{"users"}},
		{name: "多段", pattern: "/api/v1/users", want: []string{"api", "v1", "users"}},
		{name: "无前导斜杠", pattern: "api/v1/users", want: []string{"api", "v1", "users"}},
		{name: "末尾斜杠", pattern: "/api/v1/", want: []string{"api", "v1"}},
		{name: "重复斜杠", pattern: "//api///v1//users//", want: []string{"api", "v1", "users"}},
		{name: "参数段", pattern: "/users/:id/posts/:postId", want: []string{"users", ":id", "posts", ":postId"}},
		{name: "通配段", pattern: "/static/*filepath", want: []string{"static", "*filepath"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := SplitPattern(testCase.pattern)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("SplitPattern(%q) = %v, want %v", testCase.pattern, got, testCase.want)
			}
		})
	}
}

// TestIsWildcard 验证通配段的判定规则。
func TestIsWildcard(t *testing.T) {
	cases := []struct {
		segment string
		want    bool
	}{
		{segment: "", want: false},
		{segment: "users", want: false},
		{segment: ":id", want: true},
		{segment: "*", want: true},
		{segment: "*filepath", want: true},
		{segment: "user:id", want: false},
	}

	for _, testCase := range cases {
		if got := IsWildcard(testCase.segment); got != testCase.want {
			t.Fatalf("IsWildcard(%q) = %v, want %v", testCase.segment, got, testCase.want)
		}
	}
}
