package tree

import (
	"sync"
	"testing"
)

// TestTrieInsertAndSearch 验证静态、参数与通配路由的匹配。
func TestTrieInsertAndSearch(t *testing.T) {
	trie := NewTrie[int]()

	trie.Insert("/", 1)
	trie.Insert("/users", 2)
	trie.Insert("/users/:id", 3)
	trie.Insert("/users/:id/posts", 4)
	trie.Insert("/users/:id/posts/:postId", 5)
	trie.Insert("/static/*filepath", 6)
	trie.Insert("/static/readme", 7)
	trie.Insert("/files/*", 8)

	cases := []struct {
		name       string
		path       string
		wantRoute  int
		wantOK     bool
		wantParams map[string]string
	}{
		{name: "根路径", path: "/", wantRoute: 1, wantOK: true},
		{name: "静态路由", path: "/users", wantRoute: 2, wantOK: true},
		{name: "单参数", path: "/users/42", wantRoute: 3, wantOK: true, wantParams: map[string]string{"id": "42"}},
		{name: "参数加静态", path: "/users/42/posts", wantRoute: 4, wantOK: true, wantParams: map[string]string{"id": "42"}},
		{name: "双参数", path: "/users/42/posts/7", wantRoute: 5, wantOK: true, wantParams: map[string]string{"id": "42", "postId": "7"}},
		{name: "静态优先于通配", path: "/static/readme", wantRoute: 7, wantOK: true},
		{name: "通配兜底", path: "/static/css/app.css", wantRoute: 6, wantOK: true, wantParams: map[string]string{"filepath": "css/app.css"}},
		{name: "默认通配参数名", path: "/files/a/b.txt", wantRoute: 8, wantOK: true, wantParams: map[string]string{WILDCARD_DEFAULT_NAME: "a/b.txt"}},
		{name: "未注册路径", path: "/unknown", wantOK: false},
		{name: "参数段不匹配空路径", path: "/users/", wantRoute: 2, wantOK: true},
		{name: "路径过长", path: "/users/42/posts/7/comments", wantOK: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			route, params, ok := trie.Search(testCase.path)
			if ok != testCase.wantOK {
				t.Fatalf("Search(%q) ok = %v, want %v", testCase.path, ok, testCase.wantOK)
			}

			if !ok {
				if route != 0 {
					t.Fatalf("未命中时 route = %d, want 0", route)
				}

				if params != nil {
					t.Fatalf("未命中时 params = %v, want nil", params)
				}

				return
			}

			if route != testCase.wantRoute {
				t.Fatalf("Search(%q) route = %d, want %d", testCase.path, route, testCase.wantRoute)
			}

			if len(testCase.wantParams) != len(params) {
				t.Fatalf("Search(%q) params = %v, want %v", testCase.path, params, testCase.wantParams)
			}

			for key, value := range testCase.wantParams {
				if params[key] != value {
					t.Fatalf("Search(%q) params[%q] = %q, want %q", testCase.path, key, params[key], value)
				}
			}
		})
	}
}

// TestTrieBacktracking 验证参数分支失败后能回退到静态分支。
func TestTrieBacktracking(t *testing.T) {
	trie := NewTrie[string]()

	// /users/:id/detail 与 /users/me/profile 共存：
	// 访问 /users/me/profile 时，先尝试参数分支（:id=me）会失败，
	// 必须回退到静态分支 users -> me -> profile。
	trie.Insert("/users/:id/detail", "param-detail")
	trie.Insert("/users/me/profile", "static-profile")

	route, params, ok := trie.Search("/users/me/profile")
	if !ok {
		t.Fatal("静态分支必须命中")
	}

	if route != "static-profile" {
		t.Fatalf("route = %q, want static-profile", route)
	}

	if params != nil {
		t.Fatalf("静态命中时 params 必须为 nil，实际为 %v", params)
	}

	route, params, ok = trie.Search("/users/7/detail")
	if !ok {
		t.Fatal("参数分支必须命中")
	}

	if route != "param-detail" {
		t.Fatalf("route = %q, want param-detail", route)
	}

	if params["id"] != "7" {
		t.Fatalf("params[id] = %q, want 7", params["id"])
	}
}

// TestTrieParameterCleanup 验证参数分支失败后不会污染兄弟分支。
func TestTrieParameterCleanup(t *testing.T) {
	trie := NewTrie[string]()
	trie.Insert("/a/:id/left", "left")
	trie.Insert("/a/:id/right", "right")

	route, params, ok := trie.Search("/a/9/right")
	if !ok || route != "right" {
		t.Fatalf("Search = (%q, %v, %v), want (right, true, map[id:9])", route, ok, params)
	}

	if len(params) != 1 || params["id"] != "9" {
		t.Fatalf("params = %v, want map[id:9]", params)
	}
}

// TestTrieInsertPanics 验证注册期冲突的 fail-fast 行为。
func TestTrieInsertPanics(t *testing.T) {
	t.Run("重复注册", func(t *testing.T) {
		trie := NewTrie[int]()
		trie.Insert("/users", 1)

		assertPanics(t, func() { trie.Insert("/users", 2) })
	})

	t.Run("参数名冲突", func(t *testing.T) {
		trie := NewTrie[int]()
		trie.Insert("/users/:id", 1)

		assertPanics(t, func() { trie.Insert("/users/:userId", 2) })
	})

	t.Run("冻结后注册", func(t *testing.T) {
		trie := NewTrie[int]()
		trie.Insert("/users", 1)

		if err := trie.Freeze(); err != nil {
			t.Fatalf("Freeze() error = %v", err)
		}

		assertPanics(t, func() { trie.Insert("/orders", 2) })
	})
}

// TestTrieFreezeIsIdempotent 验证重复冻结不会报错。
func TestTrieFreezeIsIdempotent(t *testing.T) {
	trie := NewTrie[int]()
	trie.Insert("/users", 1)

	for index := 0; index < 3; index++ {
		if err := trie.Freeze(); err != nil {
			t.Fatalf("第 %d 次 Freeze() error = %v", index+1, err)
		}
	}
}

// TestTrieConcurrentSearch 验证冻结后的并发查找安全。
func TestTrieConcurrentSearch(t *testing.T) {
	trie := NewTrie[int]()
	trie.Insert("/users/:id", 1)
	trie.Insert("/static/*filepath", 2)
	trie.Insert("/health", 3)

	if err := trie.Freeze(); err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}

	const workers = 32
	const iterations = 2000

	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for index := 0; index < iterations; index++ {
				if route, _, ok := trie.Search("/users/42"); !ok || route != 1 {
					t.Errorf("Search(/users/42) = (%d, %v), want (1, true)", route, ok)
					return
				}

				if route, _, ok := trie.Search("/static/css/app.css"); !ok || route != 2 {
					t.Errorf("Search(/static/css/app.css) = (%d, %v), want (2, true)", route, ok)
					return
				}

				if _, _, ok := trie.Search("/missing"); ok {
					t.Error("Search(/missing) 必须未命中")
					return
				}
			}
		}()
	}

	waitGroup.Wait()
}

// assertPanics 断言函数 fn 会触发 panic。
func assertPanics(t *testing.T, fn func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatal("期望 panic，但没有触发")
		}
	}()

	fn()
}

// BenchmarkTrieSearchStatic 测量静态路由的查找开销。
func BenchmarkTrieSearchStatic(b *testing.B) {
	trie := NewTrie[int]()
	trie.Insert("/api/v1/users", 1)
	_ = trie.Freeze()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		if _, _, ok := trie.Search("/api/v1/users"); !ok {
			b.Fatal("静态路由未命中")
		}
	}
}

// BenchmarkTrieSearchParameter 测量参数路由的查找开销。
func BenchmarkTrieSearchParameter(b *testing.B) {
	trie := NewTrie[int]()
	trie.Insert("/api/v1/users/:id/posts/:postId", 1)
	_ = trie.Freeze()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		if _, _, ok := trie.Search("/api/v1/users/42/posts/7"); !ok {
			b.Fatal("参数路由未命中")
		}
	}
}

// BenchmarkTrieSearchParallel 测量并发查找的吞吐。
func BenchmarkTrieSearchParallel(b *testing.B) {
	trie := NewTrie[int]()
	trie.Insert("/api/v1/users/:id", 1)
	_ = trie.Freeze()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, _, ok := trie.Search("/api/v1/users/42"); !ok {
				b.Fatal("参数路由未命中")
			}
		}
	})
}
