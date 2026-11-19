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

func TestSchemaOfPrimitives(t *testing.T) {
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

		got := openapi.SchemaOf(value)
		for key, wantValue := range want {
			if got[key] != wantValue {
				t.Fatalf("SchemaOf(%s)[%s] = %v, want %v", name, key, got[key], wantValue)
			}
		}
	}

	if schema := openapi.SchemaOf(time.Now()); schema["format"] != "date-time" {
		t.Fatalf("time.Time 模式 = %v", schema)
	}

	if schema := openapi.SchemaOf(nil); len(schema) != 0 {
		t.Fatalf("nil 模式 = %v", schema)
	}
}

// node 是自引用类型，用于验证深度截断。
type node struct {
	Name string `json:"name"`
	Next *node  `json:"next"`
}

func TestSchemaOfTruncatesDepth(t *testing.T) {
	schema := openapi.SchemaOf(node{})
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

func TestUseOptions(t *testing.T) {
	spec := openapi.NewSpec(nil,
		openapi.UseTitle("订单服务"),
		openapi.UseVersion("2.3.0"),
		openapi.UseDescription("示例文档"),
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
