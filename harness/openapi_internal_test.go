package harness

import (
	"context"
	"strings"
	"testing"
	"time"

	"gitee.com/leminewx/leego/openapi"
)

// newMetadataService 创建用于元数据测试的隔离服务。
func newMetadataService(t *testing.T) *Service {
	t.Helper()

	service, err := NewService(WithDataDir(t.TempDir()), WithLogging(false))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if shutdownErr := service.Shutdown(ctx); shutdownErr != nil {
			t.Errorf("Shutdown() error = %v", shutdownErr)
		}
	})

	return service
}

// TestOpenAPIOperationsMatchRoutes 验证声明式操作表与已注册路由一一对应。
//
// 声明键写错时覆盖不会生效，文档会悄悄退回自动推导结果；
// 这里用测试把这种漂移暴露出来。
func TestOpenAPIOperationsMatchRoutes(t *testing.T) {
	service := newMetadataService(t)

	registered := make(map[string]bool, 32)
	for _, route := range service.GetEngine().GetRoutes() {
		key := strings.ToLower(strings.TrimSpace(route.Method)) + " " + openapi.ConvertPattern(route.Pattern)
		registered[key] = true
	}

	for key := range openAPIOperations() {
		method, pattern, ok := strings.Cut(key, " ")
		if !ok {
			t.Fatalf("声明键缺少方法: %q", key)
		}

		normalized := strings.ToLower(strings.TrimSpace(method)) + " " + openapi.ConvertPattern(pattern)
		if !registered[normalized] {
			t.Errorf("声明了不存在的路由: %s", key)
		}
	}
}

// TestOpenAPIResponsesDeclared 验证每条声明都有响应，且至少包含一个成功码。
func TestOpenAPIResponsesDeclared(t *testing.T) {
	for key, operation := range openAPIOperations() {
		if len(operation.Responses) == 0 {
			t.Errorf("%s 没有声明响应", key)
			continue
		}

		success := false
		for code := range operation.Responses {
			if strings.HasPrefix(code, "2") {
				success = true
			}
		}

		if !success {
			t.Errorf("%s 缺少 2xx 响应", key)
		}
	}
}

// TestOpenAPISchemaRefsResolve 验证响应体引用的命名模式都已注册。
func TestOpenAPISchemaRefsResolve(t *testing.T) {
	schemas := openAPISchemas()

	for key, operation := range openAPIOperations() {
		for code, response := range operation.Responses {
			for media, item := range response.Content {
				schema, ok := item.Schema.(map[string]any)
				if !ok {
					continue
				}

				target, ok := schema["$ref"].(string)
				if !ok {
					continue
				}

				name := strings.TrimPrefix(target, "#/components/schemas/")
				if _, exists := schemas[name]; !exists {
					t.Errorf("%s %s %s 引用了未注册的模式 %q", key, code, media, name)
				}
			}
		}
	}
}

// TestOpenAPITagsDeclared 验证操作引用的分组都已声明。
func TestOpenAPITagsDeclared(t *testing.T) {
	declared := make(map[string]bool, 16)
	for _, option := range openAPITagOptions() {
		settings := openapi.NewDefaultOptions()
		option(&settings)

		for _, tag := range settings.Tags {
			declared[tag.Name] = true
		}
	}

	for key, operation := range openAPIOperations() {
		if len(operation.Tags) == 0 {
			t.Errorf("%s 缺少分组", key)
			continue
		}

		for _, tag := range operation.Tags {
			if !declared[tag] {
				t.Errorf("%s 引用了未声明的分组 %q", key, tag)
			}
		}
	}
}

// TestRouteTagsMatchDeclarations 验证路由标签与声明的分组一致。
//
// 运行期指标后台按路由标签分组，文档按操作分组；两者漂移会让
// 同一接口在两个页面落到不同分组。
func TestRouteTagsMatchDeclarations(t *testing.T) {
	service := newMetadataService(t)
	operations := openAPIOperations()

	for _, route := range service.GetEngine().GetRoutes() {
		key := strings.ToLower(strings.TrimSpace(route.Method)) + " " + openapi.ConvertPattern(route.Pattern)
		declared, ok := operations[key]
		if !ok {
			// 平台路由（文档、分析后台等）由组件自行注册，不在声明表内。
			continue
		}

		if len(route.Tags) == 0 {
			// 平台路由由组件自行注册，没有业务标签；分组由声明表补充。
			continue
		}

		if len(declared.Tags) != len(route.Tags) {
			t.Errorf("%s 分组不一致: 路由=%v 声明=%v", key, route.Tags, declared.Tags)
			continue
		}

		for index := range route.Tags {
			if route.Tags[index] != declared.Tags[index] {
				t.Errorf("%s 第 %d 个分组不一致: 路由=%q 声明=%q", key, index, route.Tags[index], declared.Tags[index])
			}
		}
	}
}
