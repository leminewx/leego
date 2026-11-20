package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SourceOption 修改来源的行为。
type SourceOption func(*sourceConfig)

// sourceConfig 保存来源的可选项。
type sourceConfig struct {
	// interval 为文件轮询间隔。
	interval time.Duration
}

// WithWatchInterval 设置文件轮询间隔；小于等于 0 时使用默认值。
func WithWatchInterval(interval time.Duration) SourceOption {
	return func(config *sourceConfig) {
		if interval > 0 {
			config.interval = interval
		}
	}
}

// MapSource 以内存映射作为配置来源，常用于内置默认值。
type MapSource struct {
	// values 为原始映射，构造时做一次浅拷贝。
	values map[string]any
}

// NewMapSource 创建内存来源。
func NewMapSource(values map[string]any) *MapSource {
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}

	return &MapSource{values: copied}
}

// Load 返回配置副本。
func (own *MapSource) Load() (map[string]any, error) {
	if own == nil {
		return nil, errors.New("config: nil map source")
	}

	copied := make(map[string]any, len(own.values))
	for key, value := range own.values {
		copied[key] = value
	}

	return copied, nil
}

// Watch 不支持热更新。
func (own *MapSource) Watch(func(map[string]any)) (func(), error) {
	return nil, ErrWatchUnsupported
}

// JSONSource 以 JSON 文件作为配置来源。
//
// 文件内容必须是对象；嵌套对象会被展平为点号分隔的 key。
// 热更新按文件内容轮询，不依赖文件时间戳精度。
type JSONSource struct {
	// path 为文件路径。
	path string
	// interval 为轮询间隔（纳秒）；用原子变量以支持 Config 在启动监听前覆盖。
	interval atomic.Int64
}

// NewJSONSource 创建 JSON 文件来源。
func NewJSONSource(path string, options ...SourceOption) *JSONSource {
	config := sourceConfig{interval: _DEFAULT_WATCH_INTERVAL}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}

	source := &JSONSource{path: path}
	source.interval.Store(int64(config.interval))
	return source
}

// Load 读取并展平 JSON 文件。
func (own *JSONSource) Load() (map[string]any, error) {
	values, _, err := own.loadFile()
	return values, err
}

// loadFile 读取文件并返回展平结果与原始字节摘要。
//
// 字节摘要用于轮询时跳过未变化的文件，避免重复解析 JSON。
func (own *JSONSource) loadFile() (map[string]any, uint64, error) {
	if own == nil {
		return nil, 0, errors.New("config: nil json source")
	}

	data, err := os.ReadFile(own.path)
	if err != nil {
		return nil, 0, fmt.Errorf("config: read %q: %w", own.path, err)
	}

	decoded := make(map[string]any)
	if err = json.Unmarshal(data, &decoded); err != nil {
		return nil, 0, fmt.Errorf("config: parse %q: %w", own.path, err)
	}

	flattened := make(map[string]any, len(decoded))
	flattenMap("", decoded, flattened)
	return flattened, rawDigest(data), nil
}

// Watch 轮询文件内容；内容变化后重新加载并回调。
//
// 文件短暂不可读（编辑器保存的中间态）时保持上一次成功加载的状态，
// 下一次轮询会再次尝试，避免把半成品配置推给业务。
//
// 判定依据是内容摘要而不是文件时间戳：部分文件系统在连续快速写入时
// 会给出相同的修改时间与大小，仅比较元数据会漏掉变化。
func (own *JSONSource) Watch(notify func(map[string]any)) (func(), error) {
	if own == nil {
		return nil, errors.New("config: nil json source")
	}

	if notify == nil {
		return func() {}, nil
	}

	interval := time.Duration(own.interval.Load())
	if interval <= 0 {
		interval = _DEFAULT_WATCH_INTERVAL
	}

	initial, initialRaw, err := own.loadFile()
	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})
	var once sync.Once
	stopped := func() { once.Do(func() { close(stop) }) }

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lastRaw := initialRaw
		lastDigest := digest(initial)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				values, raw, loadErr := own.loadFile()
				if loadErr != nil {
					continue
				}

				if raw == lastRaw {
					continue
				}
				lastRaw = raw

				// 内容未变时（例如仅调整了空白）不触发回调。
				currentDigest := digest(values)
				if currentDigest == lastDigest {
					continue
				}

				lastDigest = currentDigest
				select {
				case <-stop:
					return
				default:
					notify(values)
				}
			}
		}
	}()

	return stopped, nil
}

// setWatchInterval 覆盖轮询间隔；由 Config 在启动监听前调用。
func (own *JSONSource) setWatchInterval(interval time.Duration) {
	if own != nil && interval > 0 {
		own.interval.Store(int64(interval))
	}
}

// EnvSource 以环境变量作为配置来源。
//
// 只读取指定前缀的变量，去掉前缀后转小写并把下划线替换为点号，
// 例如 LEEGO_SERVER_PORT 对应 server.port。
// 环境变量在进程生命周期内不变，因此不支持热更新。
type EnvSource struct {
	// prefix 为变量名前缀，区分大小写。
	prefix string
}

// NewEnvSource 创建环境变量来源；prefix 为空表示读取全部变量。
func NewEnvSource(prefix string) *EnvSource {
	return &EnvSource{prefix: prefix}
}

// Load 读取环境变量。
func (own *EnvSource) Load() (map[string]any, error) {
	if own == nil {
		return nil, errors.New("config: nil env source")
	}

	values := make(map[string]any)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}

		if own.prefix != "" {
			if !strings.HasPrefix(key, own.prefix) {
				continue
			}

			key = strings.TrimPrefix(key, own.prefix)
		}

		if key == "" {
			continue
		}

		values[normalizeKey(key)] = value
	}

	return values, nil
}

// Watch 不支持热更新。
func (own *EnvSource) Watch(func(map[string]any)) (func(), error) {
	return nil, ErrWatchUnsupported
}

// FlagSource 以命令行参数作为配置来源。
//
// 只包含调用方显式设置过的参数；未设置的参数应通过 MapSource 提供默认值，
// 这样“默认值 < 命令行”的优先级自然成立。
type FlagSource struct {
	// set 为要读取的参数集合。
	set *flag.FlagSet
}

// NewFlagSource 创建命令行来源。
func NewFlagSource(set *flag.FlagSet) *FlagSource {
	return &FlagSource{set: set}
}

// Load 读取已设置的参数。
func (own *FlagSource) Load() (map[string]any, error) {
	if own == nil || own.set == nil {
		return nil, errors.New("config: nil flag set")
	}

	values := make(map[string]any)
	own.set.Visit(func(item *flag.Flag) {
		values[normalizeKey(item.Name)] = item.Value.String()
	})

	return values, nil
}

// Watch 不支持热更新。
func (own *FlagSource) Watch(func(map[string]any)) (func(), error) {
	return nil, ErrWatchUnsupported
}

// flattenMap 将嵌套映射展平为点号分隔的 key。
func flattenMap(prefix string, source map[string]any, target map[string]any) {
	for key, value := range source {
		full := normalizeKey(key)
		if prefix != "" {
			full = prefix + "." + full
		}

		if nested, ok := value.(map[string]any); ok {
			flattenMap(full, nested, target)
			continue
		}

		target[full] = value
	}
}

// rawDigest 计算原始字节摘要，用于判断文件是否需要重新解析。
func rawDigest(data []byte) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write(data)
	return hasher.Sum64()
}

// digest 计算配置内容的摘要，用于区分“文件被重写但内容没变”的情况。
func digest(values map[string]any) uint64 {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)
	hasher := fnv.New64a()
	for _, key := range keys {
		_, _ = fmt.Fprintf(hasher, "%s=%v;", key, values[key])
	}

	return hasher.Sum64()
}
