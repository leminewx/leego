package bench

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/middlewares"
	"gitee.com/leminewx/leego/middlewares/breaker"
	cacheMW "gitee.com/leminewx/leego/middlewares/cache"
	"gitee.com/leminewx/leego/middlewares/compress"
	"gitee.com/leminewx/leego/middlewares/cors"
	"gitee.com/leminewx/leego/middlewares/limiter"
	"gitee.com/leminewx/leego/middlewares/trace"
	"gitee.com/leminewx/leego/session"
)

// BenchmarkMiddlewareLogger 测量访问日志中间件的格式化开销。
//
// 输出指向 io.Discard，只保留路径拼接与格式化成本，不含磁盘 IO。
func BenchmarkMiddlewareLogger(b *testing.B) {
	logger := middlewares.NewLoggerWithConfig(&middlewares.LoggerConfig{Output: io.Discard})
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(logger.AdaptToMiddleware())
		engine.GET("/users/:id", pingHandle, "get-user")
	})

	benchRequest(b, engine, newRequest("/users/42"))
}

// BenchmarkMiddlewareRecovery 测量恢复中间件（无 panic 路径）的包装开销。
func BenchmarkMiddlewareRecovery(b *testing.B) {
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(middlewares.NewRecovery().AdaptToMiddleware())
		engine.GET("/ping", pingHandle)
	})

	benchRequest(b, engine, newRequest("/ping"))
}

// BenchmarkMiddlewareCORS 测量普通跨域请求与预检请求的处理开销。
func BenchmarkMiddlewareCORS(b *testing.B) {
	b.Run("Simple", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.UseMiddlewares(cors.NewDefaultCORS().AdaptToMiddleware())
			engine.GET("/ping", pingHandle)
		})

		request := newRequest("/ping")
		request.Header.Set("Origin", "https://example.com")
		benchRequest(b, engine, request)
	})

	b.Run("Preflight", func(b *testing.B) {
		engine := newTestEngine(func(engine *leego.Engine) {
			engine.UseMiddlewares(cors.NewDefaultCORS().AdaptToMiddleware())
			engine.GET("/ping", pingHandle)
		})

		request := newRequest("/ping")
		request.Method = http.MethodOptions
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		benchRequest(b, engine, request)
	})
}

// BenchmarkMiddlewareLimiter 测量令牌桶放行路径的开销。
func BenchmarkMiddlewareLimiter(b *testing.B) {
	// 配额远大于压测量级，保证始终命中放行分支，不测 429 路径。
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(limiter.NewTokenBucketLimiter(1000000, 1000000).AdaptToMiddleware())
		engine.GET("/ping", pingHandle)
	})

	benchRequest(b, engine, newRequest("/ping"))
}

// BenchmarkMiddlewareBreaker 测量熔断器闭合状态下的放行与统计开销。
func BenchmarkMiddlewareBreaker(b *testing.B) {
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(breaker.NewCircuitBreaker(breaker.NewDefaultConfig()).AdaptToMiddleware())
		engine.GET("/ping", pingHandle)
	})

	benchRequest(b, engine, newRequest("/ping"))
}

// BenchmarkMiddlewareCompress 测量 gzip 压缩中间件对 4 KiB 文本的处理开销。
func BenchmarkMiddlewareCompress(b *testing.B) {
	payload := strings.Repeat("leego performance benchmark payload line. ", 100)
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(compress.NewCompress().AdaptToMiddleware())
		engine.GET("/report", func(ctx *leego.Context) {
			_ = ctx.ResponseText(http.StatusOK, payload)
		})
	})

	request := newRequest("/report")
	request.Header.Set("Accept-Encoding", "gzip")

	b.SetBytes(int64(len(payload)))
	benchRequest(b, engine, request)
}

// BenchmarkMiddlewareCache 测量响应缓存的命中路径。
func BenchmarkMiddlewareCache(b *testing.B) {
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(cacheMW.NewCache().AdaptToMiddleware())
		engine.GET("/catalog", func(ctx *leego.Context) {
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"items": []int{1, 2, 3}})
		})
	})

	// 预热：第一次请求写入缓存，之后的迭代全部命中。
	engine.ServeHTTP(newDiscardWriter(), newRequest("/catalog"))
	benchRequest(b, engine, newRequest("/catalog"))
}

// BenchmarkMiddlewareSession 测量携带有效 Cookie 时的会话读取与写回。
func BenchmarkMiddlewareSession(b *testing.B) {
	store := session.NewMemoryStore()
	b.Cleanup(func() { _ = store.Close() })

	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(session.New(store, session.UseSecret([]byte("bench-secret-0123456789abcdef0123456789"))))
		engine.GET("/login", func(ctx *leego.Context) {
			current, ok := session.GetFromRequest(ctx.Request)
			if !ok {
				ctx.ResponseError(http.StatusInternalServerError, "session missing")
				return
			}

			current.Set("user", "bench")
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"ok": true})
		})
	})

	// 先取一次 Set-Cookie，后续请求复用该会话，避免每次都新建。
	writer := newDiscardWriter()
	engine.ServeHTTP(writer, newRequest("/login"))
	cookie := writer.header.Get("Set-Cookie")
	if cookie == "" {
		b.Fatal("会话中间件未写入 Set-Cookie")
	}

	request := newRequest("/login")
	request.Header.Set("Cookie", strings.SplitN(cookie, ";", 2)[0])
	benchRequest(b, engine, request)
}

// BenchmarkMiddlewareTrace 测量链路追踪中间件（空导出器）的采样与上下文开销。
func BenchmarkMiddlewareTrace(b *testing.B) {
	tracer := trace.NewTracer(trace.NewNoopExporter(), trace.WithoutPropagation())
	b.Cleanup(func() { _ = tracer.Shutdown(context.Background()) })

	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(tracer.AdaptToMiddleware())
		engine.GET("/ping", pingHandle)
	})

	benchRequest(b, engine, newRequest("/ping"))
}

// BenchmarkMiddlewareChain 用真实中间件组合测量常见生产链路。
func BenchmarkMiddlewareChain(b *testing.B) {
	logger := middlewares.NewLoggerWithConfig(&middlewares.LoggerConfig{Output: io.Discard})
	engine := newTestEngine(func(engine *leego.Engine) {
		engine.UseMiddlewares(
			logger.AdaptToMiddleware(),
			middlewares.NewRecovery().AdaptToMiddleware(),
			cors.NewDefaultCORS().AdaptToMiddleware(),
			breaker.NewCircuitBreaker(breaker.NewDefaultConfig()).AdaptToMiddleware(),
		)
		engine.GET("/users/:id", func(ctx *leego.Context) {
			_ = ctx.ResponseJSON(http.StatusOK, leego.JSON{"id": ctx.GetPathValue("id")})
		}, "get-user")
	})

	benchRequest(b, engine, newRequest("/users/42"))
}
