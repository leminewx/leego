package openapi

import (
	"reflect"
	"testing"
)

// tagSample 用于验证 json 标签解析与未导出字段过滤。
type tagSample struct {
	Plain   string
	Renamed string `json:"renamed,omitempty"`
	Skipped string `json:"-"`
	Hidden  string `json:"hidden"`
	secret  string
}

// TestSchemaOfNumberKinds 验证有符号、无符号与浮点的模式映射。
func TestSchemaOfNumberKinds(t *testing.T) {
	cases := []struct {
		name       string
		value      any
		wantType   string
		wantFormat string
	}{
		{name: "uint32", value: uint32(1), wantType: "integer", wantFormat: "int32"},
		{name: "uint64", value: uint64(1), wantType: "integer", wantFormat: "int64"},
		{name: "float32", value: float32(1), wantType: "number", wantFormat: "float"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			schema := SchemaOf(testCase.value)
			if schema["type"] != testCase.wantType || schema["format"] != testCase.wantFormat {
				t.Fatalf("模式 = %v, want type=%s format=%s", schema, testCase.wantType, testCase.wantFormat)
			}
		})
	}
}

// TestSchemaOfCollections 验证切片与映射的元素模式。
func TestSchemaOfCollections(t *testing.T) {
	array := SchemaOf([]int32{})
	if array["type"] != "array" {
		t.Fatalf("切片模式 = %v", array)
	}

	items, ok := array["items"].(map[string]any)
	if !ok || items["type"] != "integer" || items["format"] != "int32" {
		t.Fatalf("切片元素模式 = %v", array["items"])
	}

	mapped := SchemaOf(map[string]bool{})
	if mapped["type"] != "object" {
		t.Fatalf("映射模式 = %v", mapped)
	}

	additional, ok := mapped["additionalProperties"].(map[string]any)
	if !ok || additional["type"] != "boolean" {
		t.Fatalf("映射值模式 = %v", mapped["additionalProperties"])
	}
}

// TestSchemaOfPointerAndInterface 验证指针透传与接口回退。
func TestSchemaOfPointerAndInterface(t *testing.T) {
	value := 1
	if pointer := SchemaOf(&value); pointer["type"] != "integer" {
		t.Fatalf("指针模式 = %v", pointer)
	}

	var target *any
	if schema := SchemaOf(target); len(schema) != 0 {
		t.Fatalf("接口模式应为空对象: %v", schema)
	}
}

// TestFieldNameParsesTags 验证 json 标签的名称与可选性解析。
func TestFieldNameParsesTags(t *testing.T) {
	typ := reflect.TypeOf(tagSample{})
	cases := []struct {
		field    string
		wantName string
		optional bool
	}{
		{field: "Plain", wantName: "Plain", optional: false},
		{field: "Renamed", wantName: "renamed", optional: true},
		{field: "Skipped", wantName: "", optional: false},
		{field: "Hidden", wantName: "hidden", optional: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.field, func(t *testing.T) {
			field, ok := typ.FieldByName(testCase.field)
			if !ok {
				t.Fatalf("字段 %s 不存在", testCase.field)
			}

			name, optional := fieldName(field)
			if name != testCase.wantName || optional != testCase.optional {
				t.Fatalf("fieldName(%s) = (%q, %v), want (%q, %v)",
					testCase.field, name, optional, testCase.wantName, testCase.optional)
			}
		})
	}
}

// TestSchemaOfSkipsUnexportedFields 验证未导出字段不参与模式生成。
func TestSchemaOfSkipsUnexportedFields(t *testing.T) {
	schema := SchemaOf(tagSample{})

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("属性集合异常: %v", schema)
	}

	if _, exists := properties["secret"]; exists {
		t.Fatal("未导出字段不应出现在模式中")
	}

	required, _ := schema["required"].([]string)
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		requiredSet[name] = true
	}

	if requiredSet["renamed"] {
		t.Fatal("omitempty 字段不应是必填")
	}

	if !requiredSet["Plain"] || !requiredSet["hidden"] {
		t.Fatalf("必填字段缺失: %v", required)
	}
}

// TestSplitTag 验证 json 标签的逗号拆分。
func TestSplitTag(t *testing.T) {
	empty := splitTag("")
	if len(empty) != 1 || empty[0] != "" {
		t.Fatalf("splitTag(\"\") = %v", empty)
	}

	parts := splitTag("name,omitempty,string")
	if len(parts) != 3 || parts[0] != "name" || parts[1] != "omitempty" || parts[2] != "string" {
		t.Fatalf("splitTag() = %v", parts)
	}
}
