package validate_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gitee.com/leminewx/leego/validate"
)

// Embedded 用于验证匿名结构体展开。
type Embedded struct {
	Email string `validate:"required,email"`
}

// profile 为校验用例的主结构体。
type profile struct {
	Embedded
	Name     string   `validate:"required,min=2,max=16"`
	Age      int      `validate:"gte=18,lte=99"`
	Tags     []string `validate:"dive,required,min=2"`
	Role     string   `validate:"oneof=admin user"`
	Homepage string   `validate:"omitempty,url"`
	Ignored  string
	internal string `validate:"required"`
}

func TestCheckPasses(t *testing.T) {
	target := &profile{
		Embedded: Embedded{Email: "dev@example.com"},
		Name:     "leego",
		Age:      30,
		Tags:     []string{"go", "http"},
		Role:     "admin",
		Homepage: "https://example.com/docs",
	}

	if err := validate.Check(target); err != nil {
		t.Fatalf("期望校验通过, 实际: %v", err)
	}
}

func TestCheckCollectsAllFailures(t *testing.T) {
	target := &profile{
		Embedded: Embedded{Email: "not-an-email"},
		Name:     "x",
		Age:      17,
		Tags:     []string{"go", ""},
		Role:     "guest",
		Homepage: "example.com",
	}

	err := validate.Check(target)
	if err == nil {
		t.Fatal("期望校验失败")
	}

	var failures validate.Errors
	if !errors.As(err, &failures) {
		t.Fatalf("错误类型 = %T, 期望 validate.Errors", err)
	}

	fields := make(map[string]bool, len(failures))
	for _, item := range failures {
		fields[item.Field+":"+item.Rule] = true
	}

	want := []string{
		"Email:email",
		"Name:min",
		"Age:gte",
		"Tags[1]:required",
		"Tags[1]:min",
		"Role:oneof",
		"Homepage:url",
	}

	for _, key := range want {
		if !fields[key] {
			t.Fatalf("缺少失败项 %q（全部: %s）", key, err)
		}
	}

	if len(failures) != len(want) {
		t.Fatalf("失败数量 = %d, 期望 %d: %s", len(failures), len(want), err)
	}

	if !strings.Contains(err.Error(), "validate: ") {
		t.Fatalf("错误前缀缺失: %v", err)
	}
}

func TestCheckNilPointer(t *testing.T) {
	var target *profile
	if err := validate.Check(target); err == nil {
		t.Fatal("期望 nil 指针报错")
	}

	if err := validate.Check(nil); err == nil {
		t.Fatal("期望 nil 参数报错")
	}

	if err := validate.Check(profile{}); err == nil {
		t.Fatal("值类型缺少必填字段应失败")
	}
}

func TestCheckUnknownRule(t *testing.T) {
	type broken struct {
		Name string `validate:"unknown_rule"`
	}

	if err := validate.Check(&broken{Name: "x"}); err == nil {
		t.Fatal("未知规则应返回错误")
	}

	type brokenDive struct {
		Items []string `validate:"dive"`
	}

	if err := validate.Check(&brokenDive{Items: []string{"a"}}); err == nil {
		t.Fatal("dive 后缺少规则应返回错误")
	}
}

func TestCheckVar(t *testing.T) {
	if err := validate.CheckVar("dev@example.com", "required,email"); err != nil {
		t.Fatalf("期望通过: %v", err)
	}

	err := validate.CheckVar("ab", "min=3")
	if err == nil {
		t.Fatal("期望失败")
	}

	var failures validate.Errors
	if !errors.As(err, &failures) || failures[0].Field != "value" {
		t.Fatalf("错误内容不符合预期: %v", err)
	}

	if err := validate.CheckVar([]string{"a"}, "dive,required"); err == nil {
		t.Fatal("CheckVar 不支持 dive")
	}
}

func TestRegisterRule(t *testing.T) {
	custom := func(value reflect.Value, param string) bool {
		return value.Kind() == reflect.String && strings.HasPrefix(value.String(), param)
	}

	if err := validate.RegisterRule("has_prefix_custom", custom); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	type data struct {
		Code string `validate:"has_prefix_custom=LG-"`
	}

	if err := validate.Check(&data{Code: "LG-100"}); err != nil {
		t.Fatalf("期望通过: %v", err)
	}

	if err := validate.Check(&data{Code: "XX-100"}); err == nil {
		t.Fatal("期望失败")
	}

	if err := validate.RegisterRule("has_prefix_custom", custom); err == nil {
		t.Fatal("重复注册应失败")
	}

	if err := validate.RegisterRule("", custom); err == nil {
		t.Fatal("空规则名应失败")
	}

	if err := validate.RegisterRule("dive", custom); err == nil {
		t.Fatal("dive 是保留名")
	}

	if err := validate.RegisterRule("nil_rule", nil); err == nil {
		t.Fatal("nil 规则应失败")
	}
}

func TestCheckMapDive(t *testing.T) {
	type data struct {
		Labels map[string]string `validate:"dive,required,min=2"`
	}

	if err := validate.Check(&data{Labels: map[string]string{"env": "prod", "tier": "api"}}); err != nil {
		t.Fatalf("期望通过: %v", err)
	}

	err := validate.Check(&data{Labels: map[string]string{"env": "x"}})
	if err == nil || !strings.Contains(err.Error(), "Labels[env]") {
		t.Fatalf("map 元素路径不正确: %v", err)
	}
}

func TestCheckPointerFieldRequired(t *testing.T) {
	type data struct {
		Value *int `validate:"required"`
	}

	if err := validate.Check(&data{}); err == nil {
		t.Fatal("nil 指针字段应触发 required")
	}

	number := 10
	if err := validate.Check(&data{Value: &number}); err != nil {
		t.Fatalf("非 nil 指针应通过: %v", err)
	}
}

func TestCheckConcurrent(t *testing.T) {
	target := &profile{
		Embedded: Embedded{Email: "dev@example.com"},
		Name:     "leego",
		Age:      30,
		Tags:     []string{"go"},
		Role:     "user",
	}

	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			wait.Done()
			for round := 0; round < 50; round++ {
				if err := validate.Check(target); err != nil {
					t.Errorf("并发校验失败: %v", err)
					return
				}
			}
		}()
	}

	wait.Wait()
}

func TestCheckRuleMatrix(t *testing.T) {
	tests := []struct {
		name  string
		value any
		tags  string
		pass  bool
	}{
		{"字符串长度按字符计", "中文", "min=2", true},
		{"max 失败", "abcd", "max=3", false},
		{"len 精确", []int{1, 2}, "len=2", true},
		{"eq 数字", 42, "eq=42", true},
		{"ne 数字", 42, "ne=41", true},
		{"gt 数字", 3.5, "gt=3", true},
		{"lte 字符串长度", "abc", "lte=3", true},
		{"contains", "leego", "contains=ee", true},
		{"prefix", "leego", "prefix=le", true},
		{"suffix", "leego", "suffix=go", true},
		{"uuid", "123e4567-e89b-12d3-a456-426614174000", "uuid", true},
		{"ipv4", "192.168.1.1", "ipv4", true},
		{"ipv6", "::1", "ipv6", true},
		{"ip 失败", "999.1.1.1", "ip", false},
		{"alpha", "leego", "alpha", true},
		{"alnum", "leego2024", "alnum", true},
		{"numeric", "2024", "numeric", true},
		{"lowercase", "leego", "lowercase", true},
		{"uppercase 失败", "leego", "uppercase", false},
		{"oneof 失败", "guest", "oneof=admin user", false},
		{"非法参数按失败处理", "abc", "min=x", false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validate.CheckVar(test.value, test.tags)
			if test.pass && err != nil {
				t.Fatalf("%v 以 %q 校验应通过: %v", test.value, test.tags, err)
			}

			if !test.pass && err == nil {
				t.Fatalf("%v 以 %q 校验应失败", test.value, test.tags)
			}
		})
	}
}

func TestErrorsError(t *testing.T) {
	var empty validate.Errors
	if empty.Error() != "" {
		t.Fatal("空集合的 Error 应为空串")
	}

	failures := validate.Errors{
		{Field: "Name", Rule: "min", Param: "2"},
		{Field: "Email", Rule: "email"},
	}

	got := failures.Error()
	want := "validate: Name: min=2; Email: email"
	if got != want {
		t.Fatalf("Error() = %q, 期望 %q", got, want)
	}
}

// Example 展示错误信息的稳定格式。
func ExampleErrors() {
	err := validate.CheckVar("x", "min=2")

	var failures validate.Errors
	if errors.As(err, &failures) {
		fmt.Println(failures[0].Field, failures[0].Rule, failures[0].Param)
	}
	// Output: value min 2
}
