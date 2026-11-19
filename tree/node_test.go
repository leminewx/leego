package tree

import (
	"testing"
)

// TestNewNodePriority 验证不同段类型的优先级与参数名。
func TestNewNodePriority(t *testing.T) {
	cases := []struct {
		name        string
		segment     string
		wantParam   string
		wantPrior   int
		wantStatic  bool
		wantWildcar bool
	}{
		{name: "静态段", segment: "users", wantParam: "", wantPrior: 3, wantStatic: true, wantWildcar: false},
		{name: "参数段", segment: ":id", wantParam: "id", wantPrior: 2, wantStatic: false, wantWildcar: true},
		{name: "无名通配段", segment: "*", wantParam: WILDCARD_DEFAULT_NAME, wantPrior: 1, wantStatic: false, wantWildcar: true},
		{name: "命名通配段", segment: "*filepath", wantParam: "filepath", wantPrior: 1, wantStatic: false, wantWildcar: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			node := NewNode[int](testCase.segment)

			if node.segment != testCase.segment {
				t.Fatalf("segment = %q, want %q", node.segment, testCase.segment)
			}

			if node.paramName != testCase.wantParam {
				t.Fatalf("paramName = %q, want %q", node.paramName, testCase.wantParam)
			}

			if node.GetPriority() != testCase.wantPrior {
				t.Fatalf("priority = %d, want %d", node.GetPriority(), testCase.wantPrior)
			}

			if node.IsStatic() != testCase.wantStatic {
				t.Fatalf("IsStatic() = %v, want %v", node.IsStatic(), testCase.wantStatic)
			}

			if node.isWildcard != testCase.wantWildcar {
				t.Fatalf("isWildcard = %v, want %v", node.isWildcard, testCase.wantWildcar)
			}

			if node.children == nil {
				t.Fatal("children 必须被初始化，避免插入时 nil 判断")
			}
		})
	}
}

// TestAddChildAndFind 验证子节点挂载与查找。
func TestAddChildAndFind(t *testing.T) {
	root := NewNode[int]("root")
	staticChild := NewNode[int]("users")
	paramChild := NewNode[int](":id")
	wildcardChild := NewNode[int]("*filepath")

	root.addChild(staticChild)
	root.addChild(paramChild)
	root.addChild(wildcardChild)

	if got := root.findChild("users"); got != staticChild {
		t.Fatalf("findChild(users) 未命中静态子节点")
	}

	if got := root.findChild("missing"); got != nil {
		t.Fatalf("findChild(missing) = %v, want nil", got)
	}

	if got := root.findWildcardChild(2); got != paramChild {
		t.Fatalf("findWildcardChild(2) 未命中参数子节点")
	}

	if got := root.findWildcardChild(1); got != wildcardChild {
		t.Fatalf("findWildcardChild(1) 未命中通配子节点")
	}

	if len(root.children) != 3 {
		t.Fatalf("children 数量 = %d, want 3", len(root.children))
	}
}

// TestAddChildAfterFreezePanics 验证冻结后禁止插入。
func TestAddChildAfterFreezePanics(t *testing.T) {
	root := NewNode[int]("root")
	root.freeze()

	defer func() {
		if recover() == nil {
			t.Fatal("冻结后 addChild 必须 panic")
		}
	}()

	root.addChild(NewNode[int]("users"))
}
