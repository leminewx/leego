package leego

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gitee.com/leminewx/leego/middleware/collector"
	"gitee.com/leminewx/leego/tree"
)

func TestRouter(t *testing.T) {
	router := newRouter(nil)

	// 测试路由插入和搜索
	handler := &handler{}
	router.Insert("GET", "/users/:id", handler)

	// 测试搜索
	route, params, found := router.Search("GET", "/users/123")
	if !found {
		t.Error("Expected to find route")
	}
	if route == nil {
		t.Error("Expected non-nil route")
	}
	if params["id"] != "123" {
		t.Errorf("Expected param 'id' to be '123', got '%s'", params["id"])
	}

	// 测试未找到的情况
	_, _, found = router.Search("POST", "/users/123")
	if found {
		t.Error("Expected not to find route for POST method")
	}
}

func TestRouterServeHTTP(t *testing.T) {
	router := newRouter(NewEngine())

	// 创建测试处理器
	called := false
	testHandler := &handler{
		handle: func(ctx *Context) {
			called = true
			_ = ctx.ResponsePlain(200, "OK")
		},
		collector: collector.New("get user info", http.MethodGet, "users/:id"),
	}

	router.Insert("GET", "/test", testHandler)

	// 创建测试请求
	req := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()

	// 执行
	router.ServeHTTP(recorder, req)

	if !called {
		t.Error("Handler was not called")
	}
	if recorder.Code != 200 {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}
}

func TestExtractSegmentNames(t *testing.T) {
	tests := []struct {
		segments []string
		expected []string
	}{
		{
			[]string{"users", ":id", "posts"},
			[]string{"id"},
		},
		{
			[]string{"files", "*"},
			[]string{"path"},
		},
		{
			[]string{"api", "*path"},
			[]string{"path"},
		},
		{
			[]string{"static", "images"},
			[]string{},
		},
	}

	for _, tt := range tests {
		result := extractSegmentNames(tt.segments)
		if len(result) != len(tt.expected) {
			t.Errorf("Expected %v, got %v", tt.expected, result)
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("At index %d: expected %s, got %s", i, tt.expected[i], result[i])
			}
		}
	}
}

// BenchmarkRouterSearchStatic 测试静态路由搜索性能
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		100 routes		8		4293456	       279.7 ns/op	     144 B/op	       3 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		1000 routes		8		 572289	        2138 ns/op	     144 B/op	       3 allocs/op
func BenchmarkRouterSearchStatic(b *testing.B) {
	router := newRouter(nil)

	// 预插入测试路由
	for i := 0; i < 1000; i++ {
		pattern := "/static/path/" + strconv.Itoa(i)
		handler := &handler{}
		router.Insert("GET", pattern, handler)
	}

	// 插入要搜索的路径
	searchPath := "/static/path/500"
	handler := &handler{}
	router.Insert("GET", searchPath, handler)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, _, ok := router.Search("GET", searchPath); !ok {
			b.Errorf("Expected to find route for path %s", searchPath)
		}
	}
}

// BenchmarkRouterSearchParam 测试参数路由搜索性能
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		100 routes		8		 3828267	       314.6 ns/op	     480 B/op	       5 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		1000 routes		8		 3899445	       310.2 ns/op	     480 B/op	       5 allocs/op
func BenchmarkRouterSearchParam(b *testing.B) {
	router := newRouter(nil)

	// 预插入测试路由
	for i := 0; i < 100; i++ {
		pattern := "/api/:version/users/:id"
		handler := &handler{}
		router.Insert("GET", pattern, handler)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		path := "/api/v1/users/" + strconv.Itoa(i%100)
		if _, _, ok := router.Search("GET", path); !ok {
			b.Errorf("Expected to find route for path %s", path)
		}
	}
}

// BenchmarkRouterSearchWildcard 测试通配符路由搜索性能
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		 2888583	       394.7 ns/op	     551 B/op	       6 allocs/op
func BenchmarkRouterSearchWildcard(b *testing.B) {
	router := newRouter(nil)

	// 插入通配符路由
	handler := &handler{}
	router.Insert("GET", "/files/*", handler)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		path := "/files/documents/reports/" + strconv.Itoa(i) + ".pdf"
		if _, _, ok := router.Search("GET", path); !ok {
			b.Errorf("Expected to find route for path %s", path)
		}
	}
}

// BenchmarkRouterMixedSearch 测试混合路由搜索性能
func BenchmarkRouterMixedSearch(b *testing.B) {
	router := newRouter(nil)

	// 插入各种类型的路由
	handler := &handler{}

	// 静态路由
	for i := 0; i < 300; i++ {
		router.Insert("GET", "/static"+strconv.Itoa(i), handler)
	}

	// 参数路由
	for i := 0; i < 300; i++ {
		router.Insert("GET", "/api/:version/resource"+strconv.Itoa(i), handler)
	}

	// 通配符路由
	for i := 0; i < 100; i++ {
		router.Insert("GET", "/wildcard"+strconv.Itoa(i)+"/*", handler)
	}

	// 深度嵌套路由
	router.Insert("GET", "/a/b/c/d/e/f/g/h/i/j/k", handler)

	searchPaths := []string{
		"/static150",
		"/api/v1/resource200",
		"/wildcard50/files/image.jpg",
		"/a/b/c/d/e/f/g/h/i/j/k",
		"/notfound",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		path := searchPaths[i%len(searchPaths)]
		_, _, _ = router.Search("GET", path)
	}
}

// BenchmarkRouterConcurrentInsert 测试并发插入性能
func BenchmarkRouterConcurrentInsert(b *testing.B) {
	router := newRouter(nil)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			pattern := "/concurrent/" + strconv.Itoa(i)
			handler := &handler{}
			router.Insert("GET", pattern, handler)
			i++
		}
	})
}

// BenchmarkRouterConcurrentSearch 测试并发搜索性能
func BenchmarkRouterConcurrentSearch(b *testing.B) {
	router := newRouter(nil)

	// 预填充路由
	handler := &handler{}
	for i := 0; i < 1000; i++ {
		pattern := "/concurrent/path/" + strconv.Itoa(i)
		router.Insert("GET", pattern, handler)
	}

	searchPaths := make([]string, 100)
	for i := 0; i < 100; i++ {
		searchPaths[i] = "/concurrent/path/" + strconv.Itoa(i*10)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := searchPaths[i%len(searchPaths)]
			_, _, _ = router.Search("GET", path)
			i++
		}
	})
}

// BenchmarkRouterServeHTTP 测试完整的HTTP服务性能
func BenchmarkRouterServeHTTP(b *testing.B) {
	router := newRouter(nil)

	// 注册测试处理器
	testHandler := &handler{
		handle: func(ctx *Context) {
			_ = ctx.ResponsePlain(200, "OK")
		},
	}

	// 插入各种路由
	for i := 0; i < 500; i++ {
		router.Insert("GET", "/api/v1/resource/"+strconv.Itoa(i), testHandler)
		router.Insert("POST", "/api/v1/resource/"+strconv.Itoa(i), testHandler)
	}

	// 创建测试请求
	req := httptest.NewRequest("GET", "/api/v1/resource/123", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != 200 {
			b.Errorf("Expected status 200, got %d", recorder.Code)
		}
	}
}

// BenchmarkRouterDifferentMethods 测试不同HTTP方法的性能
func BenchmarkRouterDifferentMethods(b *testing.B) {
	router := newRouter(nil)

	handler := &handler{}
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	// 为每个方法插入路由
	for _, method := range methods {
		for i := 0; i < 200; i++ {
			router.Insert(method, "/"+method+"/resource/"+strconv.Itoa(i), handler)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		method := methods[i%len(methods)]
		path := "/" + strings.ToLower(method) + "/resource/" + strconv.Itoa(i%200)
		_, _, _ = router.Search(method, path)
	}
}

// TestRouterConcurrentSafety 测试路由器的并发安全性
func TestRouterConcurrentSafety(t *testing.T) {
	router := newRouter(nil)

	var wg sync.WaitGroup
	concurrency := 100
	operations := 1000

	// 并发插入
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				pattern := "/goroutine/" + strconv.Itoa(id) + "/item/" + strconv.Itoa(j)
				handler := &handler{}
				router.Insert("GET", pattern, handler)
			}
		}(i)
	}

	// 并发搜索
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				path := "/goroutine/" + strconv.Itoa(id%10) + "/item/" + strconv.Itoa(j%100)
				_, _, _ = router.Search("GET", path)
			}
		}(i)
	}

	wg.Wait()

	// 验证插入结果
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			path := "/goroutine/" + strconv.Itoa(i) + "/item/" + strconv.Itoa(j)
			_, _, found := router.Search("GET", path)
			if !found {
				t.Errorf("Expected to find route: %s", path)
			}
		}
	}
}

// BenchmarkRouterMemoryUsage 测试内存使用情况
func BenchmarkRouterMemoryUsage(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		router := newRouter(nil)

		// 插入大量路由
		handler := &handler{}
		for j := 0; j < 1000; j++ {
			pattern := "/memory/test/" + strconv.Itoa(j) + "/:param"
			router.Insert("GET", pattern, handler)
		}
	}
}

// BenchmarkRouterPathExtraction 测试路径分割性能
func BenchmarkRouterPathExtraction(b *testing.B) {
	paths := []string{
		"/",
		"/users/123/posts/456/comments/789",
		"/api/v1/resource/subresource/deep/nested/path",
		"/static/images/photos/2024/january/01/photo.jpg",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			_ = tree.SplitPattern(path)
		}
	}
}

// BenchmarkLargeScaleRouter 测试大规模路由场景
func BenchmarkLargeScaleRouter(b *testing.B) {
	router := newRouter(nil)

	// 模拟真实场景的大规模路由
	handler := &handler{}

	// 预填充大量路由
	routesCount := 10000
	for i := 0; i < routesCount; i++ {
		// 多种模式混合
		switch i % 4 {
		case 0:
			// 静态路由
			router.Insert("GET", "/static"+strconv.Itoa(i), handler)
		case 1:
			// 参数路由
			router.Insert("GET", "/api/:version/resource"+strconv.Itoa(i), handler)
		case 2:
			// 嵌套路由
			router.Insert("GET", "/a/b/c/"+strconv.Itoa(i), handler)
		case 3:
			// 通配符路由
			router.Insert("GET", "/files"+strconv.Itoa(i)+"/*", handler)
		}
	}

	// 测试搜索性能
	searchPaths := []string{
		"/static5000",
		"/api/v1/resource7500",
		"/a/b/c/2500",
		"/files9999/documents/report.pdf",
		"/not/existing",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		path := searchPaths[i%len(searchPaths)]
		_, _, _ = router.Search("GET", path)
	}
}
