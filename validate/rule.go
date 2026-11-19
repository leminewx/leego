package validate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// RuleFunc 定义一条校验规则。
//
// 参数说明：
//   - value 为已解引用接口与指针的字段值，保证 IsValid 为 true；
//   - param 为标签中等号后的内容，例如 min=3 的 "3"，无参数时为空串。
//
// 返回 true 表示通过，false 表示失败。
type RuleFunc func(value reflect.Value, param string) bool

// 常用格式规则的正则；在包初始化时编译一次，避免每请求重复编译。
var (
	// _emailPattern 为常见邮箱格式，刻意不接受带引号或注释的 RFC 5322 全量语法。
	_emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
	// _uuidPattern 匹配 8-4-4-4-12 形式的 UUID。
	_uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// _alphaPattern 匹配纯字母。
	_alphaPattern = regexp.MustCompile(`^[A-Za-z]+$`)
	// _alnumPattern 匹配字母与数字。
	_alnumPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	// _numericPattern 匹配纯数字。
	_numericPattern = regexp.MustCompile(`^[0-9]+$`)
)

// ruleMu 保护规则表；RegisterRule 只在启动期调用，读路径本身也加读锁以支持热注册。
var ruleMu sync.RWMutex

// ruleTable 保存规则名到实现的映射，内置规则在包初始化时写入。
var ruleTable = map[string]RuleFunc{
	"required":  ruleRequired,
	"omitempty": ruleSkipAlways,
	"min":       ruleMin,
	"max":       ruleMax,
	"len":       ruleLength,
	"eq":        ruleEqual,
	"ne":        ruleNotEqual,
	"gt":        ruleGreaterThan,
	"gte":       ruleGreaterThanOrEqual,
	"lt":        ruleLessThan,
	"lte":       ruleLessThanOrEqual,
	"oneof":     ruleOneOf,
	"contains":  ruleContains,
	"prefix":    rulePrefix,
	"suffix":    ruleSuffix,
	"email":     ruleEmail,
	"url":       ruleURL,
	"uuid":      ruleUUID,
	"ip":        ruleIP,
	"ipv4":      ruleIPv4,
	"ipv6":      ruleIPv6,
	"alpha":     ruleAlpha,
	"alnum":     ruleAlnum,
	"numeric":   ruleNumeric,
	"lowercase": ruleLowercase,
	"uppercase": ruleUppercase,
}

// RegisterRule 注册自定义规则。
//
// 规则名不能为空、不能是保留字 dive，也不能与已有规则重名；
// 重复注册返回错误而不是覆盖，避免多个包静默争抢同名规则。
func RegisterRule(name string, rule RuleFunc) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("validate: rule name must not be empty")
	}

	if name == _RULE_DIVE {
		return fmt.Errorf("validate: %q is a reserved rule name", name)
	}

	if rule == nil {
		return fmt.Errorf("validate: rule %q must not be nil", name)
	}

	ruleMu.Lock()
	defer ruleMu.Unlock()

	if _, exists := ruleTable[name]; exists {
		return fmt.Errorf("validate: rule %q already registered", name)
	}

	ruleTable[name] = rule
	return nil
}

// lookupRule 按名称查询规则。
func lookupRule(name string) (RuleFunc, bool) {
	ruleMu.RLock()
	rule, ok := ruleTable[name]
	ruleMu.RUnlock()

	return rule, ok
}

// ruleRequired 判断值是否非零。
func ruleRequired(value reflect.Value, _ string) bool {
	return !value.IsZero()
}

// ruleSkipAlways 永远返回 true，仅用于让 omitempty 参与标签解析。
//
// 真正的“跳过后续规则”语义由 checkValue 在命中零值时提前返回实现，
// 放在规则表里是为了统一未知规则检测。
func ruleSkipAlways(_ reflect.Value, _ string) bool {
	return true
}

// ruleMin 判断数值或长度是否不小于参数。
func ruleMin(value reflect.Value, param string) bool {
	limit, ok := parseFloat(param)
	if !ok {
		return false
	}

	actual, ok := numericOrLength(value)
	return ok && actual >= limit
}

// ruleMax 判断数值或长度是否不大于参数。
func ruleMax(value reflect.Value, param string) bool {
	limit, ok := parseFloat(param)
	if !ok {
		return false
	}

	actual, ok := numericOrLength(value)
	return ok && actual <= limit
}

// ruleLength 判断长度是否等于参数。
func ruleLength(value reflect.Value, param string) bool {
	limit, ok := parseFloat(param)
	if !ok {
		return false
	}

	length, ok := lengthOf(value)
	return ok && float64(length) == limit
}

// ruleEqual 判断值是否等于参数。
func ruleEqual(value reflect.Value, param string) bool {
	if number, ok := numericValue(value); ok {
		limit, parsed := parseFloat(param)
		return parsed && number == limit
	}

	text, ok := stringOf(value)
	return ok && text == param
}

// ruleNotEqual 判断值是否不等于参数。
func ruleNotEqual(value reflect.Value, param string) bool {
	return !ruleEqual(value, param)
}

// ruleGreaterThan 判断数值或长度是否大于参数。
func ruleGreaterThan(value reflect.Value, param string) bool {
	limit, ok := parseFloat(param)
	if !ok {
		return false
	}

	actual, ok := numericOrLength(value)
	return ok && actual > limit
}

// ruleGreaterThanOrEqual 判断数值或长度是否不小于参数。
func ruleGreaterThanOrEqual(value reflect.Value, param string) bool {
	return ruleMin(value, param)
}

// ruleLessThan 判断数值或长度是否小于参数。
func ruleLessThan(value reflect.Value, param string) bool {
	limit, ok := parseFloat(param)
	if !ok {
		return false
	}

	actual, ok := numericOrLength(value)
	return ok && actual < limit
}

// ruleLessThanOrEqual 判断数值或长度是否不大于参数。
func ruleLessThanOrEqual(value reflect.Value, param string) bool {
	return ruleMax(value, param)
}

// ruleOneOf 判断字符串是否属于空格分隔的枚举。
func ruleOneOf(value reflect.Value, param string) bool {
	text, ok := stringOf(value)
	if !ok {
		return false
	}

	for _, candidate := range strings.Fields(param) {
		if text == candidate {
			return true
		}
	}

	return false
}

// ruleContains 判断字符串是否包含参数。
func ruleContains(value reflect.Value, param string) bool {
	text, ok := stringOf(value)
	return ok && strings.Contains(text, param)
}

// rulePrefix 判断字符串是否以参数开头。
func rulePrefix(value reflect.Value, param string) bool {
	text, ok := stringOf(value)
	return ok && strings.HasPrefix(text, param)
}

// ruleSuffix 判断字符串是否以参数结尾。
func ruleSuffix(value reflect.Value, param string) bool {
	text, ok := stringOf(value)
	return ok && strings.HasSuffix(text, param)
}

// ruleEmail 判断字符串是否为常见邮箱格式。
func ruleEmail(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && _emailPattern.MatchString(text)
}

// ruleURL 判断字符串是否为带 scheme 的绝对 URL。
func ruleURL(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	if !ok {
		return false
	}

	parsed, err := url.ParseRequestURI(text)
	return err == nil && parsed.Scheme != ""
}

// ruleUUID 判断字符串是否为 UUID。
func ruleUUID(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && _uuidPattern.MatchString(text)
}

// ruleIP 判断字符串是否为 IP 地址。
func ruleIP(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && net.ParseIP(text) != nil
}

// ruleIPv4 判断字符串是否为 IPv4 地址。
func ruleIPv4(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	if !ok {
		return false
	}

	ip := net.ParseIP(text)
	return ip != nil && ip.To4() != nil
}

// ruleIPv6 判断字符串是否为 IPv6 地址。
func ruleIPv6(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	if !ok {
		return false
	}

	ip := net.ParseIP(text)
	return ip != nil && ip.To4() == nil
}

// ruleAlpha 判断字符串是否只包含字母。
func ruleAlpha(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && _alphaPattern.MatchString(text)
}

// ruleAlnum 判断字符串是否只包含字母与数字。
func ruleAlnum(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && _alnumPattern.MatchString(text)
}

// ruleNumeric 判断字符串是否只包含数字。
func ruleNumeric(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && _numericPattern.MatchString(text)
}

// ruleLowercase 判断字符串是否全为小写。
func ruleLowercase(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && text == strings.ToLower(text)
}

// ruleUppercase 判断字符串是否全为大写。
func ruleUppercase(value reflect.Value, _ string) bool {
	text, ok := stringOf(value)
	return ok && text == strings.ToUpper(text)
}

// parseFloat 解析浮点参数。
func parseFloat(param string) (float64, bool) {
	param = strings.TrimSpace(param)
	if param == "" {
		return 0, false
	}

	number, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return 0, false
	}

	return number, true
}

// stringOf 将字符串或实现了 String 的具名类型转为字符串。
func stringOf(value reflect.Value) (string, bool) {
	if value.Kind() != reflect.String {
		return "", false
	}

	return value.String(), true
}

// numericValue 返回数值类型（含无符号）的浮点表示。
func numericValue(value reflect.Value) (float64, bool) {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint()), true
	case reflect.Float32, reflect.Float64:
		return value.Float(), true
	default:
		return 0, false
	}
}

// lengthOf 返回字符串（按字符计）或集合的长度。
func lengthOf(value reflect.Value) (int, bool) {
	switch value.Kind() {
	case reflect.String:
		return utf8.RuneCountInString(value.String()), true
	case reflect.Slice, reflect.Array, reflect.Map:
		return value.Len(), true
	default:
		return 0, false
	}
}

// numericOrLength 优先按数值比较，其次按长度比较。
func numericOrLength(value reflect.Value) (float64, bool) {
	if number, ok := numericValue(value); ok {
		return number, true
	}

	length, ok := lengthOf(value)
	return float64(length), ok
}
