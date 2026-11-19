package trace_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/harness/testutil"
	"gitee.com/leminewx/leego/middleware/trace"
)

const (
	// validTraceID 为测试用链路 ID。
	validTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	// validParentID 为测试用上游片段 ID。
	validParentID = "00f067aa0ba902b7"
)

func TestParseTraceparent(t *testing.T) {
	traceID, parentID, sampled, ok := trace.ParseTraceparent("00-" + validTraceID + "-" + validParentID + "-01")
	if !ok {
		t.Fatal("合法 traceparent 解析失败")
	}

	if traceID != validTraceID || parentID != validParentID || !sampled {
		t.Fatalf("解析结果 = (%s, %s, %v)", traceID, parentID, sampled)
	}

	invalid := map[string]string{
		"空串":            "",
		"版本不支持":         "01-" + validTraceID + "-" + validParentID + "-01",
		"版本非法":          "ff-" + validTraceID + "-" + validParentID + "-01",
		"traceID 非十六进制": "00-" + strings.Repeat("z", 32) + "-" + validParentID + "-01",
		"traceID 全零":    "00-" + strings.Repeat("0", 32) + "-" + validParentID + "-01",
		"traceID 大写":    "00-" + strings.ToUpper(validTraceID) + "-" + validParentID + "-01",
		"spanID 长度错误":   "00-" + validTraceID + "-00f067aa-01",
		"spanID 全零":     "00-" + validTraceID + "-" + strings.Repeat("0", 16) + "-01",
		"标志位长度错误":       "00-" + validTraceID + "-" + validParentID + "-1",
		"附加字段":          "00-" + validTraceID + "-" + validParentID + "-01-extra",
	}

	for name, value := range invalid {
		if _, _, _, ok := trace.ParseTraceparent(value); ok {
			t.Fatalf("%s 应解析失败: %q", name, value)
		}
	}
}

func TestFormatTraceparent(t *testing.T) {
	value := trace.FormatTraceparent(validTraceID, validParentID, true)
	if want := "00-" + validTraceID + "-" + validParentID + "-01"; value != want {
		t.Fatalf("FormatTraceparent = %q, want %q", value, want)
	}

	if other := trace.FormatTraceparent(validTraceID, validParentID, false); !strings.HasSuffix(other, "-00") {
		t.Fatalf("未采样标志位错误: %q", other)
	}

	if value := trace.FormatTraceparent("bad", validParentID, true); value != "" {
		t.Fatalf("非法长度应返回空串, got %q", value)
	}
}

func TestFormatAndParseRoundTrip(t *testing.T) {
	value := trace.FormatTraceparent(validTraceID, validParentID, true)
	traceID, parentID, sampled, ok := trace.ParseTraceparent("  " + value + "  ")
	if !ok || traceID != validTraceID || parentID != validParentID || !sampled {
		t.Fatalf("往返解析失败: %q", value)
	}
}

// newTracerEngine 构造带追踪中间件的引擎，并返回捕获的片段。
func newTracerEngine(tracer *trace.Tracer, captured **trace.Span) *leego.Engine {
	engine := leego.NewEngine()
	engine.UseMiddlewares(tracer.Middleware())
	engine.GET("/ping", func(ctx *leego.Context) {
		if span, ok := trace.TraceFrom(ctx.Request.Context()); ok {
			*captured = span
		}

		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	return engine
}

func TestMiddlewarePropagatesTraceparent(t *testing.T) {
	exporter := trace.NewMemoryExporter(0)
	tracer := trace.New(exporter, trace.WithFlushInterval(time.Hour))

	var captured *trace.Span
	engine := newTracerEngine(tracer, &captured)
	client := testutil.NewClient(engine)

	incoming := trace.FormatTraceparent(validTraceID, validParentID, true)
	response := client.Get("/ping", testutil.WithHeader("traceparent", incoming))
	response.AssertStatus(t, http.StatusOK)

	traceID, spanID, sampled, ok := trace.ParseTraceparent(response.Header.Get("traceparent"))
	if !ok {
		t.Fatalf("响应头 traceparent 非法: %q", response.Header.Get("traceparent"))
	}

	if traceID != validTraceID || !sampled || spanID == validParentID {
		t.Fatalf("响应头 traceparent = %q", response.Header.Get("traceparent"))
	}

	if captured == nil {
		t.Fatal("处理器中未取到片段")
	}

	if captured.TraceID != validTraceID || captured.ParentID != validParentID || captured.SpanID != spanID {
		t.Fatalf("片段信息不匹配: %+v", captured)
	}

	if err := tracer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	spans := exporter.Spans()
	if len(spans) != 1 {
		t.Fatalf("导出片段数 = %d, want 1", len(spans))
	}

	if spans[0].Status != http.StatusOK || spans[0].End.IsZero() || spans[0].Name != "/ping" {
		t.Fatalf("导出片段异常: %+v", spans[0])
	}

	if spans[0].Attrs["http.method"] != http.MethodGet {
		t.Fatalf("属性缺失: %+v", spans[0].Attrs)
	}

	if err := tracer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
}

func TestMiddlewareGeneratesTraceForInvalid(t *testing.T) {
	exporter := trace.NewMemoryExporter(0)
	tracer := trace.New(exporter, trace.WithFlushInterval(time.Hour))

	var captured *trace.Span
	engine := newTracerEngine(tracer, &captured)

	response := testutil.NewClient(engine).Get("/ping", testutil.WithHeader("traceparent", "not-a-traceparent"))
	traceID, spanID, _, ok := trace.ParseTraceparent(response.Header.Get("traceparent"))
	if !ok {
		t.Fatal("非法输入时应生成新的 traceparent")
	}

	if traceID == "" || spanID == "" {
		t.Fatalf("生成的 ID 为空: %q", response.Header.Get("traceparent"))
	}

	if captured == nil || captured.ParentID != "" {
		t.Fatalf("非法输入不应产生上游片段: %+v", captured)
	}

	if captured.TraceID != traceID {
		t.Fatalf("上下文片段与响应头不一致: %s vs %s", captured.TraceID, traceID)
	}

	if err := tracer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
}

func TestMiddlewareSkipsUnscheduledSpans(t *testing.T) {
	exporter := trace.NewMemoryExporter(0)
	tracer := trace.New(exporter,
		trace.WithFlushInterval(time.Hour),
		trace.WithSampler(func(*http.Request) bool { return false }),
	)

	var captured *trace.Span
	engine := newTracerEngine(tracer, &captured)
	response := testutil.NewClient(engine).Get("/ping")
	response.AssertStatus(t, http.StatusOK)

	if captured != nil {
		t.Fatal("未采样时不应写入片段")
	}

	if _, _, sampled, ok := trace.ParseTraceparent(response.Header.Get("traceparent")); !ok || sampled {
		t.Fatalf("未采样时响应头标志位应为 00: %q", response.Header.Get("traceparent"))
	}

	if err := tracer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}

	if len(exporter.Spans()) != 0 {
		t.Fatal("未采样时不应导出片段")
	}

	if err := tracer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
}

func TestShutdownWithoutRequests(t *testing.T) {
	tracer := trace.New(nil)
	if err := tracer.Shutdown(context.Background()); err != nil {
		t.Fatalf("未启动时 Shutdown 应返回 nil, got %v", err)
	}

	// 关闭后再次请求不应阻塞或 panic。
	engine := leego.NewEngine()
	engine.UseMiddlewares(tracer.Middleware())
	engine.GET("/ping", func(ctx *leego.Context) {
		_ = ctx.ResponseText(http.StatusOK, "pong")
	})

	testutil.NewClient(engine).Get("/ping").AssertStatus(t, http.StatusOK)
}

func TestMemoryExporterTrimsHistory(t *testing.T) {
	exporter := trace.NewMemoryExporter(2)
	ctx := context.Background()

	if err := exporter.Export(ctx, []trace.Span{{TraceID: "a"}, {TraceID: "b"}}); err != nil {
		t.Fatalf("Export 失败: %v", err)
	}

	if err := exporter.Export(ctx, []trace.Span{{TraceID: "c"}}); err != nil {
		t.Fatalf("Export 失败: %v", err)
	}

	spans := exporter.Spans()
	if len(spans) != 2 || spans[0].TraceID != "b" || spans[1].TraceID != "c" {
		t.Fatalf("保留历史异常: %+v", spans)
	}

	if !strings.Contains(exporter.String(), "2 spans") {
		t.Fatalf("String 异常: %s", exporter.String())
	}
}

func TestIDsAreRandomHex(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for index := 0; index < 64; index++ {
		value := trace.FormatTraceparent(trace.NewTraceID(), trace.NewSpanID(), true)
		if value == "" {
			t.Fatal("生成的 ID 非法")
		}

		if _, ok := seen[value]; ok {
			t.Fatalf("重复 ID: %s", value)
		}

		seen[value] = struct{}{}
	}
}
