// Package openapi 提供 OpenAPI 3.1 文档生成能力。
//
// 设计要点：
//   - 本包不导入 leego 根包：根包会导入它来做适配，反向依赖会成环；
//   - 只接受 RouteSource 提供的最小路由描述，便于任意框架复用；
//   - 支持声明式补充：servers、tags、安全方案、请求体、多响应与命名模式；
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
	// _SCHEMA_REF_PREFIX 为命名模式引用的前缀。
	_SCHEMA_REF_PREFIX = "#/components/schemas/"
	// _OPENAPI_OK 为默认成功响应码。
	_OPENAPI_OK = "200"
	// _OPENAPI_DEFAULT_RESPONSE 为默认成功响应说明。
	_OPENAPI_DEFAULT_RESPONSE = "OK"
)

// 参数位置常量，对应 OpenAPI 的 in 字段。
const (
	// IN_PATH 表示路径参数。
	IN_PATH = "path"
	// IN_QUERY 表示查询参数。
	IN_QUERY = "query"
	// IN_HEADER 表示请求头参数。
	IN_HEADER = "header"
	// IN_COOKIE 表示 Cookie 参数。
	IN_COOKIE = "cookie"
)

// 常用媒体类型常量。
const (
	// MEDIA_JSON 为 JSON 媒体类型。
	MEDIA_JSON = "application/json"
	// MEDIA_TEXT 为纯文本媒体类型。
	MEDIA_TEXT = "text/plain"
	// MEDIA_EVENT_STREAM 为 SSE 媒体类型。
	MEDIA_EVENT_STREAM = "text/event-stream"
	// MEDIA_OCTET_STREAM 为二进制流媒体类型。
	MEDIA_OCTET_STREAM = "application/octet-stream"
)

// Contact 对应 info.contact 对象。
type Contact struct {
	// Name 为联系人名称。
	Name string `json:"name,omitempty"`
	// URL 为联系页面地址。
	URL string `json:"url,omitempty"`
	// Email 为联系邮箱。
	Email string `json:"email,omitempty"`
}

// License 对应 info.license 对象。
type License struct {
	// Name 为许可名称。
	Name string `json:"name"`
	// URL 为许可全文地址。
	URL string `json:"url,omitempty"`
}

// Info 对应 OpenAPI 的 info 对象。
type Info struct {
	// Title 为文档标题。
	Title string `json:"title"`
	// Version 为服务版本。
	Version string `json:"version"`
	// Description 为服务描述。
	Description string `json:"description,omitempty"`
	// TermsOfService 为服务条款地址。
	TermsOfService string `json:"termsOfService,omitempty"`
	// Contact 为联系人信息。
	Contact *Contact `json:"contact,omitempty"`
	// License 为许可信息。
	License *License `json:"license,omitempty"`
}

// Server 对应 servers 数组元素。
//
// URL 允许是相对路径（如 "/"），表示与文档同源，便于内嵌文档随实例迁移。
type Server struct {
	// URL 为服务基地址。
	URL string `json:"url"`
	// Description 为该地址的用途说明。
	Description string `json:"description,omitempty"`
}

// Tag 对应顶层 tags 数组元素，用于描述业务分组。
type Tag struct {
	// Name 为分组名称，需与操作上的 tags 一致。
	Name string `json:"name"`
	// Description 为分组说明。
	Description string `json:"description,omitempty"`
}

// MediaType 对应 content 中某个媒体类型的描述。
type MediaType struct {
	// Schema 为 JSON Schema 片段，可以是内联模式或 $ref 引用。
	Schema any `json:"schema,omitempty"`
	// Example 为示例值，UI 会直接渲染为 JSON。
	Example any `json:"example,omitempty"`
}

// RequestBody 对应 requestBody 对象。
type RequestBody struct {
	// Description 为请求体说明。
	Description string `json:"description,omitempty"`
	// Required 表示请求体是否必填。
	Required bool `json:"required,omitempty"`
	// Content 为媒体类型到描述的映射。
	Content map[string]MediaType `json:"content"`
}

// Parameter 对应 OpenAPI 的参数对象。
type Parameter struct {
	// Name 为参数名。
	Name string `json:"name"`
	// In 为参数位置，取值见 IN_* 常量。
	In string `json:"in"`
	// Description 为参数说明。
	Description string `json:"description,omitempty"`
	// Required 表示是否必填，路径参数恒为 true。
	Required bool `json:"required"`
	// Deprecated 表示该参数已废弃。
	Deprecated bool `json:"deprecated,omitempty"`
	// Schema 为参数的模式。
	Schema map[string]any `json:"schema,omitempty"`
}

// Response 对应 OpenAPI 的响应对象。
type Response struct {
	// Description 为响应说明。
	Description string `json:"description"`
	// Content 为响应内容；键为媒体类型。
	Content map[string]MediaType `json:"content,omitempty"`
}

// Operation 对应 OpenAPI 的操作对象。
type Operation struct {
	// OperationID 为唯一操作标识。
	OperationID string `json:"operationId,omitempty"`
	// Summary 为单行摘要。
	Summary string `json:"summary,omitempty"`
	// Description 为详细说明，支持 Markdown。
	Description string `json:"description,omitempty"`
	// Tags 为业务分组。
	Tags []string `json:"tags,omitempty"`
	// Deprecated 表示该操作已废弃。
	Deprecated bool `json:"deprecated,omitempty"`
	// Parameters 为参数列表。
	Parameters []Parameter `json:"parameters,omitempty"`
	// RequestBody 为请求体声明。
	RequestBody *RequestBody `json:"requestBody,omitempty"`
	// Responses 为响应集合，键为状态码或 default。
	Responses map[string]Response `json:"responses"`
	// Security 为操作级安全要求；为空时表示不需要凭据。
	Security []map[string][]string `json:"security,omitempty"`
}

// SecurityScheme 对应 components.securitySchemes 的成员。
//
// 只覆盖最常见的 http（bearer/basic）与 apiKey 两类；
// oauth2、openIdConnect 的流程描述过于依赖具体实现，留待调用方自行扩展。
type SecurityScheme struct {
	// Type 为方案类型：http 或 apiKey。
	Type string `json:"type"`
	// Description 为方案说明。
	Description string `json:"description,omitempty"`
	// Scheme 为 http 方案名：bearer 或 basic。
	Scheme string `json:"scheme,omitempty"`
	// BearerFormat 为 bearer 令牌的格式提示，例如 JWT。
	BearerFormat string `json:"bearerFormat,omitempty"`
	// In 为 apiKey 的位置：header、query 或 cookie。
	In string `json:"in,omitempty"`
	// Name 为 apiKey 的参数名。
	Name string `json:"name,omitempty"`
}

// Components 对应 OpenAPI 的 components 对象。
type Components struct {
	// Schemas 为命名模式集合。
	Schemas map[string]any `json:"schemas,omitempty"`
	// SecuritySchemes 为命名安全方案集合。
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// Route 描述一条待生成文档的路由。
//
// 只有 Method 与 Pattern 是必需的；其余字段用于补充结构化语义，
// 未填写的字段会由路由模式自动推导。
type Route struct {
	// Method 为 HTTP 方法。
	Method string
	// Pattern 为路由模式，支持 :param 与 *param。
	Pattern string
	// Name 为路由名称，用作 operationId。
	Name string
	// Tags 为业务分组。
	Tags []string
	// Summary 为单行摘要。
	Summary string
	// Description 为详细说明。
	Description string
	// Deprecated 表示该路由已废弃。
	Deprecated bool
	// Parameters 为额外参数（query / header / cookie），路径参数自动生成。
	Parameters []Parameter
	// RequestBody 为请求体声明。
	RequestBody *RequestBody
	// Responses 为响应声明；为空时只写入默认 200。
	Responses map[string]Response
}

// RouteSource 提供路由列表。
type RouteSource interface {
	// GetRoutes 返回当前注册的路由。
	GetRoutes() []Route
}

// Options 定义文档级配置。
type Options struct {
	// Title 为文档标题。
	Title string
	// Version 为服务版本。
	Version string
	// Description 为服务描述。
	Description string
	// TermsOfService 为服务条款地址。
	TermsOfService string
	// Contact 为联系人信息。
	Contact *Contact
	// License 为许可信息。
	License *License
	// Servers 为服务地址列表。
	Servers []Server
	// Tags 为业务分组说明。
	Tags []Tag
	// SecuritySchemes 为命名安全方案。
	SecuritySchemes map[string]SecurityScheme
	// Schemas 为命名模式的示例值，值会经 BuildSchema 推导为 JSON Schema。
	Schemas map[string]any
	// Operations 为声明式操作覆盖表，键格式为 "METHOD 路由模式"。
	//
	// 键中的方法大小写不敏感，模式既可以是 :param 也可以写成 {param}；
	// 覆盖时只替换显式填写的字段，未填写的部分保留自动推导结果。
	Operations map[string]Operation
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

	own.Title = strings.TrimSpace(own.Title)
	own.Version = strings.TrimSpace(own.Version)
	own.Description = strings.TrimSpace(own.Description)

	return own
}

// Option 修改 Options。
type Option func(*Options)

// WithTitle 设置文档标题。
func WithTitle(title string) Option {
	return func(options *Options) {
		options.Title = title
	}
}

// WithVersion 设置服务版本。
func WithVersion(version string) Option {
	return func(options *Options) {
		options.Version = version
	}
}

// WithDescription 设置服务描述。
func WithDescription(description string) Option {
	return func(options *Options) {
		options.Description = description
	}
}

// WithTermsOfService 设置服务条款地址。
func WithTermsOfService(url string) Option {
	return func(options *Options) {
		options.TermsOfService = url
	}
}

// WithContact 设置联系人信息。
func WithContact(name, url, email string) Option {
	return func(options *Options) {
		options.Contact = &Contact{Name: name, URL: url, Email: email}
	}
}

// WithLicense 设置许可信息。
func WithLicense(name, url string) Option {
	return func(options *Options) {
		options.License = &License{Name: name, URL: url}
	}
}

// WithServer 追加一个服务地址。
func WithServer(url, description string) Option {
	return func(options *Options) {
		options.Servers = append(options.Servers, Server{URL: url, Description: description})
	}
}

// WithTag 追加一个业务分组说明。
func WithTag(name, description string) Option {
	return func(options *Options) {
		options.Tags = append(options.Tags, Tag{Name: name, Description: description})
	}
}

// WithSecurityScheme 追加一个命名安全方案。
func WithSecurityScheme(name string, scheme SecurityScheme) Option {
	return func(options *Options) {
		if options.SecuritySchemes == nil {
			options.SecuritySchemes = make(map[string]SecurityScheme, 2)
		}

		options.SecuritySchemes[name] = scheme
	}
}

// WithOperation 声明一条操作的覆盖项。
//
// key 形如 "GET /api/users/:id"；未匹配到路由时该声明会被忽略，
// 避免因为路由改名而让文档里出现不存在的接口。
func WithOperation(key string, operation Operation) Option {
	return func(options *Options) {
		if options.Operations == nil {
			options.Operations = make(map[string]Operation, 8)
		}

		options.Operations[key] = operation
	}
}

// WithOperations 批量声明操作覆盖项。
func WithOperations(operations map[string]Operation) Option {
	return func(options *Options) {
		for key, operation := range operations {
			WithOperation(key, operation)(options)
		}
	}
}

// WithSchema 注册一个命名模式，供响应体的 $ref 引用。
func WithSchema(name string, value any) Option {
	return func(options *Options) {
		if options.Schemas == nil {
			options.Schemas = make(map[string]any, 8)
		}

		options.Schemas[name] = value
	}
}

// WithSchemas 批量注册命名模式。
func WithSchemas(schemas map[string]any) Option {
	return func(options *Options) {
		for name, value := range schemas {
			WithSchema(name, value)(options)
		}
	}
}

// document 是最终序列化的内部结构。
type document struct {
	OpenAPI    string                          `json:"openapi"`
	Info       Info                            `json:"info"`
	Servers    []Server                        `json:"servers,omitempty"`
	Tags       []Tag                           `json:"tags,omitempty"`
	Paths      map[string]map[string]Operation `json:"paths"`
	Components *Components                     `json:"components,omitempty"`
}

// Spec 是 OpenAPI 文档的可变构建器。
//
// 并发约定：构建期单协程写入，运行期只读；注册完成后不要再修改。
type Spec struct {
	// info 为文档信息。
	info Info
	// servers 为服务地址列表。
	servers []Server
	// tags 为业务分组说明。
	tags []Tag
	// paths 保存"路径模板 → 方法 → 操作"。
	paths map[string]map[string]Operation
	// schemas 保存命名模式。
	schemas map[string]any
	// securitySchemes 保存命名安全方案。
	securitySchemes map[string]SecurityScheme
	// overrides 保存声明式操作覆盖表，键为 "method 路径模板"。
	overrides map[string]Operation
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
			Title:          settings.Title,
			Version:        settings.Version,
			Description:    settings.Description,
			TermsOfService: settings.TermsOfService,
			Contact:        cloneContact(settings.Contact),
			License:        cloneLicense(settings.License),
		},
		servers:         cloneServers(settings.Servers),
		tags:            cloneTags(settings.Tags),
		paths:           make(map[string]map[string]Operation),
		schemas:         make(map[string]any),
		securitySchemes: make(map[string]SecurityScheme, len(settings.SecuritySchemes)),
		overrides:       make(map[string]Operation, len(settings.Operations)),
	}

	for name, scheme := range settings.SecuritySchemes {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}

		own.securitySchemes[name] = scheme
	}

	for key, operation := range settings.Operations {
		if normalized := normalizeOperationKey(key); normalized != "" {
			own.overrides[normalized] = operation
		}
	}

	for name, value := range settings.Schemas {
		own.AddSchema(name, value)
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
	path := ConvertPattern(pattern)
	if path == "" {
		return
	}

	operation := Operation{
		OperationID: route.Name,
		Summary:     route.Summary,
		Description: route.Description,
		Tags:        append([]string(nil), route.Tags...),
		Deprecated:  route.Deprecated,
		Parameters:  pathParameters(pattern),
		RequestBody: route.RequestBody,
		Responses:   cloneResponses(route.Responses),
	}

	if operation.OperationID == "" {
		operation.OperationID = operationID(method, pattern)
	}

	if len(operation.Responses) == 0 {
		operation.Responses = defaultResponses()
	}

	operation.Parameters = mergeParameters(operation.Parameters, route.Parameters)

	if override, ok := own.overrides[method+" "+path]; ok {
		operation = mergeOperation(operation, override)
	}

	own.AddOperation(method, path, operation)
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

	if len(operation.Responses) == 0 {
		operation.Responses = defaultResponses()
	}

	item[strings.ToLower(strings.TrimSpace(method))] = operation
}

// AddSchema 基于示例值生成命名模式。
func (own *Spec) AddSchema(name string, value any) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	own.schemas[name] = BuildSchema(value)
}

// AddServer 追加一个服务地址。
func (own *Spec) AddServer(url, description string) {
	if url = strings.TrimSpace(url); url == "" {
		return
	}

	own.servers = append(own.servers, Server{URL: url, Description: description})
}

// AddTag 追加一个业务分组说明。
func (own *Spec) AddTag(name, description string) {
	if name = strings.TrimSpace(name); name == "" {
		return
	}

	own.tags = append(own.tags, Tag{Name: name, Description: description})
}

// AddSecurityScheme 写入一个命名安全方案。
func (own *Spec) AddSecurityScheme(name string, scheme SecurityScheme) {
	if name = strings.TrimSpace(name); name == "" {
		return
	}

	if own.securitySchemes == nil {
		own.securitySchemes = make(map[string]SecurityScheme, 2)
	}

	own.securitySchemes[name] = scheme
}

// MarshalJSON 序列化文档。
//
// encoding/json 会对 map 键排序，因此同一份输入的输出字节完全一致。
func (own *Spec) MarshalJSON() ([]byte, error) {
	doc := document{
		OpenAPI: _OPENAPI_VERSION,
		Info:    own.info,
		Servers: own.servers,
		Tags:    own.tags,
		Paths:   own.paths,
	}

	if len(own.schemas) > 0 || len(own.securitySchemes) > 0 {
		doc.Components = &Components{
			Schemas:         own.schemas,
			SecuritySchemes: own.securitySchemes,
		}
	}

	return json.MarshalIndent(doc, "", "  ")
}

// NewParameter 创建参数，schema 为 nil 时按字符串处理。
func NewParameter(name, in, description string, required bool, schema map[string]any) Parameter {
	parameter := Parameter{
		Name:        strings.TrimSpace(name),
		In:          strings.TrimSpace(in),
		Description: description,
		Required:    required,
		Schema:      schema,
	}

	if parameter.In == "" {
		parameter.In = IN_QUERY
	}

	if parameter.In == IN_PATH {
		parameter.Required = true
	}

	if parameter.Schema == nil {
		parameter.Schema = map[string]any{"type": "string"}
	}

	return parameter
}

// NewJSONResponse 以示例值推导 JSON 响应体；example 为 nil 时不写 content。
func NewJSONResponse(description string, example any) Response {
	response := Response{Description: description}
	if example == nil {
		return response
	}

	response.Content = map[string]MediaType{
		MEDIA_JSON: {Schema: BuildSchema(example), Example: example},
	}

	return response
}

// NewJSONRefResponse 用 components.schemas 中的命名模式作为 JSON 响应体。
//
// 适合大对象：模式只在 components 里出现一次，多个接口共用同一个 $ref。
func NewJSONRefResponse(description, schemaName string) Response {
	return Response{
		Description: description,
		Content: map[string]MediaType{
			MEDIA_JSON: {Schema: SchemaRef(schemaName)},
		},
	}
}

// NewJSONRequestBody 以示例值推导 JSON 请求体。
func NewJSONRequestBody(required bool, example any) *RequestBody {
	return &RequestBody{
		Required: required,
		Content: map[string]MediaType{
			MEDIA_JSON: {Schema: BuildSchema(example), Example: example},
		},
	}
}

// SchemaRef 返回指向命名模式的引用对象。
func SchemaRef(name string) map[string]any {
	return map[string]any{"$ref": _SCHEMA_REF_PREFIX + strings.TrimSpace(name)}
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
			In:       IN_PATH,
			Required: true,
			Schema:   map[string]any{"type": "string"},
		})
	}

	return parameters
}

// normalizeOperationKey 规范化声明式覆盖表的键。
//
// 接受 "GET /a/:id"、"get /a/{id}" 与 "/a/:id" 三种写法，
// 统一转换为 "method 路径模板"。
func normalizeOperationKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	method := _PATH_ITEM_GET
	pattern := key
	if head, tail, ok := strings.Cut(key, " "); ok {
		if trimmed := strings.ToLower(strings.TrimSpace(head)); trimmed != "" {
			method = trimmed
		}

		pattern = tail
	}

	path := ConvertPattern(pattern)
	if path == "" {
		return ""
	}

	return method + " " + path
}

// defaultResponses 返回默认成功响应。
func defaultResponses() map[string]Response {
	return map[string]Response{
		_OPENAPI_OK: {Description: _OPENAPI_DEFAULT_RESPONSE},
	}
}

// mergeParameters 合并参数：同名且同位置的参数由补充项覆盖。
func mergeParameters(base, extra []Parameter) []Parameter {
	if len(extra) == 0 {
		return base
	}

	merged := append([]Parameter(nil), base...)
	for _, parameter := range extra {
		replaced := false
		for index := range merged {
			if merged[index].Name == parameter.Name && merged[index].In == parameter.In {
				merged[index] = parameter
				replaced = true
				break
			}
		}

		if !replaced {
			merged = append(merged, parameter)
		}
	}

	return merged
}

// mergeOperation 用声明项覆盖自动推导的操作，只替换显式填写的字段。
func mergeOperation(base, declared Operation) Operation {
	if declared.OperationID != "" {
		base.OperationID = declared.OperationID
	}

	if declared.Summary != "" {
		base.Summary = declared.Summary
	}

	if declared.Description != "" {
		base.Description = declared.Description
	}

	if len(declared.Tags) > 0 {
		base.Tags = append([]string(nil), declared.Tags...)
	}

	if declared.Deprecated {
		base.Deprecated = true
	}

	if len(declared.Parameters) > 0 {
		base.Parameters = mergeParameters(base.Parameters, declared.Parameters)
	}

	if declared.RequestBody != nil {
		base.RequestBody = declared.RequestBody
	}

	if len(declared.Responses) > 0 {
		base.Responses = cloneResponses(declared.Responses)
	}

	if len(declared.Security) > 0 {
		base.Security = cloneSecurity(declared.Security)
	}

	return base
}

// cloneResponses 复制响应集合，避免调用方后续修改影响文档。
func cloneResponses(responses map[string]Response) map[string]Response {
	if len(responses) == 0 {
		return nil
	}

	cloned := make(map[string]Response, len(responses))
	for code, response := range responses {
		copied := response
		if len(response.Content) > 0 {
			copied.Content = make(map[string]MediaType, len(response.Content))
			for media, item := range response.Content {
				copied.Content[media] = item
			}
		}

		cloned[code] = copied
	}

	return cloned
}

// cloneSecurity 复制安全要求列表。
func cloneSecurity(security []map[string][]string) []map[string][]string {
	if len(security) == 0 {
		return nil
	}

	cloned := make([]map[string][]string, 0, len(security))
	for _, requirement := range security {
		copied := make(map[string][]string, len(requirement))
		for name, scopes := range requirement {
			copied[name] = append([]string(nil), scopes...)
		}

		cloned = append(cloned, copied)
	}

	return cloned
}

// cloneContact 复制联系人信息。
func cloneContact(contact *Contact) *Contact {
	if contact == nil {
		return nil
	}

	copied := *contact
	return &copied
}

// cloneLicense 复制许可信息。
func cloneLicense(license *License) *License {
	if license == nil {
		return nil
	}

	copied := *license
	return &copied
}

// cloneServers 复制服务地址列表。
func cloneServers(servers []Server) []Server {
	if len(servers) == 0 {
		return nil
	}

	return append([]Server(nil), servers...)
}

// cloneTags 复制分组说明列表。
func cloneTags(tags []Tag) []Tag {
	if len(tags) == 0 {
		return nil
	}

	return append([]Tag(nil), tags...)
}

// operationID 基于方法与路径生成稳定的操作标识。
func operationID(method, pattern string) string {
	replacer := strings.NewReplacer("/", "_", ":", "", "*", "", "{", "", "}", "", ".", "_")
	name := strings.Trim(replacer.Replace(strings.ToLower(pattern)), "_")
	return method + "_" + name
}
