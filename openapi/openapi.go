// Package openapi 提供 OpenAPI 3.1 文档生成能力。
//
// 设计要点：
//   - 本包不导入 leego 根包：根包会导入它来做适配，反向依赖会成环；
//   - 只接受 RouteSource 提供的最小路由描述，便于任意框架复用；
//   - 序列化结果完全确定（encoding/json 对 map 键排序），便于快照对比；
//   - UI 使用内嵌 HTML + 原生 fetch，不引用任何 CDN。
package openapi

import (
	"encoding/json"
	"strings"
)

// 版本与默认值常量。
const (
	// _OPENAPI_VERSION 为支持的规范版本。
	_OPENAPI_VERSION = "3.1.0"
	// _DEFAULT_TITLE 为默认文档标题。
	_DEFAULT_TITLE = "Leego Service"
	// _DEFAULT_VERSION 为默认服务版本。
	_DEFAULT_VERSION = "1.0.0"
	// _PATH_ITEM_GET 为路径项的默认方法键。
	_PATH_ITEM_GET = "get"
)

// Route 描述一条待生成文档的路由。
type Route struct {
	// Method 为 HTTP 方法。
	Method string
	// Pattern 为路由模式，支持 :param 与 *param。
	Pattern string
	// Name 为路由名称，用作 operationId。
	Name string
	// Tags 为业务标签。
	Tags []string
}

// RouteSource 提供路由列表。
type RouteSource interface {
	// GetRoutes 返回当前注册的路由。
	GetRoutes() []Route
}

// Info 对应 OpenAPI 的 info 对象。
type Info struct {
	// Title 为文档标题。
	Title string `json:"title"`
	// Version 为服务版本。
	Version string `json:"version"`
	// Description 为服务描述。
	Description string `json:"description,omitempty"`
}

// Parameter 对应 OpenAPI 的参数对象。
type Parameter struct {
	// Name 为参数名。
	Name string `json:"name"`
	// In 为参数位置，固定为 path。
	In string `json:"in"`
	// Required 表示是否必填，路径参数恒为 true。
	Required bool `json:"required"`
	// Schema 为参数的模式。
	Schema map[string]any `json:"schema"`
}

// Response 对应 OpenAPI 的响应对象。
type Response struct {
	// Description 为响应说明。
	Description string `json:"description"`
	// Content 为响应内容；键为媒体类型。
	Content map[string]any `json:"content,omitempty"`
}

// Operation 对应 OpenAPI 的操作对象。
type Operation struct {
	// OperationID 为唯一操作标识。
	OperationID string `json:"operationId,omitempty"`
	// Summary 为操作摘要。
	Summary string `json:"summary,omitempty"`
	// Tags 为业务标签。
	Tags []string `json:"tags,omitempty"`
	// Parameters 为路径参数列表。
	Parameters []Parameter `json:"parameters,omitempty"`
	// Responses 为响应集合。
	Responses map[string]Response `json:"responses"`
}

// Components 对应 OpenAPI 的 components 对象。
type Components struct {
	// Schemas 为命名模式集合。
	Schemas map[string]any `json:"schemas,omitempty"`
}

// Options 定义文档级配置。
type Options struct {
	// Title 为文档标题。
	Title string
	// Version 为服务版本。
	Version string
	// Description 为服务描述。
	Description string
}

// NewDefaultOptions 返回推荐配置。
func NewDefaultOptions() Options {
	return Options{
		Title:   _DEFAULT_TITLE,
		Version: _DEFAULT_VERSION,
	}
}

// normalize 修正空字段。
func (own Options) normalize() Options {
	defaults := NewDefaultOptions()

	if strings.TrimSpace(own.Title) == "" {
		own.Title = defaults.Title
	}

	if strings.TrimSpace(own.Version) == "" {
		own.Version = defaults.Version
	}

	return own
}

// Option 修改 Options。
type Option func(*Options)

// UseTitle 设置文档标题。
func UseTitle(title string) Option {
	return func(options *Options) {
		options.Title = title
	}
}

// UseVersion 设置服务版本。
func UseVersion(version string) Option {
	return func(options *Options) {
		options.Version = version
	}
}

// UseDescription 设置服务描述。
func UseDescription(description string) Option {
	return func(options *Options) {
		options.Description = description
	}
}

// document 是最终序列化的内部结构。
type document struct {
	OpenAPI    string                          `json:"openapi"`
	Info       Info                            `json:"info"`
	Paths      map[string]map[string]Operation `json:"paths"`
	Components Components                      `json:"components,omitempty"`
}

// Spec 是 OpenAPI 文档的可变构建器。
//
// 并发约定：构建期单协程写入，运行期只读；注册完成后不要再修改。
type Spec struct {
	// info 为文档信息。
	info Info
	// paths 保存"路径模板 → 方法 → 操作"。
	paths map[string]map[string]Operation
	// schemas 保存命名模式。
	schemas map[string]any
}

// NewSpec 创建文档构建器。
//
// source 非 nil 时立即把其中的路由转换为操作；路由必须在注册期确定。
func NewSpec(source RouteSource, options ...Option) *Spec {
	settings := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	settings = settings.normalize()

	own := &Spec{
		info: Info{
			Title:       settings.Title,
			Version:     settings.Version,
			Description: settings.Description,
		},
		paths:   make(map[string]map[string]Operation),
		schemas: make(map[string]any),
	}

	if source != nil {
		for _, route := range source.GetRoutes() {
			own.AddRoute(route)
		}
	}

	return own
}

// AddRoute 把一条路由转换为操作。
func (own *Spec) AddRoute(route Route) {
	method := strings.ToLower(strings.TrimSpace(route.Method))
	if method == "" {
		method = _PATH_ITEM_GET
	}

	pattern := strings.TrimSpace(route.Pattern)
	if pattern == "" {
		return
	}

	operation := Operation{
		OperationID: route.Name,
		Tags:        append([]string(nil), route.Tags...),
		Parameters:  pathParameters(pattern),
		Responses: map[string]Response{
			"200": {Description: "OK"},
		},
	}

	if operation.OperationID == "" {
		operation.OperationID = operationID(method, pattern)
	}

	own.AddOperation(method, pattern, operation)
}

// AddOperation 写入一个操作。
//
// pattern 会被转换为 OpenAPI 路径模板（:id → {id}）。
func (own *Spec) AddOperation(method, pattern string, operation Operation) {
	path := ConvertPattern(pattern)
	if path == "" {
		return
	}

	item, ok := own.paths[path]
	if !ok {
		item = make(map[string]Operation)
		own.paths[path] = item
	}

	if operation.Responses == nil {
		operation.Responses = map[string]Response{"200": {Description: "OK"}}
	}

	item[strings.ToLower(method)] = operation
}

// AddSchema 基于示例值生成命名模式。
func (own *Spec) AddSchema(name string, value any) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	own.schemas[name] = SchemaOf(value)
}

// MarshalJSON 序列化文档。
//
// encoding/json 会对 map 键排序，因此同一份输入的输出字节完全一致。
func (own *Spec) MarshalJSON() ([]byte, error) {
	doc := document{
		OpenAPI: _OPENAPI_VERSION,
		Info:    own.info,
		Paths:   own.paths,
	}

	if len(own.schemas) > 0 {
		doc.Components = Components{Schemas: own.schemas}
	}

	return json.MarshalIndent(doc, "", "  ")
}

// ConvertPattern 把框架路由模式转换为 OpenAPI 路径模板。
//
// 例如 /users/:id → /users/{id}，/files/*path → /files/{path}。
func ConvertPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}

	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}

	segments := strings.Split(pattern, "/")
	for index, segment := range segments {
		if segment == "" {
			continue
		}

		switch segment[0] {
		case ':':
			segments[index] = "{" + segment[1:] + "}"
		case '*':
			segments[index] = "{" + segment[1:] + "}"
		}
	}

	return strings.Join(segments, "/")
}

// pathParameters 提取路径参数。
func pathParameters(pattern string) []Parameter {
	segments := strings.Split(ConvertPattern(pattern), "/")
	parameters := make([]Parameter, 0, len(segments))
	for _, segment := range segments {
		if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' {
			continue
		}

		parameters = append(parameters, Parameter{
			Name:     segment[1 : len(segment)-1],
			In:       "path",
			Required: true,
			Schema:   map[string]any{"type": "string"},
		})
	}

	return parameters
}

// operationID 基于方法与路径生成稳定的操作标识。
func operationID(method, pattern string) string {
	replacer := strings.NewReplacer("/", "_", ":", "", "*", "", "{", "", "}", "", ".", "_")
	name := strings.Trim(replacer.Replace(strings.ToLower(pattern)), "_")
	return method + "_" + name
}
