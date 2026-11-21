package compress_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/harness/testutil"
	"gitee.com/leminewx/leego/middlewares/compress"
)

// TestCompressRestoresWriterOnPanic 验证处理器 panic 后压缩包装器被还原。
func TestCompressRestoresWriterOnPanic(t *testing.T) {
	handle := compress.New().AdaptToMiddleware()(func(*leego.Context) {
		panic("boom")
	})

	ctx := leego.NewContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	original := ctx.Response

	func() {
		defer func() {
			_ = recover()
		}()
		handle(ctx)
	}()

	if ctx.Response != original {
		t.Fatal("panic 后必须还原响应写入器")
	}
}

// newEngine 构造启用压缩的最小引擎。
func newEngine(config ...compress.Config) *leego.Engine {
	engine := leego.NewEngine()
	engine.UseMiddlewares(compress.New(config...).AdaptToMiddleware())

	engine.GET("/json", func(ctx *leego.Context) {
		_ = ctx.ResponseJSON(http.StatusOK, map[string]string{"message": strings.Repeat("leego ", 200)})
	})

	engine.GET("/event", func(ctx *leego.Context) {
		ctx.SetResponseHeader("Content-Type", "text/event-stream")
		_, _ = ctx.Response.Write([]byte("data: hello\n\n"))
	})

	engine.GET("/small", func(ctx *leego.Context) {
		ctx.SetResponseHeader("Content-Type", "application/json")
		ctx.SetResponseHeader("Content-Length", "2")
		_, _ = ctx.Response.Write([]byte("{}"))
	})

	engine.GET("/plain", func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusOK, strings.Repeat("leego ", 100))
	})

	engine.GET("/empty", func(ctx *leego.Context) {
		ctx.SetStatusCode(http.StatusNoContent)
	})

	return engine
}

// gunzip 解压响应体。
func gunzip(t *testing.T, data []byte) string {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("创建 gzip 读取器失败: %v", err)
	}

	defer func() { _ = reader.Close() }()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}

	return string(decoded)
}

func TestCompressJSON(t *testing.T) {
	client := testutil.NewClient(newEngine())
	response := client.Get("/json", testutil.WithHeader("Accept-Encoding", "gzip, deflate"))

	response.AssertStatus(t, http.StatusOK)
	response.AssertHeader(t, "Content-Encoding", "gzip")

	if !strings.Contains(response.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q", response.Header.Get("Vary"))
	}

	if response.Header.Get("Content-Length") != "" {
		t.Fatal("压缩后应移除 Content-Length")
	}

	decoded := gunzip(t, response.Body)
	if !strings.Contains(decoded, "message") {
		t.Fatalf("解压内容异常: %s", decoded)
	}
}

func TestCompressSkipsWithoutNegotiation(t *testing.T) {
	client := testutil.NewClient(newEngine())
	response := client.Get("/json")
	response.AssertStatus(t, http.StatusOK)

	if response.Header.Get("Content-Encoding") != "" {
		t.Fatal("客户端未声明 gzip 时不应压缩")
	}

	response.AssertContains(t, "message")
}

func TestCompressSkipsEventStream(t *testing.T) {
	client := testutil.NewClient(newEngine())
	response := client.Get("/event", testutil.WithHeader("Accept-Encoding", "gzip"))

	if response.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("SSE 不应压缩")
	}

	response.AssertContains(t, "data: hello")
}

func TestCompressSkipsSmallBody(t *testing.T) {
	client := testutil.NewClient(newEngine())
	response := client.Get("/small", testutil.WithHeader("Accept-Encoding", "gzip"))

	if response.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("小响应体不应压缩")
	}
}

func TestCompressSkipsHeadAndNoContent(t *testing.T) {
	client := testutil.NewClient(newEngine())

	head := client.Head("/json", testutil.WithHeader("Accept-Encoding", "gzip"))
	if head.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("HEAD 请求不应压缩")
	}

	empty := client.Get("/empty", testutil.WithHeader("Accept-Encoding", "gzip"))
	empty.AssertStatus(t, http.StatusNoContent)
	if empty.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("204 不应压缩")
	}
}

func TestCompressHonorsCustomTypes(t *testing.T) {
	// 只压缩 JSON，text/plain 保持原样。
	engine := newEngine(compress.Config{
		Level:   gzip.BestSpeed,
		MinSize: 0,
		Types:   []string{"application/json"},
	})

	client := testutil.NewClient(engine)

	plain := client.Get("/plain", testutil.WithHeader("Accept-Encoding", "gzip"))
	if plain.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("未命中 Types 的响应不应压缩")
	}

	jsonResponse := client.Get("/json", testutil.WithHeader("Accept-Encoding", "gzip"))
	if jsonResponse.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("命中的 JSON 应压缩")
	}
}

func TestCompressDetectsContentType(t *testing.T) {
	// 未显式设置 Content-Type 时按首块数据嗅探。
	engine := leego.NewEngine()
	engine.UseMiddlewares(compress.New().AdaptToMiddleware())
	engine.GET("/detect", func(ctx *leego.Context) {
		_, _ = ctx.Response.Write([]byte(`{"names":["` + strings.Repeat("leego", 500) + `"]}`))
	})

	response := testutil.NewClient(engine).Get("/detect", testutil.WithHeader("Accept-Encoding", "gzip"))
	if response.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("JSON 数据应被嗅探并压缩")
	}

	// 压缩后必须把嗅探到原始内容的类型回写到响应头，
	// 否则底层会对 gzip 字节重新嗅探出 application/x-gzip 等错误类型。
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain（嗅探结果回写）", contentType)
	}

	if strings.Contains(contentType, "gzip") {
		t.Fatalf("Content-Type = %q, 不得把压缩后字节的类型当作原类型", contentType)
	}

	if !strings.Contains(gunzip(t, response.Body), "leego") {
		t.Fatal("解压内容异常")
	}
}

// TestCompressHonorsRejectedQuality 验证 Accept-Encoding: gzip;q=0 不压缩。
//
// 仅匹配到 gzip 字样就压缩会违背客户端明确拒绝的语义，
// 客户端可能因此拿到无法处理的内容。
func TestCompressHonorsRejectedQuality(t *testing.T) {
	client := testutil.NewClient(newEngine())

	rejected := client.Get("/json", testutil.WithHeader("Accept-Encoding", "gzip;q=0, deflate"))
	rejected.AssertStatus(t, http.StatusOK)

	if rejected.Header.Get("Content-Encoding") != "" {
		t.Fatalf("q=0 时不应压缩，Content-Encoding = %q", rejected.Header.Get("Content-Encoding"))
	}

	rejected.AssertContains(t, "message")

	// 通配符标记 q=0 同样视为拒绝。
	wildcard := client.Get("/json", testutil.WithHeader("Accept-Encoding", "*;q=0"))
	if wildcard.Header.Get("Content-Encoding") != "" {
		t.Fatalf("*;q=0 时不应压缩，Content-Encoding = %q", wildcard.Header.Get("Content-Encoding"))
	}
}
