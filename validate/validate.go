// Package validate 提供基于结构体标签的轻量校验。
//
// 设计取舍：
//   - 只依赖反射与标准库，规则表可扩展，不做结构体代码生成；
//   - 元信息按 reflect.Type 缓存，热路径上只做值判断，不重复解析标签；
//   - 未知规则在首次校验时返回错误而非静默忽略，避免“规则名写错导致永远通过”。
package validate

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

const (
	// _TAG_NAME 为默认读取的结构体标签名。
	_TAG_NAME = "validate"
	// _RULE_DIVE 表示其后的规则作用于集合元素。
	_RULE_DIVE = "dive"
	// _RULE_REQUIRED 为必填规则名，nil 指针的特殊语义依赖它。
	_RULE_REQUIRED = "required"
	// _RULE_OMITEMPTY 表示值为零时跳过其后的全部规则。
	_RULE_OMITEMPTY = "omitempty"
	// _MAX_VALUE_PREVIEW 限制错误信息中回显的字符串长度。
	_MAX_VALUE_PREVIEW = 64
)

// FieldError 描述单个字段的校验失败。
type FieldError struct {
	// Field 为字段路径，嵌套字段使用点号，集合元素使用 [下标] 或 [键]。
	Field string `json:"field"`
	// Rule 为触发失败的规则名。
	Rule string `json:"rule"`
	// Param 为规则参数，例如 min=3 中的 3。
	Param string `json:"param,omitempty"`
	// Value 为失败时的字段值预览；复杂类型不回显，避免日志膨胀。
	Value any `json:"value,omitempty"`
}

// Errors 是字段错误的集合，实现 error 接口。
//
// 空集合视为 nil：调用方可以统一使用 `err == nil` 判断校验是否通过。
type Errors []FieldError

// Error 返回可直接阅读的错误描述。
func (own Errors) Error() string {
	if len(own) == 0 {
		return ""
	}

	parts := make([]string, 0, len(own))
	for _, item := range own {
		if item.Param != "" {
			parts = append(parts, fmt.Sprintf("%s: %s=%s", item.Field, item.Rule, item.Param))
			continue
		}

		parts = append(parts, fmt.Sprintf("%s: %s", item.Field, item.Rule))
	}

	return "validate: " + strings.Join(parts, "; ")
}

// Check 校验结构体或其指针。
//
// 参数 target 必须为非 nil 的结构体指针；解引用后逐字段执行标签规则。
// 校验失败返回 Errors（可断言），编程错误（未知规则、类型不支持）返回普通 error。
func Check(target any) error {
	if target == nil {
		return errors.New("validate: target must not be nil")
	}

	value := reflect.ValueOf(target)
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return errors.New("validate: target must not be a nil pointer")
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return fmt.Errorf("validate: target must be a struct, got %s", value.Kind())
	}

	metadata, err := resolveStruct(value.Type())
	if err != nil {
		return err
	}

	var failures Errors
	checkFields(value, metadata, "", &failures)
	if len(failures) == 0 {
		return nil
	}

	return failures
}

// CheckVar 校验单个值，tags 的语义与结构体标签一致。
//
// 单个值没有字段名，错误中的 Field 固定为 "value"；
// CheckVar 不支持 dive，需要按元素校验时请改用结构体字段。
func CheckVar(value any, tags string) error {
	rules, dive, elementRules, err := parseRules(tags)
	if err != nil {
		return err
	}

	if dive || len(elementRules) > 0 {
		return errors.New("validate: CheckVar does not support dive")
	}

	var failures Errors
	checkValue(reflect.ValueOf(value), rules, "value", &failures)
	if len(failures) == 0 {
		return nil
	}

	return failures
}

// ruleSpec 保存一条解析后的规则。
type ruleSpec struct {
	// name 为规则名。
	name string
	// param 为等号后的参数。
	param string
}

// fieldMeta 保存一个需要校验的字段元信息。
type fieldMeta struct {
	// index 为相对结构体根的下标路径，支持匿名嵌套。
	index []int
	// name 为错误信息中展示的字段路径。
	name string
	// rules 为字段自身的规则。
	rules []ruleSpec
	// dive 表示 rules 作用于集合元素。
	dive bool
	// elementRules 为 dive 之后的元素规则。
	elementRules []ruleSpec
}

// structMeta 保存一个结构体的字段元信息。
type structMeta struct {
	// fields 为待校验字段列表。
	fields []fieldMeta
}

// structCache 缓存 reflect.Type 到元信息的映射。
var structCache sync.Map

// resolveStruct 返回结构体元信息，首次调用时解析标签并缓存。
func resolveStruct(structType reflect.Type) (*structMeta, error) {
	if cached, ok := structCache.Load(structType); ok {
		return cached.(*structMeta), nil
	}

	metadata := &structMeta{}
	// visited 阻断匿名嵌入形成的类型环：A 嵌入 B、B 嵌入 A 时
	// 无限递归会直接栈溢出，而这类写法在 Go 中完全合法。
	visited := make(map[reflect.Type]struct{}, 4)
	if err := collectFields(structType, nil, "", metadata, visited); err != nil {
		return nil, err
	}

	actual, _ := structCache.LoadOrStore(structType, metadata)
	return actual.(*structMeta), nil
}

// collectFields 递归收集字段；匿名嵌套结构体在无标签时展开。
//
// visited 记录本次解析已经展开过的结构体类型，用于阻断循环嵌入。
func collectFields(structType reflect.Type, prefix []int, namePrefix string, metadata *structMeta, visited map[reflect.Type]struct{}) error {
	if _, exists := visited[structType]; exists {
		return nil
	}

	visited[structType] = struct{}{}

	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		if field.PkgPath != "" {
			// 未导出字段无法通过 Interface 读取，直接跳过。
			continue
		}

		path := append(append([]int(nil), prefix...), index)
		tags := field.Tag.Get(_TAG_NAME)

		if field.Anonymous && tags == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}

			if embedded.Kind() == reflect.Struct && embedded != structType {
				if err := collectFields(embedded, path, namePrefix, metadata, visited); err != nil {
					return err
				}

				continue
			}
		}

		if tags == "" {
			continue
		}

		rules, dive, elementRules, err := parseRules(tags)
		if err != nil {
			return fmt.Errorf("validate: field %s: %w", field.Name, err)
		}

		metadata.fields = append(metadata.fields, fieldMeta{
			index:        path,
			name:         namePrefix + field.Name,
			rules:        rules,
			dive:         dive,
			elementRules: elementRules,
		})
	}

	return nil
}

// parseRules 解析标签串，返回字段规则、是否 dive 以及元素规则。
func parseRules(tags string) ([]ruleSpec, bool, []ruleSpec, error) {
	tags = strings.TrimSpace(tags)
	if tags == "" {
		return nil, false, nil, nil
	}

	var (
		rules        []ruleSpec
		elementRules []ruleSpec
		dive         bool
	)

	for _, token := range strings.Split(tags, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		if token == _RULE_DIVE {
			dive = true
			continue
		}

		name, param, _ := strings.Cut(token, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, false, nil, fmt.Errorf("validate: empty rule in %q", tags)
		}

		if _, ok := lookupRule(name); !ok {
			return nil, false, nil, fmt.Errorf("validate: unknown rule %q", name)
		}

		spec := ruleSpec{name: name, param: strings.TrimSpace(param)}
		if dive {
			elementRules = append(elementRules, spec)
			continue
		}

		rules = append(rules, spec)
	}

	if dive && len(elementRules) == 0 {
		return nil, false, nil, errors.New("validate: dive must be followed by at least one rule")
	}

	return rules, dive, elementRules, nil
}

// checkFields 按元信息逐字段校验。
func checkFields(value reflect.Value, metadata *structMeta, namePrefix string, failures *Errors) {
	for _, field := range metadata.fields {
		fieldValue, ok := fieldByIndex(value, field.index)
		if !ok {
			continue
		}

		name := namePrefix + field.name
		if !field.dive {
			checkValue(fieldValue, field.rules, name, failures)
			continue
		}

		checkElements(fieldValue, field.elementRules, name, failures)
	}
}

// checkElements 对切片、数组与 map 逐元素执行规则。
func checkElements(value reflect.Value, rules []ruleSpec, name string, failures *Errors) {
	value = unwrap(value)
	if !value.IsValid() {
		return
	}

	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			checkValue(value.Index(index), rules, fmt.Sprintf("%s[%d]", name, index), failures)
		}

	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			checkValue(iterator.Value(), rules, fmt.Sprintf("%s[%v]", name, iterator.Key().Interface()), failures)
		}
	}
}

// checkValue 对单个值执行规则，nil 只对 required 报错。
func checkValue(value reflect.Value, rules []ruleSpec, name string, failures *Errors) {
	if len(rules) == 0 {
		return
	}

	unwrapped := unwrap(value)
	if !unwrapped.IsValid() {
		for _, rule := range rules {
			if rule.name == _RULE_REQUIRED {
				*failures = append(*failures, FieldError{Field: name, Rule: _RULE_REQUIRED})
				return
			}
		}

		return
	}

	for _, rule := range rules {
		// omitempty 必须前置：命中后其后的规则全部跳过。
		if rule.name == _RULE_OMITEMPTY && unwrapped.IsZero() {
			return
		}

		ruleFunc, ok := lookupRule(rule.name)
		if !ok || !ruleFunc(unwrapped, rule.param) {
			*failures = append(*failures, FieldError{
				Field: name,
				Rule:  rule.name,
				Param: rule.param,
				Value: previewValue(unwrapped),
			})
		}
	}
}

// unwrap 逐层解引用接口与指针；nil 返回零值。
func unwrap(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}

		value = value.Elem()
	}

	return value
}

// fieldByIndex 安全地按下标路径取值，路径上出现 nil 指针时返回 false。
func fieldByIndex(value reflect.Value, index []int) (reflect.Value, bool) {
	current := value
	for _, position := range index {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				return reflect.Value{}, false
			}

			current = current.Elem()
		}

		if current.Kind() != reflect.Struct || position >= current.NumField() {
			return reflect.Value{}, false
		}

		current = current.Field(position)
	}

	return current, true
}

// previewValue 返回适合放进错误信息的字段值。
func previewValue(value reflect.Value) any {
	switch value.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return value.Interface()

	case reflect.String:
		text := value.String()
		if len(text) > _MAX_VALUE_PREVIEW {
			return text[:_MAX_VALUE_PREVIEW] + "..."
		}

		return text

	default:
		return nil
	}
}
