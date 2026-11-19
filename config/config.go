// Package config 提供零依赖的配置加载、类型转换与热更新。
//
// 能力边界：
//   - 来源：内存映射（默认值）、JSON 文件、环境变量、命令行参数，按顺序覆盖；
//   - 键名：统一转小写并使用点号分层，嵌套对象会被展平；
//   - 热更新：文件来源按修改时间轮询，变化后重建快照并按 key 派发订阅回调；
//   - 并发：内部使用不可变快照 + atomic.Pointer，读取路径无锁。
//
// 不支持 YAML/TOML：需要时可在业务侧解析后通过 MapSource 注入，保持核心零依赖。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// _DEFAULT_WATCH_INTERVAL 为文件轮询的默认间隔。
	_DEFAULT_WATCH_INTERVAL = time.Second
	// _TAG_CONFIG 为字段读取的配置标签名。
	_TAG_CONFIG = "config"
	// _TAG_DEFAULT 为字段默认值标签名。
	_TAG_DEFAULT = "default"
)

// ErrWatchUnsupported 表示来源不支持热更新。
var ErrWatchUnsupported = errors.New("config: source does not support watch")

// Source 定义配置来源。
//
// Load 返回本次的完整数据；Watch 在数据变化时回调 notify，返回停止函数。
// 不支持热更新的来源可以返回 ErrWatchUnsupported，Load 会忽略该错误。
type Source interface {
	// Load 加载一份完整配置。
	Load() (map[string]any, error)
	// Watch 监听变化；notify 的参数为变化后的完整数据。
	Watch(notify func(map[string]any)) (stop func(), err error)
}

// watchIntervalSetter 允许 Config 覆盖来源的轮询间隔。
type watchIntervalSetter interface {
	setWatchInterval(interval time.Duration)
}

// Config 持有不可变配置快照，可被并发读取。
type Config struct {
	// values 指向当前快照；替换而不是修改，保证读取无锁。
	values atomic.Pointer[map[string]any]
	// sources 为加载顺序固定的来源列表。
	sources []Source

	// subsMu 保护订阅表。
	subsMu sync.RWMutex
	// subs 保存 key 到回调列表的映射。
	subs map[string][]func(any)

	// watchMu 保护停止函数与监听状态。
	watchMu sync.Mutex
	// stops 保存各来源的停止函数。
	stops []func()
	// watching 标记是否已启动监听。
	watching bool

	// reloadSignal 用于合并高频变化，容量为 1。
	reloadSignal chan struct{}
	// done 在 Close 时关闭，用于退出监听协程。
	done chan struct{}
	// closeOnce 保证 Close 幂等。
	closeOnce sync.Once
}

// Load 按顺序加载并合并来源，后者覆盖前者。
//
// 参数 sources 为空时返回空配置，不会报错；任一来源加载失败立即返回。
func Load(sources ...Source) (*Config, error) {
	instance := &Config{
		sources:      make([]Source, 0, len(sources)),
		subs:         make(map[string][]func(any)),
		reloadSignal: make(chan struct{}, 1),
		done:         make(chan struct{}),
	}

	for _, source := range sources {
		if source != nil {
			instance.sources = append(instance.sources, source)
		}
	}

	merged, err := mergeSources(instance.sources)
	if err != nil {
		return nil, err
	}

	instance.values.Store(&merged)
	return instance, nil
}

// Get 返回 key 对应的原始值。
func (own *Config) Get(key string) (any, bool) {
	values := own.values.Load()
	if values == nil {
		return nil, false
	}

	value, ok := (*values)[normalizeKey(key)]
	return value, ok
}

// GetString 返回字符串值；缺失或无法转换时返回 fallback。
func (own *Config) GetString(key, fallback string) string {
	value, ok := own.Get(key)
	if !ok {
		return fallback
	}

	text, ok := toString(value)
	if !ok {
		return fallback
	}

	return text
}

// GetInt 返回 int 值；缺失或无法转换时返回 fallback。
func (own *Config) GetInt(key string, fallback int) int {
	value, ok := own.Get(key)
	if !ok {
		return fallback
	}

	number, ok := toInt64(value)
	if !ok {
		return fallback
	}

	return int(number)
}

// GetBool 返回布尔值；缺失或无法转换时返回 fallback。
func (own *Config) GetBool(key string, fallback bool) bool {
	value, ok := own.Get(key)
	if !ok {
		return fallback
	}

	flag, ok := toBool(value)
	if !ok {
		return fallback
	}

	return flag
}

// GetFloat64 返回浮点值；缺失或无法转换时返回 fallback。
func (own *Config) GetFloat64(key string, fallback float64) float64 {
	value, ok := own.Get(key)
	if !ok {
		return fallback
	}

	number, ok := toFloat64(value)
	if !ok {
		return fallback
	}

	return number
}

// GetDuration 返回时长；字符串按 time.ParseDuration 解析，数字按纳秒处理。
func (own *Config) GetDuration(key string, fallback time.Duration) time.Duration {
	value, ok := own.Get(key)
	if !ok {
		return fallback
	}

	duration, ok := toDuration(value)
	if !ok {
		return fallback
	}

	return duration
}

// Snapshot 返回当前快照的浅拷贝，供诊断页面或日志展示。
func (own *Config) Snapshot() map[string]any {
	values := own.values.Load()
	if values == nil {
		return map[string]any{}
	}

	copied := make(map[string]any, len(*values))
	for key, value := range *values {
		copied[key] = value
	}

	return copied
}

// OnChange 订阅指定 key 的变化，返回取消函数。
//
// 语义约定：
//   - 回调在独立协程中执行，不阻塞配置加载；
//   - 只在值确实变化时触发，新增或删除 key 也视为变化；
//   - 取消函数幂等，可在回调内部调用。
func (own *Config) OnChange(key string, notify func(any)) (cancel func()) {
	if notify == nil {
		return func() {}
	}

	normalized := normalizeKey(key)
	own.subsMu.Lock()
	own.subs[normalized] = append(own.subs[normalized], notify)
	own.subsMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			own.subsMu.Lock()
			callbacks := own.subs[normalized]
			filtered := callbacks[:0]
			for _, callback := range callbacks {
				if reflect.ValueOf(callback).Pointer() != reflect.ValueOf(notify).Pointer() {
					filtered = append(filtered, callback)
				}
			}

			if len(filtered) == 0 {
				delete(own.subs, normalized)
			} else {
				own.subs[normalized] = filtered
			}

			own.subsMu.Unlock()
		})
	}
}

// Watch 启动热更新监听。
//
// 参数 interval 大于 0 时覆盖来源构造时的 UseWatchInterval 配置；
// 小于等于 0 时保留来源自身的间隔（默认 1 秒）。
// 可重复调用，只有第一次生效；不支持监听的来源会被跳过。
func (own *Config) Watch(interval time.Duration) error {
	own.watchMu.Lock()
	defer own.watchMu.Unlock()

	if own.watching {
		return nil
	}

	for _, source := range own.sources {
		if interval > 0 {
			if setter, ok := source.(watchIntervalSetter); ok {
				setter.setWatchInterval(interval)
			}
		}

		stop, err := source.Watch(func(map[string]any) { own.scheduleReload() })
		if errors.Is(err, ErrWatchUnsupported) {
			continue
		}

		if err != nil {
			for _, item := range own.stops {
				item()
			}

			own.stops = nil
			return err
		}

		own.stops = append(own.stops, stop)
	}

	own.watching = true
	go own.runReloadLoop()
	return nil
}

// Close 停止监听并释放资源；可重复调用。
func (own *Config) Close() error {
	own.closeOnce.Do(func() {
		close(own.done)

		own.watchMu.Lock()
		stops := own.stops
		own.stops = nil
		own.watching = false
		own.watchMu.Unlock()

		for _, stop := range stops {
			if stop != nil {
				stop()
			}
		}
	})

	return nil
}

// scheduleReload 向监听协程投递一次刷新请求，已存在待处理请求时直接丢弃。
func (own *Config) scheduleReload() {
	select {
	case own.reloadSignal <- struct{}{}:
	default:
	}
}

// runReloadLoop 串行执行刷新，保证 apply 不会被并发调用。
func (own *Config) runReloadLoop() {
	for {
		select {
		case <-own.done:
			return
		case <-own.reloadSignal:
			if err := own.reload(); err != nil {
				// 刷新失败保留旧快照，避免把半成品配置推给业务。
				continue
			}
		}
	}
}

// reload 重新加载全部来源并应用。
func (own *Config) reload() error {
	merged, err := mergeSources(own.sources)
	if err != nil {
		return err
	}

	own.apply(merged)
	return nil
}

// apply 替换快照并按 key 派发回调。
func (own *Config) apply(next map[string]any) {
	previous := own.values.Swap(&next)
	if previous == nil {
		return
	}

	own.subsMu.RLock()
	subscribers := make(map[string][]func(any), len(own.subs))
	for key, callbacks := range own.subs {
		subscribers[key] = slices.Clone(callbacks)
	}
	own.subsMu.RUnlock()

	for key, callbacks := range subscribers {
		newValue, exists := next[key]
		oldValue, hadOld := (*previous)[key]
		if exists && hadOld && reflect.DeepEqual(newValue, oldValue) {
			continue
		}

		if !exists && !hadOld {
			continue
		}

		for _, callback := range callbacks {
			go safelyNotify(callback, newValue)
		}
	}
}

// safelyNotify 执行回调并吞掉 panic，避免订阅者拖垮配置刷新。
func safelyNotify(callback func(any), value any) {
	defer func() { _ = recover() }()
	callback(value)
}

// mergeSources 按顺序合并全部来源。
func mergeSources(sources []Source) (map[string]any, error) {
	merged := make(map[string]any)
	for _, source := range sources {
		values, err := source.Load()
		if err != nil {
			return nil, err
		}

		for key, value := range values {
			merged[normalizeKey(key)] = value
		}
	}

	return merged, nil
}

// Bind 将配置绑定到结构体。
//
// 字段通过 `config:"server.read_timeout"` 指定键名，`default:"5s"` 指定缺失时的
// 回退值；未声明 config 标签的导出字段会被跳过。
// 支持的类型：string、bool、整数、浮点数、time.Duration、[]string 及其指针。
func (own *Config) Bind(target any) error {
	if target == nil {
		return errors.New("config: bind target must not be nil")
	}

	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("config: bind target must be a non-nil pointer")
	}

	value = value.Elem()
	if value.Kind() != reflect.Struct {
		return fmt.Errorf("config: bind target must point to a struct, got %s", value.Kind())
	}

	structType := value.Type()
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		key := normalizeKey(field.Tag.Get(_TAG_CONFIG))
		if key == "" || field.PkgPath != "" {
			continue
		}

		fieldValue := value.Field(index)
		if !fieldValue.CanSet() {
			continue
		}

		raw, exists := own.Get(key)
		if !exists {
			raw = field.Tag.Get(_TAG_DEFAULT)
			if raw == "" {
				continue
			}
		}

		if err := assignValue(fieldValue, raw); err != nil {
			return fmt.Errorf("config: field %s (%s): %w", field.Name, key, err)
		}
	}

	return nil
}

// assignValue 将原始值转换后写入字段。
func assignValue(field reflect.Value, raw any) error {
	if field.Kind() == reflect.Pointer {
		item := reflect.New(field.Type().Elem())
		if err := assignValue(item.Elem(), raw); err != nil {
			return err
		}

		field.Set(item)
		return nil
	}

	if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.String {
		items, err := toStringSlice(raw)
		if err != nil {
			return err
		}

		field.Set(reflect.ValueOf(items))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		text, ok := toString(raw)
		if !ok {
			return fmt.Errorf("cannot convert %T to string", raw)
		}

		field.SetString(text)

	case reflect.Bool:
		flag, ok := toBool(raw)
		if !ok {
			return fmt.Errorf("cannot convert %T to bool", raw)
		}

		field.SetBool(flag)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			duration, ok := toDuration(raw)
			if !ok {
				return fmt.Errorf("cannot convert %T to time.Duration", raw)
			}

			field.SetInt(int64(duration))
			return nil
		}

		number, ok := toInt64(raw)
		if !ok {
			return fmt.Errorf("cannot convert %T to integer", raw)
		}

		field.SetInt(number)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number, ok := toInt64(raw)
		if !ok || number < 0 {
			return fmt.Errorf("cannot convert %T to unsigned integer", raw)
		}

		field.SetUint(uint64(number))

	case reflect.Float32, reflect.Float64:
		number, ok := toFloat64(raw)
		if !ok {
			return fmt.Errorf("cannot convert %T to float", raw)
		}

		field.SetFloat(number)

	default:
		return fmt.Errorf("unsupported field type %s", field.Type())
	}

	return nil
}

// normalizeKey 统一 key 形态：去空格、转小写、下划线替换为点号。
func normalizeKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, "_", ".")
	return strings.Trim(key, ".")
}

// toString 将值转为字符串。
func toString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	case bool:
		return strconv.FormatBool(typed), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case time.Duration:
		return typed.String(), true
	case fmt.Stringer:
		return typed.String(), true
	default:
		return "", false
	}
}

// toInt64 将值转为整数。
func toInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint64:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case json.Number:
		number, err := strconv.ParseInt(typed.String(), 10, 64)
		return number, err == nil
	default:
		text, ok := toString(value)
		if !ok {
			return 0, false
		}

		number, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		return number, err == nil
	}
}

// toFloat64 将值转为浮点数。
func toFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		text, ok := toString(value)
		if !ok {
			return 0, false
		}

		number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		return number, err == nil
	}
}

// toBool 将值转为布尔。
func toBool(value any) (bool, bool) {
	if flag, ok := value.(bool); ok {
		return flag, true
	}

	text, ok := toString(value)
	if !ok {
		return false, false
	}

	flag, err := strconv.ParseBool(strings.TrimSpace(text))
	return flag, err == nil
}

// toDuration 将值转为时长；数字按纳秒解释。
func toDuration(value any) (time.Duration, bool) {
	switch typed := value.(type) {
	case time.Duration:
		return typed, true
	case int:
		return time.Duration(typed), true
	case int64:
		return time.Duration(typed), true
	case float64:
		return time.Duration(typed), true
	}

	text, ok := toString(value)
	if !ok {
		return 0, false
	}

	duration, err := time.ParseDuration(strings.TrimSpace(text))
	return duration, err == nil
}

// toStringSlice 将值转为字符串切片。
func toStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := toString(item)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to string", item)
			}

			items = append(items, text)
		}

		return items, nil
	default:
		text, ok := toString(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %T to []string", value)
		}

		if strings.TrimSpace(text) == "" {
			return nil, nil
		}

		parts := strings.Split(text, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			items = append(items, strings.TrimSpace(part))
		}

		return items, nil
	}
}
