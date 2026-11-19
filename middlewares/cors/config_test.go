package cors

import (
	"net/http"
	"testing"
)

// TestSetAllowOrigins 验证来源白名单的设置与规范化。
func TestSetAllowOrigins(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins(" https://b.example ", "https://a.example", "https://a.example")

	origins := cors.GetAllowedOrigins()
	if len(origins) != 2 || origins[0] != "https://a.example" || origins[1] != "https://b.example" {
		t.Fatalf("来源白名单 = %v, want 去重并按字母序", origins)
	}

	// 空参数、空字符串与 "*" 都表示允许任意来源。
	cors.SetAllowOrigins("", "  ")
	if origins = cors.GetAllowedOrigins(); len(origins) != 1 || origins[0] != "*" {
		t.Fatalf("空白名单应回退为通配符，实际 = %v", origins)
	}

	cors.SetAllowOrigins("https://a.example", "*")
	if origins = cors.GetAllowedOrigins(); len(origins) != 1 || origins[0] != "*" {
		t.Fatalf("包含通配符时应允许任意来源，实际 = %v", origins)
	}
}

// TestSetAllowCredentials 验证凭证开关与通配符互斥。
func TestSetAllowCredentials(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowOrigins("https://a.example")
	cors.SetAllowCredentials(true)

	if !cors.GetConfig().AllowCredentials {
		t.Fatal("具体来源下应允许开启凭证")
	}

	cors.SetAllowOrigins()
	cors.SetAllowCredentials(true)

	if cors.GetConfig().AllowCredentials {
		t.Fatal("通配符源下凭证必须被强制关闭")
	}
}

// TestSetAllowMethods 验证方法白名单的规范化、去重与顺序。
func TestSetAllowMethods(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowMethods(" post ", "POST", "get", "")

	methods := cors.GetAllowedMethods()
	if len(methods) != 2 || methods[0] != "POST" || methods[1] != "GET" {
		t.Fatalf("方法白名单 = %v, want [POST GET]", methods)
	}

	config := cors.GetConfig()
	if len(config.AllowMethods) != 2 {
		t.Fatalf("GetConfig 方法白名单 = %v", config.AllowMethods)
	}

	// 清空白名单后，预检方法校验应该全部失败。
	cors.SetAllowMethods()
	if len(cors.GetAllowedMethods()) != 0 {
		t.Fatalf("空方法列表 = %v, want 空", cors.GetAllowedMethods())
	}

	if cors.isMethodAllowed(http.MethodGet) {
		t.Fatal("空方法列表不应允许任何方法")
	}
}

// TestSetAllowHeaders 验证请求头白名单大小写不敏感去重。
func TestSetAllowHeaders(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetAllowHeaders("X-Token", "x-token", " Accept ", "")

	headerNames := cors.GetAllowedHeaders()
	if len(headerNames) != 2 || headerNames[0] != "X-Token" || headerNames[1] != "Accept" {
		t.Fatalf("请求头白名单 = %v, want [X-Token Accept]", headerNames)
	}
}

// TestSetExposeHeaders 验证暴露响应头去重与副本语义。
func TestSetExposeHeaders(t *testing.T) {
	cors := NewDefaultCORS()
	cors.SetExposeHeaders("X-Total", "x-total")

	headerNames := cors.GetExposedHeaders()
	if len(headerNames) != 1 || headerNames[0] != "X-Total" {
		t.Fatalf("暴露响应头 = %v, want [X-Total]", headerNames)
	}

	// 返回值必须是副本。
	headerNames[0] = "changed"
	if cors.GetExposedHeaders()[0] != "X-Total" {
		t.Fatal("GetExposedHeaders 必须返回副本")
	}
}

// TestSetMaxAge 验证预检缓存时间的设置。
func TestSetMaxAge(t *testing.T) {
	cors := NewDefaultCORS()

	cors.SetMaxAge(120)
	if cors.GetConfig().MaxAgeSeconds != 120 {
		t.Fatalf("MaxAge = %d, want 120", cors.GetConfig().MaxAgeSeconds)
	}

	cors.SetMaxAge(0)
	if cors.GetConfig().MaxAgeSeconds != 0 {
		t.Fatalf("MaxAge = %d, want 0", cors.GetConfig().MaxAgeSeconds)
	}

	cors.SetMaxAge(-1)
	if cors.GetConfig().MaxAgeSeconds != 86400 {
		t.Fatalf("负数应恢复默认值，实际 = %d", cors.GetConfig().MaxAgeSeconds)
	}
}

// TestGetConfigDeepCopy 验证配置返回深拷贝。
func TestGetConfigDeepCopy(t *testing.T) {
	cors := NewDefaultCORS()

	config := cors.GetConfig()
	config.AllowOrigins[0] = "changed"
	config.AllowMethods[0] = "CHANGED"
	config.AllowHeaders = append(config.AllowHeaders, "X-Changed")

	if cors.GetAllowedOrigins()[0] != "*" {
		t.Fatal("修改 AllowOrigins 影响了内部状态")
	}

	if cors.GetAllowedMethods()[0] == "CHANGED" {
		t.Fatal("修改 AllowMethods 影响了内部状态")
	}

	if len(cors.GetAllowedHeaders()) != 0 {
		t.Fatal("修改 AllowHeaders 影响了内部状态")
	}
}

// TestEnableDisable 验证启停开关。
func TestEnableDisable(t *testing.T) {
	cors := NewDefaultCORS()

	cors.Disable()
	if cors.IsEnabled() || cors.GetConfig().IsEnabled {
		t.Fatal("Disable 后必须为禁用状态")
	}

	cors.Enable()
	if !cors.IsEnabled() || !cors.GetConfig().IsEnabled {
		t.Fatal("Enable 后必须为启用状态")
	}
}
