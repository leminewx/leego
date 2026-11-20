package openapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitee.com/leminewx/leego/openapi"
)

// Address 用于模式生成测试的嵌套结构。
type Address struct {
	City string `json:"city"`
}

// User 用于模式生成测试的示例结构。
type User struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Tags      []string          `json:"tags"`
	Attrs     map[string]string `json:"attrs"`
	Address   Address           `json:"address"`
	Score     *float64          `json:"score"`
	Skip      string            `json:"-"`
	hidden    string
}

// sampleRoutes 实现 RouteSource。
type sampleRoutes struct {
	routes []openapi.Route
}

// GetRoutes 返回预设路由。
func (own sampleRoutes) GetRoutes() []openapi.Route {
	return own.routes
}

func TestConvertPattern(t *testing.T) {
	cases := map[string]string{
		"/users/:id":     "/users/{id}",
		"/files/*path":   "/files/{path}",
		"users/:id/edit": "/users/{id}/edit",
		"":               "",
		"/plain":         "/plain",
	}

	for input, want := range cases {
		if got := openapi.ConvertPattern(input); got != want {
			t.Fatalf("ConvertPattern(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSpecMarshalsStably(t *testing.T) {
	source := sampleRoutes{routes: []openapi.Route{
		{Method: http.MethodGet, Pattern: "/users/:id", Name: "get-user", Tags: []string{"vip"}},
		{Method: http.MethodPost, Pattern: "/orders"},
	}}

	first, err := openapi.NewSpec(source).MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	second, err := openapi.NewSpec(source).MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("两次序列化结果不一致")
	}

	text := string(first)
	for _, want := range []string{
		`"openapi": "3.1.0"`,
		`"/users/{id}"`,
		`"/orders"`,
		`"operationId": "get-user"`,
		`"name": "id"`,
		`"required": true`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("文档缺少 %q\n%s", want, text)
		}
	}

	var document map[string]any
	if err = json.Unmarshal(first, &document); err != nil {
		t.Fatalf("文档不是合法 JSON: %v", err)
	}

	paths, ok := document["paths"].(map[string]any)
	if !ok || len(paths) != 2 {
		t.Fatalf("paths = %v", document["paths"])
	}
}

func TestSpecAddSchema(t *testing.T) {
	spec := openapi.NewSpec(nil)
	spec.AddSchema("User", User{})
	spec.AddOperation(http.MethodGet, "/users/:id", openapi.Operation{OperationID: "get-user"})

	data, err := spec.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var document struct {
		Components struct {
			Schemas map[string]struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}

	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	user, ok := document.Components.Schemas["User"]
	if !ok {
		t.Fatalf("缺少 User 模式: %s", data)
	}

	if user.Type != "object" {
		t.Fatalf("User 类型 = %q", user.Type)
	}

	if _, ok = user.Properties["created_at"]; !ok {
		t.Fatal("缺少 created_at 字段")
	}

	if _, ok = user.Properties["Skip"]; ok {
		t.Fatal("json:\"-\" 字段不应出现")
	}

	required := make(map[string]bool, len(user.Required))
	for _, name := range user.Required {
		required[name] = true
	}

	if required["name"] {
		t.Fatal("omitempty 字段不应是必填")
	}

	if !required["id"] || !required["created_at"] {
		t.Fatalf("必填字段缺失: %v", user.Required)
	}
}

func TestBuildSchemaPrimitives(t *testing.T) {
	cases := map[string]map[string]any{
		"bool":    {"type": "boolean"},
		"string":  {"type": "string"},
		"int32":   {"type": "integer", "format": "int32"},
		"int64":   {"type": "integer", "format": "int64"},
		"float64": {"type": "number", "format": "double"},
	}

	for name, want := range cases {
		var value any
		switch name {
		case "bool":
			value = true
		case "string":
			value = "x"
		case "int32":
			value = int32(1)
		case "int64":
			value = int64(1)
		case "float64":
			value = 1.0
		}

		got := openapi.BuildSchema(value)
		for key, wantValue := range want {
			if got[key] != wantValue {
				t.Fatalf("BuildSchema(%s)[%s] = %v, want %v", name, key, got[key], wantValue)
			}
		}
	}

	if schema := openapi.BuildSchema(time.Now()); schema["format"] != "date-time" {
		t.Fatalf("time.Time 模式 = %v", schema)
	}

	if schema := openapi.BuildSchema(nil); len(schema) != 0 {
		t.Fatalf("nil 模式 = %v", schema)
	}
}

// node 是自引用类型，用于验证深度截断。
type node struct {
	Name string `json:"name"`
	Next *node  `json:"next"`
}

func TestBuildSchemaTruncatesDepth(t *testing.T) {
	schema := openapi.BuildSchema(node{})
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("顶层模式异常: %v", schema)
	}

	current := properties["next"]
	for depth := 0; depth < 20; depth++ {
		next, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("第 %d 层模式异常: %v", depth, current)
		}

		if next["type"] == "object" && next["properties"] == nil {
			// 命中深度上限后的回退模式。
			return
		}

		inner, ok := next["properties"].(map[string]any)
		if !ok {
			t.Fatalf("第 %d 层缺少 properties: %v", depth, next)
		}

		current = inner["next"]
	}

	t.Fatal("自引用类型未被深度截断")
}

func TestHandlers(t *testing.T) {
	spec := openapi.NewSpec(sampleRoutes{routes: []openapi.Route{
		{Method: http.MethodGet, Pattern: "/ping", Name: "ping"},
	}})

	document := httptest.NewRecorder()
	openapi.NewHandler(spec).ServeHTTP(document, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if document.Code != http.StatusOK || !strings.Contains(document.Body.String(), "/ping") {
		t.Fatalf("文档响应异常: %d %s", document.Code, document.Body.String())
	}

	page := httptest.NewRecorder()
	openapi.NewUIHandler(spec).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/openapi", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("页面状态码 = %d", page.Code)
	}

	body := page.Body.String()
	if !strings.Contains(body, "openapi.json") || !strings.Contains(body, "Leego OpenAPI") {
		t.Fatalf("页面内容异常: %s", body)
	}

	empty := httptest.NewRecorder()
	openapi.NewHandler(nil).ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(empty.Body.String(), "3.1.0") {
		t.Fatalf("空文档异常: %s", empty.Body.String())
	}
}

// TestHandlerCachesDocument 验证文档在创建处理器时序列化一次。
//
// 运行期再给 Spec 加路由不应影响已创建的处理器的输出，
// 这样每个请求都不需要重新编码整份文档。
func TestHandlerCachesDocument(t *testing.T) {
	spec := openapi.NewSpec(sampleRoutes{routes: []openapi.Route{
		{Method: http.MethodGet, Pattern: "/first", Name: "first"},
	}})

	handler := openapi.NewHandler(spec)
	spec.AddRoute(openapi.Route{Method: http.MethodGet, Pattern: "/second", Name: "second"})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))

	body := recorder.Body.String()
	if !strings.Contains(body, "/first") {
		t.Fatalf("文档缺少初始路由: %s", body)
	}

	if strings.Contains(body, "/second") {
		t.Fatalf("处理器不应反映创建之后新增的路由: %s", body)
	}
}

func TestUseOptions(t *testing.T) {
	spec := openapi.NewSpec(nil,
		openapi.WithTitle("订单服务"),
		openapi.WithVersion("2.3.0"),
		openapi.WithDescription("示例文档"),
	)

	data, err := spec.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	text := string(data)
	for _, want := range []string{"订单服务", "2.3.0", "示例文档"} {
		if !strings.Contains(text, want) {
			t.Fatalf("文档缺少 %q", want)
		}
	}
}

// TestSpecStructuredDocument 验证服务地址、分组、安全方案、命名模式、
// 请求体与多状态码响应都能写入文档。
func TestSpecStructuredDocument(t *testing.T) {
	source := sampleRoutes{routes: []openapi.Route{
		{Method: http.MethodGet, Pattern: "/users/:id", Name: "get-user", Tags: []string{"用户"}},
		{Method: http.MethodPost, Pattern: "/users", Name: "create-user", Tags: []string{"用户"}},
	}}

	spec := openapi.NewSpec(source,
		openapi.WithTitle("用户服务"),
		openapi.WithVersion("3.0.0"),
		openapi.WithTermsOfService("https://example.com/terms"),
		openapi.WithContact("维护者", "https://example.com", "dev@example.com"),
		openapi.WithLicense("MIT", "https://example.com/license"),
		openapi.WithServer("https://api.example.com", "生产环境"),
		openapi.WithTag("用户", "用户相关接口"),
		openapi.WithSecurityScheme("bearerAuth", openapi.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		}),
		openapi.WithSchema("UserProfile", User{}),
		openapi.WithOperation("GET /users/:id", openapi.Operation{
			Summary:  "查询用户",
			Security: []map[string][]string{{"bearerAuth": nil}},
			Parameters: []openapi.Parameter{
				openapi.NewParameter("verbose", openapi.IN_QUERY, "是否返回扩展字段", false, map[string]any{"type": "boolean"}),
			},
			Responses: map[string]openapi.Response{
				"200": openapi.NewJSONRefResponse("用户详情", "UserProfile"),
				"404": openapi.NewJSONRefResponse("用户不存在", "UserProfile"),
			},
		}),
		openapi.WithOperation("POST /users", openapi.Operation{
			Summary:     "创建用户",
			RequestBody: openapi.NewJSONRequestBody(true, map[string]any{"name": "leego"}),
			Responses: map[string]openapi.Response{
				"201": openapi.NewJSONResponse("已创建", map[string]any{"id": 1}),
				"400": {Description: "参数非法"},
			},
		}),
	)

	data, err := spec.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		`"termsOfService": "https://example.com/terms"`,
		`"contact"`,
		`"license"`,
		`"servers"`,
		`"description": "生产环境"`,
		`"securitySchemes"`,
		`"bearerAuth"`,
		`"bearerFormat": "JWT"`,
		`"$ref": "#/components/schemas/UserProfile"`,
		`"requestBody"`,
		`"operationId": "get-user"`,
		`"verbose"`,
		`"201"`,
		`"404"`,
		`"security"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("文档缺少 %q\n%s", want, text)
		}
	}

	var document struct {
		Servers []openapi.Server `json:"servers"`
		Tags    []openapi.Tag    `json:"tags"`
		Paths   map[string]map[string]struct {
			Summary     string                      `json:"summary"`
			OperationID string                      `json:"operationId"`
			Parameters  []openapi.Parameter         `json:"parameters"`
			Responses   map[string]openapi.Response `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}

	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if len(document.Servers) != 1 || document.Servers[0].URL != "https://api.example.com" {
		t.Fatalf("servers = %+v", document.Servers)
	}

	if len(document.Tags) != 1 || document.Tags[0].Name != "用户" {
		t.Fatalf("tags = %+v", document.Tags)
	}

	if _, ok := document.Components.Schemas["UserProfile"]; !ok {
		t.Fatalf("缺少命名模式: %v", document.Components.Schemas)
	}

	get := document.Paths["/users/{id}"]["get"]
	if get.Summary != "查询用户" || get.OperationID != "get-user" {
		t.Fatalf("get 操作 = %+v", get)
	}

	if len(get.Responses) != 2 {
		t.Fatalf("get 响应数量 = %d", len(get.Responses))
	}
}

// TestUseOperationKeyVariants 验证键的多种写法都能命中同一路由。
func TestUseOperationKeyVariants(t *testing.T) {
	source := sampleRoutes{routes: []openapi.Route{
		{Method: http.MethodGet, Pattern: "/users/:id", Name: "get-user"},
	}}

	for _, key := range []string{"GET /users/:id", "get /users/{id}", "/users/:id"} {
		spec := openapi.NewSpec(source, openapi.WithOperation(key, openapi.Operation{Summary: "查询用户"}))

		data, err := spec.MarshalJSON()
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}

		text := string(data)
		if !strings.Contains(text, "查询用户") {
			t.Fatalf("键 %q 未生效\n%s", key, text)
		}

		// 未声明的字段必须保留自动推导结果。
		if !strings.Contains(text, `"operationId": "get-user"`) {
			t.Fatalf("键 %q 覆盖后丢失 operationId\n%s", key, text)
		}
	}
}

// TestAddTagAndServer 验证运行时追加的顶层元数据。
func TestAddTagAndServer(t *testing.T) {
	spec := openapi.NewSpec(nil)
	spec.AddTag("系统", "系统接口")
	spec.AddServer("/", "同源")
	spec.AddSecurityScheme("basicAuth", openapi.SecurityScheme{Type: "http", Scheme: "basic"})

	data, err := spec.MarshalJSON()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	text := string(data)
	for _, want := range []string{"系统接口", "同源", "basicAuth"} {
		if !strings.Contains(text, want) {
			t.Fatalf("文档缺少 %q\n%s", want, text)
		}
	}
}
