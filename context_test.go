package leego

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// cpu: QEMU Virtual CPU version 2.5+		2   	8573836	       137.5 ns/op	     136 B/op	       2 allocs/op
func BenchmarkContext_NewContext(b *testing.B) {
	engine := NewEngine()

	b.Run("使用sync.Pool", func(b *testing.B) {
		resp := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/users/123", nil)

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			_ = newContext(engine, resp, req, nil)
		}
	})
}

// cpu: QEMU Virtual CPU version 2.5+		2   	591463	      2959 ns/op	    2520 B/op	      24 allocs/op
func BenchmarkContext_NewContext_Concurrent(b *testing.B) {
	engine := NewEngine()
	router := engine.Group("/api")
	router.GET("/users/:id", func(ctx *Context) {
		// 模拟真实处理逻辑
		id := ctx.GetPathValue("id")
		ctx.ResponseJSON(200, map[string]string{"id": id})
	})

	b.Run("并发压测", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/api/users/123", nil)
				engine.ServeHTTP(resp, req)
			}
		})
	})
}
