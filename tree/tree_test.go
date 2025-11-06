package tree

import (
	"testing"
)

// TestSplitPattern 测试路径分割函数
func TestSplitPattern(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"/", []string{}},
		{"/users", []string{"users"}},
		{"/users/:id", []string{"users", ":id"}},
		{"/users/:id/posts", []string{"users", ":id", "posts"}},
		{"users/:id/", []string{"users", ":id"}},
		{"//users//:id//", []string{"users", ":id"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SplitPattern(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("SplitPattern(%s) = %v, want %v", tt.input, result, tt.expected)
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("SplitPattern(%s)[%d] = %v, want %v", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}
