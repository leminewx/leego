package leego

import (
	"net/http"
	"testing"
)

// TestRouteAdapterConvertsRoutes 验证根包路由信息到 openapi 结构的适配。
func TestRouteAdapterConvertsRoutes(t *testing.T) {
	adapter := &routeAdapter{routes: []RouteInfo{
		{Method: http.MethodGet, Pattern: "/users/:id", Name: "get-user", Tags: []string{"user"}},
		{Method: http.MethodPost, Pattern: "/orders", Name: "post-order"},
	}}

	routes := adapter.GetRoutes()
	if len(routes) != 2 {
		t.Fatalf("路由数量 = %d, want 2", len(routes))
	}

	first := routes[0]
	if first.Method != http.MethodGet || first.Pattern != "/users/:id" || first.Name != "get-user" {
		t.Fatalf("首条路由 = %+v", first)
	}

	if len(first.Tags) != 1 || first.Tags[0] != "user" {
		t.Fatalf("首条路由标签 = %v, want [user]", first.Tags)
	}

	if routes[1].Method != http.MethodPost || routes[1].Pattern != "/orders" {
		t.Fatalf("次条路由 = %+v", routes[1])
	}
}

// TestRouteAdapterEmpty 验证空路由返回空切片而非 nil。
func TestRouteAdapterEmpty(t *testing.T) {
	routes := (&routeAdapter{}).GetRoutes()
	if routes == nil {
		t.Fatal("GetRoutes() = nil, want 空切片")
	}

	if len(routes) != 0 {
		t.Fatalf("路由数量 = %d, want 0", len(routes))
	}
}

// TestRegisterOpenAPIPrefixNormalization 验证前缀补斜杠与去尾斜杠。
func TestRegisterOpenAPIPrefixNormalization(t *testing.T) {
	engine := createTestEngine()
	engine.GET("/ping", func(ctx *Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	}, "ping")

	if err := engine.RegisterOpenAPI("docs/"); err != nil {
		t.Fatalf("RegisterOpenAPI() error = %v", err)
	}

	for _, target := range []string{"/docs", "/docs/openapi.json"} {
		if recorder := performRequest(engine, http.MethodGet, target, nil); recorder.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d, want 200", target, recorder.Code)
		}
	}
}
