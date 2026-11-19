package openapi

import (
	"reflect"
	"time"
)

const (
	// _MAX_SCHEMA_DEPTH 为模式递归深度上限。
	_MAX_SCHEMA_DEPTH = 8
	// _FALLBACK_SCHEMA_TYPE 为超过深度上限时的回退类型。
	_FALLBACK_SCHEMA_TYPE = "object"
)

// timeType 缓存 time.Time 的类型对象，避免每次反射查表。
var timeType = reflect.TypeOf(time.Time{})

// SchemaOf 基于示例值生成 JSON Schema（OpenAPI 3.1 子集）。
//
// 支持：基本类型、time.Time、结构体、map、切片/数组、指针与接口；
// 递归深度超过 8 层时回退为 {"type":"object"}，避免自引用类型无限展开。
func SchemaOf(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}

	return schemaOfType(reflect.TypeOf(value), 0)
}

// schemaOfType 基于类型生成模式。
func schemaOfType(target reflect.Type, depth int) map[string]any {
	if target == nil {
		return map[string]any{}
	}

	if depth > _MAX_SCHEMA_DEPTH {
		return map[string]any{"type": _FALLBACK_SCHEMA_TYPE}
	}

	if target == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}

	switch target.Kind() {
	case reflect.Pointer:
		// 指针与目标类型同构，nil 只影响必填性而不影响模式。
		return schemaOfType(target.Elem(), depth+1)
	case reflect.Interface:
		return map[string]any{}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32:
		return map[string]any{"type": "number", "format": "float"}
	case reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaOfType(target.Elem(), depth+1),
		}
	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": schemaOfType(target.Elem(), depth+1),
		}
	case reflect.Struct:
		return structSchema(target, depth+1)
	default:
		return map[string]any{"type": _FALLBACK_SCHEMA_TYPE}
	}
}

// structSchema 生成结构体模式。
func structSchema(target reflect.Type, depth int) map[string]any {
	properties := make(map[string]any, target.NumField())
	required := make([]string, 0, target.NumField())

	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
		if field.PkgPath != "" {
			// 未导出字段不参与 JSON 序列化。
			continue
		}

		name, optional := fieldName(field)
		if name == "" {
			continue
		}

		properties[name] = schemaOfType(field.Type, depth)
		if !optional {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// fieldName 解析字段的 JSON 名称与是否可选。
//
// 匿名字段不提升其内部字段：OpenAPI 消费方更关心结构而不是 Go 的嵌入细节，
// 提升会让同一份 DTO 在不同语言里产生不同的字段路径。
func fieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}

	name := field.Name
	optional := false
	if tag != "" {
		parts := splitTag(tag)
		if parts[0] != "" {
			name = parts[0]
		}

		for _, option := range parts[1:] {
			if option == "omitempty" {
				optional = true
			}
		}
	}

	if name == "" {
		return "", false
	}

	return name, optional
}

// splitTag 按逗号拆分 json 标签。
func splitTag(tag string) []string {
	parts := make([]string, 0, 2)
	start := 0
	for index := 0; index < len(tag); index++ {
		if tag[index] == ',' {
			parts = append(parts, tag[start:index])
			start = index + 1
		}
	}

	return append(parts, tag[start:])
}
