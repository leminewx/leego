package validate

import (
	"reflect"
	"strings"
	"testing"
)

// TestPatternRules 覆盖格式类规则的正反用例。
func TestPatternRules(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"email", "dev@example.com", true},
		{"email", "dev@example", false},
		{"url", "https://example.com/path", true},
		{"url", "/relative", false},
		{"uuid", "123e4567-e89b-12d3-a456-426614174000", true},
		{"uuid", "123e4567", false},
		{"ip", "10.0.0.1", true},
		{"ip", "999.0.0.1", false},
		{"ipv4", "10.0.0.1", true},
		{"ipv4", "::1", false},
		{"ipv6", "::1", true},
		{"ipv6", "10.0.0.1", false},
		{"alpha", "leego", true},
		{"alpha", "leego1", false},
		{"alnum", "leego1", true},
		{"alnum", "leego-1", false},
		{"numeric", "2026", true},
		{"numeric", "20.26", false},
		{"lowercase", "leego", true},
		{"lowercase", "Leego", false},
		{"uppercase", "LEEGO", true},
		{"uppercase", "Leego", false},
	}

	for _, testCase := range cases {
		rule, ok := lookupRule(testCase.name)
		if !ok {
			t.Fatalf("规则 %q 未注册", testCase.name)
		}

		if got := rule(reflect.ValueOf(testCase.input), ""); got != testCase.want {
			t.Fatalf("%s(%q) = %v, want %v", testCase.name, testCase.input, got, testCase.want)
		}
	}
}

// TestComparisonRules 覆盖数值、长度与字符串比较规则。
func TestComparisonRules(t *testing.T) {
	cases := []struct {
		rule  string
		value any
		param string
		want  bool
	}{
		{"required", "x", "", true},
		{"required", "", "", false},
		{"min", 5, "3", true},
		{"min", "ab", "3", false},
		{"max", "abc", "3", true},
		{"max", 4, "3", false},
		{"len", "中文", "2", true},
		{"len", []int{1, 2}, "2", true},
		{"eq", 3, "3", true},
		{"eq", "x", "x", true},
		{"ne", "x", "y", true},
		{"gt", 4, "3", true},
		{"gte", 3, "3", true},
		{"lt", 2, "3", true},
		{"lte", 3, "3", true},
		{"gt", 3, "3", false},
		{"gt", 3, "not-a-number", false},
	}

	for _, testCase := range cases {
		rule, ok := lookupRule(testCase.rule)
		if !ok {
			t.Fatalf("规则 %q 未注册", testCase.rule)
		}

		if got := rule(reflect.ValueOf(testCase.value), testCase.param); got != testCase.want {
			t.Fatalf("%s(%v, %q) = %v, want %v", testCase.rule, testCase.value, testCase.param, got, testCase.want)
		}
	}
}

// TestStringRules 覆盖包含、前后缀与枚举规则。
func TestStringRules(t *testing.T) {
	cases := []struct {
		rule  string
		value string
		param string
		want  bool
	}{
		{"oneof", "v2", "v1 v2 v3", true},
		{"oneof", "v4", "v1 v2 v3", false},
		{"contains", "leego-framework", "framework", true},
		{"contains", "leego", "framework", false},
		{"prefix", "leego", "lee", true},
		{"prefix", "leego", "go", false},
		{"suffix", "leego", "go", true},
		{"suffix", "leego", "lee", false},
	}

	for _, testCase := range cases {
		rule, ok := lookupRule(testCase.rule)
		if !ok {
			t.Fatalf("规则 %q 未注册", testCase.rule)
		}

		if got := rule(reflect.ValueOf(testCase.value), testCase.param); got != testCase.want {
			t.Fatalf("%s(%q, %q) = %v, want %v", testCase.rule, testCase.value, testCase.param, got, testCase.want)
		}
	}
}

// TestRegisterRuleValidation 验证非法注册被拒绝且不覆盖已有规则。
func TestRegisterRuleValidation(t *testing.T) {
	if err := RegisterRule("  ", func(reflect.Value, string) bool { return true }); err == nil {
		t.Fatal("空规则名必须返回错误")
	}

	if err := RegisterRule(_RULE_DIVE, func(reflect.Value, string) bool { return true }); err == nil {
		t.Fatal("保留字 dive 必须返回错误")
	}

	if err := RegisterRule("nil-rule", nil); err == nil {
		t.Fatal("nil 规则必须返回错误")
	}

	if err := RegisterRule("required", func(reflect.Value, string) bool { return true }); err == nil ||
		!strings.Contains(err.Error(), "already registered") {
		t.Fatalf("重复规则 error = %v, want already registered", err)
	}
}

// TestUnknownRuleLookup 验证未注册规则返回 false。
func TestUnknownRuleLookup(t *testing.T) {
	if _, ok := lookupRule("no-such-rule"); ok {
		t.Fatal("未注册规则不应存在")
	}
}
