package tree

import (
	"strconv"
	"testing"
)

func TestTrie(t *testing.T) {
	trie := NewTrie[string]()
	trie.Insert("/", "root")
	trie.Insert("/users", "users")
	trie.Insert("/users/:id", "user_detail")
	trie.Insert("/users/:id/posts", "user_posts")
	trie.Insert("/files/*", "file_server")
	trie.Insert("/api/v1/:service/*", "api_handler")

	for _, pattern := range []string{
		"/",                           // 静态路径
		"/users",                      // 静态路径
		"/users/test_uer",             // 参数路径
		"/users/test_uer/posts",       // 参数路径
		"/files/*",                    // 通配符路径
		"/api/v1/tasks/tid1234567890", // 参数路径
	} {
		route, params, found := trie.Search(pattern)
		if !found {
			t.Logf("not found %s", pattern)
		} else {
			t.Logf("found %s %v", route, params)
		}
	}
}

// TestTrieBacktracking 测试匹配失败后能否回溯到其他兄弟分支
func TestTrieBacktracking(t *testing.T) {
	t.Run("静态分支失败后回退到参数分支", func(t *testing.T) {
		trie := NewTrie[string]()
		// /users/new/settings 只有静态分支，/users/:id/profile 只有参数分支
		trie.Insert("/users/new/settings", "new_settings")
		trie.Insert("/users/:id/profile", "profile")

		// "new" 会先命中静态节点 users/new，但其下没有 profile，
		// 必须回溯到 :id 分支才能匹配成功
		route, params, found := trie.Search("/users/new/profile")
		if !found {
			t.Fatalf("期望回溯匹配到 /users/:id/profile，实际未找到")
		}
		if route != "profile" {
			t.Errorf("route = %v, want profile", route)
		}
		if params["id"] != "new" {
			t.Errorf("params[id] = %v, want new", params["id"])
		}
	})

	t.Run("通配符非路由节点不应吞掉匹配", func(t *testing.T) {
		trie := NewTrie[string]()
		// 通配符节点本身不是路由，只有它的子节点 meta 才是
		trie.Insert("/assets/*/meta", "meta")

		// /assets/logo.png 无法匹配到任何已注册路由，必须返回未找到，
		// 且不能错误地把非路由的通配符节点当成结果返回
		route, _, found := trie.Search("/assets/logo.png")
		if found {
			t.Errorf("不应匹配非路由的通配符节点，却得到 route=%v", route)
		}
	})

	t.Run("回溯后不残留失败分支的参数", func(t *testing.T) {
		trie := NewTrie[string]()
		// 让失败分支比成功分支先被尝试：静态 files 优先于参数 :x
		trie.Insert("/a/files/:fid/b", "fb")
		trie.Insert("/a/:x/:y/c", "axyc")

		_, params, found := trie.Search("/a/files/9/c")
		if !found {
			t.Fatalf("期望匹配到 /a/:x/:y/c，实际未找到")
		}
		// fid 属于先失败的 /a/files/:fid/b 分支，不应残留在最终结果里
		if _, leaked := params["fid"]; leaked {
			t.Errorf("失败分支的参数 fid 泄漏到结果中: %v", params)
		}
		if params["x"] != "files" || params["y"] != "9" {
			t.Errorf("params = %v, want x=files y=9", params)
		}
	})
}

// TestTrieInsertAndSearch 测试插入和搜索功能
func TestTrieInsertAndSearch(t *testing.T) {
	trie := NewTrie[string]()

	// 测试数据
	routes := map[string]string{
		"/":                  "root",
		"/users":             "users",
		"/users/:id":         "user_detail",
		"/users/:id/posts":   "user_posts",
		"/files/*":           "file_server",
		"/api/v1/:service/*": "api_handler",
	}

	// 插入路由
	for pattern, handler := range routes {
		trie.Insert(pattern, handler)
	}

	// 测试用例
	tests := []struct {
		path     string
		expected string
		params   map[string]string
		found    bool
	}{
		{
			path:     "/",
			expected: "root",
			params:   map[string]string{},
			found:    true,
		},
		{
			path:     "/users",
			expected: "users",
			params:   map[string]string{},
			found:    true,
		},
		{
			path:     "/users/123",
			expected: "user_detail",
			params:   map[string]string{"id": "123"},
			found:    true,
		},
		{
			path:     "/users/456/posts",
			expected: "user_posts",
			params:   map[string]string{"id": "456"},
			found:    true,
		},
		{
			path:     "/files/images/photo.jpg",
			expected: "file_server",
			params:   map[string]string{WILDCARD_DEFAULT_NAME: "images/photo.jpg"},
			found:    true,
		},
		{
			path:     "/api/v1/users/profile",
			expected: "api_handler",
			params:   map[string]string{"service": "users", WILDCARD_DEFAULT_NAME: "profile"},
			found:    true,
		},
		{
			path:     "/notfound",
			expected: "",
			params:   map[string]string{},
			found:    false,
		},
		{
			path:     "/users/123/invalid",
			expected: "",
			params:   map[string]string{},
			found:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			handler, params, found := trie.Search(tt.path)

			if found != tt.found {
				t.Errorf("Search(%s) found = %v, want %v", tt.path, found, tt.found)
				return
			}

			if !found {
				return
			}

			if handler != tt.expected {
				t.Errorf("Search(%s) handler = %v, want %v", tt.path, handler, tt.expected)
			}

			if len(params) != len(tt.params) {
				t.Errorf("Search(%s) params length = %v, want %v", tt.path, len(params), len(tt.params))
			}

			for key, expectedValue := range tt.params {
				actualValue, exists := params[key]
				if !exists || actualValue != expectedValue {
					t.Errorf("Search(%s) param[%s] = %v, want %v", tt.path, key, actualValue, expectedValue)
				}
			}
		})
	}
}

// TestTriePriority 测试路由优先级
func TestTriePriority(t *testing.T) {
	trie := NewTrie[string]()

	// 插入路由，故意打乱顺序
	trie.Insert("/users/:id", "param_route")
	trie.Insert("/users/static", "static_route")
	trie.Insert("/users/*", "wildcard_route")

	tests := []struct {
		path     string
		expected string
	}{
		{"/users/static", "static_route"},          // 静态路径优先
		{"/users/123", "param_route"},              // 参数路径次之
		{"/users/anything/else", "wildcard_route"}, // 通配符最后
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			handler, _, found := trie.Search(tt.path)
			if !found {
				t.Errorf("Search(%s) not found", tt.path)
				return
			}
			if handler != tt.expected {
				t.Errorf("Search(%s) = %v, want %v", tt.path, handler, tt.expected)
			}
		})
	}
}

// TestTrieEdgeCases 测试边界情况
func TestTrieEdgeCases(t *testing.T) {
	trie := NewTrie[string]()

	t.Run("空路径", func(t *testing.T) {
		trie.Insert("", "empty_handler")
		handler, _, found := trie.Search("")
		if !found || handler != "empty_handler" {
			t.Errorf("Empty path search failed")
		}
	})

	t.Run("只有通配符的路径", func(t *testing.T) {
		trie.Insert("*", "catch_all")
		handler, params, found := trie.Search("any/path")
		if !found || handler != "catch_all" {
			t.Errorf("Catch-all search failed")
		}
		if params[WILDCARD_DEFAULT_NAME] != "any/path" {
			t.Errorf("Catch-all param incorrect")
		}
	})

	t.Run("多重参数", func(t *testing.T) {
		trie.Insert("/:a/:b/:c", "multi_param")
		_, params, found := trie.Search("/1/2/3")
		if !found {
			t.Errorf("Multi-param search failed")
		}
		if params["a"] != "1" || params["b"] != "2" || params["c"] != "3" {
			t.Errorf("Multi-param values incorrect: %v", params)
		}
	})
}

// TestTrieComplexPatterns 测试复杂模式
func TestTrieComplexPatterns(t *testing.T) {
	trie := NewTrie[int]()

	patterns := map[string]int{
		"/api/:version/users/:id":       1,
		"/api/:version/posts/:postId":   2,
		"/api/:version/comments/:cid/*": 3,
		"/static/*":                     4,
		"/:lang/users/:id":              5,
	}

	for pattern, value := range patterns {
		trie.Insert(pattern, value)
	}

	tests := []struct {
		path     string
		expected int
		params   map[string]string
	}{
		{
			path:     "/api/v1/users/123",
			expected: 1,
			params:   map[string]string{"version": "v1", "id": "123"},
		},
		{
			path:     "/api/v2/posts/456",
			expected: 2,
			params:   map[string]string{"version": "v2", "postId": "456"},
		},
		{
			path:     "/api/v1/comments/789/replies/123",
			expected: 3,
			params:   map[string]string{"version": "v1", "cid": "789", WILDCARD_DEFAULT_NAME: "replies/123"},
		},
		{
			path:     "/static/css/style.css",
			expected: 4,
			params:   map[string]string{WILDCARD_DEFAULT_NAME: "css/style.css"},
		},
		{
			path:     "/en/users/999",
			expected: 5,
			params:   map[string]string{"lang": "en", "id": "999"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			value, params, found := trie.Search(tt.path)
			if !found {
				t.Errorf("Search(%s) not found", tt.path)
				return
			}
			if value != tt.expected {
				t.Errorf("Search(%s) = %v, want %v", tt.path, value, tt.expected)
			}
			for key, expectedValue := range tt.params {
				if params[key] != expectedValue {
					t.Errorf("Param[%s] = %v, want %v", key, params[key], expectedValue)
				}
			}
		})
	}
}

// BenchmarkTrieInsert 测试插入性能
//cpu: Intel(R) Core(TM) i5-14600KF							static paths				20		 4053619	       298.9 ns/op	     280 B/op	      15 allocs/op
//cpu: Intel(R) Core(TM) i5-14600KF							dynamic paths				20		 5551840	       215.0 ns/op	     296 B/op	       9 allocs/op
//cpu: Intel(R) Core(TM) i5-14600KF							complex dynamic paths		20		    4602	    254316 ns/op	  154917 B/op	    3900 allocs/op

// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		static paths				8		 2314869	       525.2 ns/op	     280 B/op	      15 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		dynamic paths				8		 3056307	       408.4 ns/op	     296 B/op	       9 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		complex dynamic paths		8		    2916	      404929 ns/op	  154937 B/op	    3901 allocs/op
func BenchmarkTrieInsert(b *testing.B) {
	b.Run("简单路径插入", func(b *testing.B) {
		trie := NewTrie[string]()
		patterns := []string{"/", "/users", "/posts", "/comments", "/api/v1"}
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			for _, pattern := range patterns {
				trie.Insert(pattern, "handler_"+pattern)
			}
		}
	})

	b.Run("带参数路径插入", func(b *testing.B) {
		trie := NewTrie[string]()
		patterns := []string{"/users/:id", "/posts/:postId", "/:lang/users/:id"}
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			for _, pattern := range patterns {
				trie.Insert(pattern, "handler_"+pattern)
			}
		}
	})

	b.Run("大规模路径插入", func(b *testing.B) {
		trie := NewTrie[int]()
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// 每轮插入1000个路由
			for j := 0; j < 1000; j++ {
				pattern := "/api/v" + strconv.Itoa(j%10) + "/resource/" + strconv.Itoa(j)
				trie.Insert(pattern, j)
			}
		}
	})
}

// BenchmarkTrieSearch 测试搜索性能
// cpu: Intel(R) Core(TM) i5-14600KF						static paths				20		17367970	       65.27 ns/op	     104 B/op	       3 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF						dynamic param paths			20		 8658120	       125.9 ns/op	     330 B/op	       4 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF						dynamic wildcard paths		20		 7297352	       165.0 ns/op	     448 B/op	       5 allocs/op
// cpu: Intel(R) Core(TM) i5-14600KF						not found paths				10		12672865	       95.43 ns/op	     213 B/op	       3 allocs/op
//
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		static paths				8		10413692	       117.4 ns/op	     104 B/op	       3 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		dynamic param paths			8		 5726457	       211.4 ns/op	     330 B/op	       4 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		dynamic wildcard paths		8		 4468950	       269.0 ns/op	     448 B/op	       5 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		not found paths				8		 7396630	       161.6 ns/op	     213 B/op	       3 allocs/op
func BenchmarkTrieSearch(b *testing.B) {
	// 准备测试数据
	trie := prepareTestTrie(1000)

	b.Run("静态路径搜索", func(b *testing.B) {
		paths := []string{"/", "/users", "/api/v1", "/deep/nested/path"}
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			trie.Search(path)
		}
	})

	b.Run("参数路径搜索", func(b *testing.B) {
		paths := []string{"/users/123", "/posts/456", "/api/v1/resource/789"}
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			trie.Search(path)
		}
	})

	b.Run("通配符路径搜索", func(b *testing.B) {
		paths := []string{"/files/docs/report.pdf", "/static/css/main.css"}
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			trie.Search(path)
		}
	})

	b.Run("未找到路径搜索", func(b *testing.B) {
		paths := []string{"/notfound", "/invalid/path", "/api/v999/resource"}
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			trie.Search(path)
		}
	})
}

// BenchmarkTrieConcurrent 测试并发性能
// cpu: Intel(R) Core(TM) i5-14600KF						8		 21651288	       53.05 ns/op	     340 B/op	       4 allocs/op
// cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		 10900864	       112.6 ns/op	     340 B/op	       4 allocs/op
func BenchmarkTrieConcurrent(b *testing.B) {
	trie := prepareTestTrie(500)

	b.Run("并发搜索", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			paths := []string{"/users/123", "/api/v1/resource/456", "/static/file.txt", "/notfound"}
			i := 0
			for pb.Next() {
				path := paths[i%len(paths)]
				trie.Search(path)
				i++
			}
		})
	})
}

// 准备测试用的Trie树
func prepareTestTrie(size int) *Trie[string] {
	trie := NewTrie[string]()

	// 插入静态路由
	trie.Insert("/", "root")
	trie.Insert("/users", "users")
	trie.Insert("/api/v1", "api_v1")
	trie.Insert("/deep/nested/path", "deep_path")

	// 插入参数路由
	for i := 0; i < size/2; i++ {
		pattern := "/users/:id" + strconv.Itoa(i)
		trie.Insert(pattern, "user_handler_"+strconv.Itoa(i))
	}

	// 插入通配符路由
	trie.Insert("/files/*", "file_server")
	trie.Insert("/static/*", "static_server")
	trie.Insert("/api/*", "api_catch_all")

	// 插入复杂模式
	for i := 0; i < size/4; i++ {
		pattern := "/api/v:version/resource/:id"
		trie.Insert(pattern, "api_resource_"+strconv.Itoa(i))
	}

	return trie
}

func BenchmarkTrie(b *testing.B) {
	trie := NewTrie[string]()
	trie.Insert("/user/:id", "user")
	trie.Insert("/static/*filepath", "static")
	trie.Insert("/api/v1/posts", "posts")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		trie.Search("/user/123")
		trie.Search("/static/css/style.css")
		trie.Search("/api/v1/posts")
	}
}
