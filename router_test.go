package leego

import (
	"net/http"
	"strconv"
	"sync"
	"testing"

	"gitee.com/leminewx/leego/tree"
)

// TestRouterInsertAndSearch 验证路由插入与参数提取。
func TestRouterInsertAndSearch(t *testing.T) {
	router := newRouter(nil)

	handler := &handler{}
	router.Insert(http.MethodGet, "/users/:id", handler)

	found, params, ok := router.Search(http.MethodGet, "/users/123")
	if !ok || found == nil {
		t.Fatal("已注册路由必须能匹配")
	}

	if params["id"] != "123" {
		t.Fatalf("路径参数 = %q, want 123", params["id"])
	}

	if _, _, ok = router.Search(http.MethodPost, "/users/123"); ok {
		t.Fatal("未注册的方法不应匹配")
	}

	if _, _, ok = router.Search(http.MethodGet, "/missing"); ok {
		t.Fatal("未注册的路径不应匹配")
	}
}

// TestRouterHeadFallsBackToGet 验证 HEAD 回退到 GET。
func TestRouterHeadFallsBackToGet(t *testing.T) {
	router := newRouter(nil)
	router.Insert(http.MethodGet, "/resource", &handler{})

	if _, _, ok := router.Search(http.MethodHead, "/resource"); !ok {
		t.Fatal("HEAD 未注册时必须回退到 GET")
	}

	// HEAD 已注册时优先使用自身路由。
	headHandler := &handler{}
	router.Insert(http.MethodHead, "/resource", headHandler)

	found, _, ok := router.Search(http.MethodHead, "/resource")
	if !ok || found != headHandler {
		t.Fatal("HEAD 已注册时必须返回 HEAD 处理器")
	}
}

// TestRouterCollectAllowedMethods 验证 Allow 方法收集。
func TestRouterCollectAllowedMethods(t *testing.T) {
	router := newRouter(nil)
	router.Insert(http.MethodPost, "/resource", &handler{})
	router.Insert(http.MethodGet, "/resource", &handler{})

	methods := router.CollectAllowedMethods("/resource")
	want := []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost}
	if len(methods) != len(want) {
		t.Fatalf("Allow 方法 = %v, want %v", methods, want)
	}

	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("Allow 方法 = %v, want %v（必须按字母序）", methods, want)
		}
	}

	if unknown := router.CollectAllowedMethods("/missing"); len(unknown) != 0 {
		t.Fatalf("未知路径的 Allow 方法 = %v, want 空", unknown)
	}
}

// TestRouterBlacklist 验证黑名单路径规范化与查询。
func TestRouterBlacklist(t *testing.T) {
	router := newRouter(nil)
	router.AddBlacklist(" /secret ", "", "/admin")

	if !router.IsBlacklisted("/secret") {
		t.Fatal("黑名单路径必须去除首尾空白")
	}

	if !router.IsBlacklisted("/admin") {
		t.Fatal("黑名单必须支持多个路径")
	}

	if router.IsBlacklisted("") || router.IsBlacklisted("/public") {
		t.Fatal("空路径与未加入的路径不应被拉黑")
	}
}

// TestRouterFreeze 验证冻结的幂等性与写入保护。
func TestRouterFreeze(t *testing.T) {
	router := newRouter(nil)
	router.Insert(http.MethodGet, "/ready", &handler{})

	router.Freeze()
	router.Freeze()

	if _, _, ok := router.Search(http.MethodGet, "/ready"); !ok {
		t.Fatal("冻结后仍必须能查询已有路由")
	}

	assertPanics(t, "冻结后插入路由", func() {
		router.Insert(http.MethodGet, "/late", &handler{})
	})
}

// TestExtractSegmentNames 验证参数名提取。
func TestExtractSegmentNames(t *testing.T) {
	tests := []struct {
		segments []string
		want     []string
	}{
		{[]string{"users", ":id", "posts"}, []string{"id"}},
		{[]string{"files", "*"}, []string{tree.WILDCARD_DEFAULT_NAME}},
		{[]string{"api", "*path"}, []string{"path"}},
		{[]string{"static", "images"}, []string{}},
		{nil, []string{}},
	}

	for _, test := range tests {
		got := extractSegmentNames(test.segments)
		if len(got) != len(test.want) {
			t.Fatalf("extractSegmentNames(%v) = %v, want %v", test.segments, got, test.want)
		}

		for index := range test.want {
			if got[index] != test.want[index] {
				t.Fatalf("extractSegmentNames(%v)[%d] = %q, want %q", test.segments, index, got[index], test.want[index])
			}
		}
	}
}

// TestRouterConcurrentSearch 验证冻结后的并发查询安全。
func TestRouterConcurrentSearch(t *testing.T) {
	const workers = 32
	const attemptsPerWorker = 500

	router := newRouter(nil)
	for index := 0; index < 100; index++ {
		router.Insert(http.MethodGet, "/items/"+strconv.Itoa(index), &handler{})
	}

	router.Freeze()

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func(id int) {
			defer waitGroup.Done()

			for attempt := 0; attempt < attemptsPerWorker; attempt++ {
				path := "/items/" + strconv.Itoa((id+attempt)%100)
				if _, _, ok := router.Search(http.MethodGet, path); !ok {
					t.Errorf("并发查询未命中 %s", path)
					return
				}
			}
		}(worker)
	}

	waitGroup.Wait()
}

// BenchmarkRouterSearchStatic 测量静态路由查询开销。
func BenchmarkRouterSearchStatic(b *testing.B) {
	router := newRouter(nil)
	shared := &handler{}

	for index := 0; index < 1000; index++ {
		router.Insert(http.MethodGet, "/static/path/"+strconv.Itoa(index), shared)
	}

	router.Freeze()
	path := "/static/path/500"

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		if _, _, ok := router.Search(http.MethodGet, path); !ok {
			b.Fatalf("未命中 %s", path)
		}
	}
}

// BenchmarkRouterSearchParam 测量参数路由查询开销。
func BenchmarkRouterSearchParam(b *testing.B) {
	router := newRouter(nil)
	shared := &handler{}

	router.Insert(http.MethodGet, "/api/:version/users/:id", shared)
	router.Freeze()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		path := "/api/v1/users/" + strconv.Itoa(index%100)
		if _, _, ok := router.Search(http.MethodGet, path); !ok {
			b.Fatalf("未命中 %s", path)
		}
	}
}

// BenchmarkRouterSearchWildcard 测量通配符路由查询开销。
func BenchmarkRouterSearchWildcard(b *testing.B) {
	router := newRouter(nil)
	router.Insert(http.MethodGet, "/files/*", &handler{})
	router.Freeze()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		path := "/files/documents/reports/" + strconv.Itoa(index) + ".pdf"
		if _, _, ok := router.Search(http.MethodGet, path); !ok {
			b.Fatalf("未命中 %s", path)
		}
	}
}

// BenchmarkRouterParallelSearch 测量并发查询吞吐。
func BenchmarkRouterParallelSearch(b *testing.B) {
	router := newRouter(nil)
	shared := &handler{}

	for index := 0; index < 1000; index++ {
		router.Insert(http.MethodGet, "/concurrent/path/"+strconv.Itoa(index), shared)
	}

	router.Freeze()
	paths := make([]string, 100)
	for index := range paths {
		paths[index] = "/concurrent/path/" + strconv.Itoa(index*10)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		index := 0
		for pb.Next() {
			_, _, _ = router.Search(http.MethodGet, paths[index%len(paths)])
			index++
		}
	})
}
