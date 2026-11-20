package bench

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
)

// benchUser 是中大型 JSON 响应的元素类型。
type benchUser struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// newBenchUsers 生成约 6.6 KiB 的 JSON 载荷；子基准名沿用 JSON8KB 表示量级。
func newBenchUsers() []benchUser {
	users := make([]benchUser, 120)
	for index := range users {
		users[index] = benchUser{ID: index, Name: "user-name", Email: "user@example.com"}
	}

	return users
}

// benchBindPayload 是请求体绑定的目标结构。
type benchBindPayload struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// BenchmarkEngineRouting 测量不同路由形态的匹配与分发开销。
func BenchmarkEngineRouting(b *testing.B) {
	b.Run("Static", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/users", pingHandle, "list-users")
		})
		benchRequest(b, engine, newRequest("/users"))
	})

	b.Run("Param", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/users/:id", func(ctx *leego.Context) {
				_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("id"))
			}, "get-user")
		})
		benchRequest(b, engine, newRequest("/users/42"))
	})

	b.Run("Wildcard", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/files/*path", func(ctx *leego.Context) {
				_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("path"))
			}, "get-file")
		})
		benchRequest(b, engine, newRequest("/files/docs/guide/readme.txt"))
	})

	b.Run("NotFound", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/users", pingHandle)
		})
		benchRequest(b, engine, newRequest("/missing"))
	})

	// 路由表规模是 trie 的典型变量：注册 1000 条静态路由后访问最后一条。
	b.Run("Table1000", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			for index := 0; index < 1000; index++ {
				engine.GET("/api/v1/item/"+strconv.Itoa(index), pingHandle)
			}
		})
		benchRequest(b, engine, newRequest("/api/v1/item/999"))
	})

	// 两级分组 + 组级中间件，模拟真实项目的路由组织方式。
	b.Run("GroupNested", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			api := engine.NewGroup("/api")
			api.UseMiddlewares(noopMiddleware)
			v1 := api.NewGroup("/v1")
			v1.GET("/users/:id", func(ctx *leego.Context) {
				_ = ctx.ResponseText(http.StatusOK, ctx.GetPathValue("id"))
			}, "get-user")
		})
		benchRequest(b, engine, newRequest("/api/v1/users/42"))
	})
}

// BenchmarkEngineMiddlewares 测量中间件链的线性开销。
func BenchmarkEngineMiddlewares(b *testing.B) {
	for _, count := range []int{0, 1, 4} {
		b.Run(fmt.Sprintf("Chain%d", count), func(b *testing.B) {
			engine := newTestEngine(func(engine *leego.Engine) {
				chain := make([]leego.MiddlewareFunc, 0, count)
				for index := 0; index < count; index++ {
					chain = append(chain, noopMiddleware)
				}

				engine.UseMiddlewares(chain...)
				engine.GET("/ping", pingHandle)
			})
			benchRequest(b, engine, newRequest("/ping"))
		})
	}
}

// BenchmarkEngineResponse 测量响应序列化开销。
func BenchmarkEngineResponse(b *testing.B) {
	b.Run("JSONSmall", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/ping", func(ctx *leego.Context) {
				_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"message": "pong"})
			})
		})
		benchRequest(b, engine, newRequest("/ping"))
	})

	b.Run("JSON8KB", func(b *testing.B) {
		users := newBenchUsers()
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/users", func(ctx *leego.Context) {
				_ = ctx.ResponseJSON(http.StatusOK, users)
			})
		})
		benchRequest(b, engine, newRequest("/users"))
	})

	b.Run("Text", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/ping", pingHandle)
		})
		benchRequest(b, engine, newRequest("/ping"))
	})
}

// BenchmarkEngineRequest 测量请求侧解析开销。
func BenchmarkEngineRequest(b *testing.B) {
	b.Run("ReadBody8KB", func(b *testing.B) {
		body := []byte(strings.Repeat("a", 8<<10))
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.POST("/upload", func(ctx *leego.Context) {
				data, err := ctx.ReadBody()
				if err != nil {
					ctx.ResponseError(http.StatusBadRequest, err.Error())
					return
				}

				_ = ctx.ResponseText(http.StatusOK, strconv.Itoa(len(data)))
			})
		})

		benchRequestEach(b, engine, func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body))
		})
	})

	b.Run("BindJSON1KB", func(b *testing.B) {
		payload := []byte(`{"name":"` + strings.Repeat("a", 900) + `","email":"user@example.com","age":30}`)
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.POST("/users", func(ctx *leego.Context) {
				var target benchBindPayload
				if err := ctx.BindJSON(&target); err != nil {
					ctx.ResponseError(http.StatusBadRequest, err.Error())
					return
				}

				_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"name": target.Name})
			})
		})

		benchRequestEach(b, engine, func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(payload))
		})
	})

	b.Run("HeadersCookieIP", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.GET("/context", func(ctx *leego.Context) {
				_ = ctx.GetRequestHeader("X-Request-Id")
				ctx.SetResponseHeader("X-Served-By", "bench")
				ctx.SetCookieData("session", "abcdef", 3600, "/", "", false, true)
				_ = ctx.GetClientIP()
				_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"ok": true})
			})
		})

		request := newRequest("/context")
		request.Header.Set("X-Request-Id", "0123456789abcdef")
		benchRequest(b, engine, request)
	})
}

// BenchmarkEngineParallel 测量多协程并发处理同一路由的吞吐。
func BenchmarkEngineParallel(b *testing.B) {
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.GET("/users/:id", func(ctx *leego.Context) {
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": ctx.GetPathValue("id")})
		})
	})

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(probe *testing.PB) {
		writer := newDiscardWriter()
		request := newRequest("/users/42")
		for probe.Next() {
			writer.reset()
			engine.ServeHTTP(writer, request)
		}
	})
}
