package cache_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/harness/testutil"
	"gitee.com/leminewx/leego/middlewares/cache"
)

// counter 记录处理器被执行的次数。
type counter struct {
	value atomic.Int64
}

// hit 返回当前计数。
func (own *counter) hit() int64 {
	return own.value.Load()
}

// newEngine 构造带计数的缓存引擎。
func newEngine(config ...cache.Config) (*leego.Engine, *counter) {
	count := &counter{}
	engine := leego.NewEngine()
	engine.UseMiddlewares(cache.NewCache(config...).AdaptToMiddleware())

	engine.GET("/data", func(ctx *leego.Context) {
		count.value.Add(1)
		_ = ctx.ResponseJSON(http.StatusOK, map[string]string{"name": "leego"})
	})

	engine.POST("/data", func(ctx *leego.Context) {
		count.value.Add(1)
		_ = ctx.ResponseJSON(http.StatusOK, map[string]string{"name": "posted"})
	})

	engine.GET("/session", func(ctx *leego.Context) {
		count.value.Add(1)
		ctx.SetResponseHeader("Set-Cookie", "session=abc; Path=/")
		_ = ctx.ResponseText(http.StatusOK, "session")
	})

	engine.GET("/private", func(ctx *leego.Context) {
		count.value.Add(1)
		ctx.SetResponseHeader("Cache-Control", "no-store")
		_ = ctx.ResponseText(http.StatusOK, "private")
	})

	engine.GET("/large", func(ctx *leego.Context) {
		count.value.Add(1)
		_ = ctx.ResponseText(http.StatusOK, strings.Repeat("leego", 100))
	})

	return engine, count
}

func TestCacheHitAndMiss(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	first := client.Get("/data")
	first.AssertStatus(t, http.StatusOK)
	first.AssertHeader(t, "X-Cache", "MISS")
	first.AssertContains(t, "leego")

	second := client.Get("/data")
	second.AssertStatus(t, http.StatusOK)
	second.AssertHeader(t, "X-Cache", "HIT")
	second.AssertContains(t, "leego")

	if count.hit() != 1 {
		t.Fatalf("处理器执行次数 = %d, want 1", count.hit())
	}
}

func TestCacheVaryByAuthorization(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	client.Get("/data", testutil.WithHeader("Authorization", "Bearer alice")).AssertHeader(t, "X-Cache", "MISS")
	client.Get("/data", testutil.WithHeader("Authorization", "Bearer bob")).AssertHeader(t, "X-Cache", "MISS")
	client.Get("/data", testutil.WithHeader("Authorization", "Bearer alice")).AssertHeader(t, "X-Cache", "HIT")

	if count.hit() != 2 {
		t.Fatalf("处理器执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheSkipsCookieRequestsByDefault(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	// Cookie 代表会话身份，且不在默认 Vary 中：默认旁路，避免越权命中。
	first := client.Get("/data", testutil.WithHeader("Cookie", "session=abc"))
	first.AssertStatus(t, http.StatusOK)
	if tag := first.Header.Get("X-Cache"); tag != "" {
		t.Fatalf("带 Cookie 的请求应旁路缓存, X-Cache = %q", tag)
	}

	second := client.Get("/data", testutil.WithHeader("Cookie", "session=abc"))
	second.AssertStatus(t, http.StatusOK)
	if tag := second.Header.Get("X-Cache"); tag == "HIT" || tag == "STALE" {
		t.Fatalf("带 Cookie 的请求不应命中缓存, X-Cache = %q", tag)
	}

	if count.hit() != 2 {
		t.Fatalf("执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheAllowsCookieWhenEnabled(t *testing.T) {
	engine, count := newEngine(cache.Config{AllowCookieCaching: true})
	client := testutil.NewClient(engine)

	client.Get("/data", testutil.WithHeader("Cookie", "session=abc")).AssertHeader(t, "X-Cache", "MISS")
	client.Get("/data", testutil.WithHeader("Cookie", "session=abc")).AssertHeader(t, "X-Cache", "HIT")

	if count.hit() != 1 {
		t.Fatalf("执行次数 = %d, want 1", count.hit())
	}
}

func TestCacheSkipsUncacheableMethods(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	// POST 不在可缓存方法内，中间件应完全旁路：既不写 X-Cache 也不缓存。
	first := client.Post("/data", []byte("{}"))
	if first.Header.Get("X-Cache") != "" {
		t.Fatalf("POST 必须旁路缓存, X-Cache = %q", first.Header.Get("X-Cache"))
	}

	client.Post("/data", []byte("{}"))

	if count.hit() != 2 {
		t.Fatalf("POST 不应命中缓存, 执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheSkipsSetCookie(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	client.Get("/session").AssertStatus(t, http.StatusOK)
	second := client.Get("/session")
	second.AssertStatus(t, http.StatusOK)

	if second.Header.Get("X-Cache") == "HIT" {
		t.Fatal("带 Set-Cookie 的响应不应被缓存")
	}

	if count.hit() != 2 {
		t.Fatalf("执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheSkipsCacheControlNoStore(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	client.Get("/private").AssertStatus(t, http.StatusOK)
	second := client.Get("/private")

	if second.Header.Get("X-Cache") == "HIT" {
		t.Fatal("no-store 的响应不应被缓存")
	}

	if count.hit() != 2 {
		t.Fatalf("执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheSkipsOversizedBody(t *testing.T) {
	// MaxBody = 8 字节，/large 必然超出，只允许直写。
	engine, count := newEngine(cache.Config{MaxBody: 8})
	client := testutil.NewClient(engine)

	client.Get("/large").AssertStatus(t, http.StatusOK)
	second := client.Get("/large")

	if second.Header.Get("X-Cache") == "HIT" {
		t.Fatal("超过 MaxBody 的响应不应被缓存")
	}

	if count.hit() != 2 {
		t.Fatalf("执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheServesHeadFromGet(t *testing.T) {
	engine, count := newEngine()
	client := testutil.NewClient(engine)

	client.Get("/data").AssertStatus(t, http.StatusOK)

	head := client.Head("/data")
	head.AssertStatus(t, http.StatusOK)
	head.AssertHeader(t, "X-Cache", "HIT")

	if len(head.Body) != 0 {
		t.Fatalf("HEAD 响应体长度 = %d, want 0", len(head.Body))
	}

	if head.Header.Get("Content-Length") == "" {
		t.Fatal("HEAD 命中缓存时应补全 Content-Length")
	}

	if count.hit() != 1 {
		t.Fatalf("处理器执行次数 = %d, want 1", count.hit())
	}
}

func TestCacheExpiresAfterTTL(t *testing.T) {
	engine, count := newEngine(cache.Config{TTL: 20 * time.Millisecond})
	client := testutil.NewClient(engine)

	client.Get("/data").AssertHeader(t, "X-Cache", "MISS")
	time.Sleep(40 * time.Millisecond)

	second := client.Get("/data")
	second.AssertHeader(t, "X-Cache", "MISS")

	if count.hit() != 2 {
		t.Fatalf("执行次数 = %d, want 2", count.hit())
	}
}

func TestCacheServesStaleAndRefreshes(t *testing.T) {
	engine, count := newEngine(cache.Config{
		TTL:   10 * time.Millisecond,
		Stale: 2 * time.Second,
	})
	client := testutil.NewClient(engine)

	client.Get("/data").AssertHeader(t, "X-Cache", "MISS")
	time.Sleep(30 * time.Millisecond)

	stale := client.Get("/data")
	stale.AssertHeader(t, "X-Cache", "STALE")
	stale.AssertContains(t, "leego")

	// 后台刷新完成后应重新变为 HIT，且处理器只多执行一次。
	deadline := time.Now().Add(2 * time.Second)
	for {
		hit := client.Get("/data")
		if hit.Header.Get("X-Cache") == "HIT" {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("后台刷新未在超时前完成")
		}

		time.Sleep(5 * time.Millisecond)
	}

	if count.hit() != 2 {
		t.Fatalf("执行次数 = %d, want 2（首次回源 + 一次刷新）", count.hit())
	}
}
