package trace

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"
)

// sampleSpan 构造用于导出测试的片段。
func sampleSpan(name string) Span {
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	return Span{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:  "00f067aa0ba902b7",
		Name:    name,
		Start:   start,
		End:     start.Add(time.Millisecond),
		Status:  200,
	}
}

// TestNoopExporterReturnsNil 验证空导出器丢弃片段且不报错。
func TestNoopExporterReturnsNil(t *testing.T) {
	if err := NewNoopExporter().Export(context.Background(), []Span{sampleSpan("noop")}); err != nil {
		t.Fatalf("空导出器应返回 nil: %v", err)
	}
}

// TestLogExporterWritesEachSpan 验证每条片段都被打印为一行摘要。
func TestLogExporterWritesEachSpan(t *testing.T) {
	buffer := new(bytes.Buffer)
	exporter := NewLogExporter(log.New(buffer, "", 0))

	spans := []Span{sampleSpan("first"), sampleSpan("second")}
	if err := exporter.Export(context.Background(), spans); err != nil {
		t.Fatalf("日志导出失败: %v", err)
	}

	output := buffer.String()
	if lines := strings.Count(output, "trace="); lines != 2 {
		t.Fatalf("日志行数 = %d, want 2: %s", lines, output)
	}

	if !strings.Contains(output, "name=first") || !strings.Contains(output, "duration=1ms") {
		t.Fatalf("日志内容异常: %s", output)
	}
}

// TestNewLogExporterAcceptsNilLogger 验证 nil 日志器回退到默认值。
func TestNewLogExporterAcceptsNilLogger(t *testing.T) {
	if exporter := NewLogExporter(nil); exporter == nil {
		t.Fatal("应返回非 nil 导出器")
	}
}

// TestMemoryExporterReturnsCopies 验证快照读取不会暴露内部切片。
func TestMemoryExporterReturnsCopies(t *testing.T) {
	exporter := NewMemoryExporter(2)
	if err := exporter.Export(context.Background(), []Span{sampleSpan("first")}); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "first" {
		t.Fatalf("导出结果异常: %+v", spans)
	}

	spans[0].Name = "mutated"
	if exported := exporter.GetSpans(); exported[0].Name != "first" {
		t.Fatalf("GetSpans 应返回副本: %+v", exported)
	}

	if summary := exporter.String(); summary != "memory exporter: 1 spans" {
		t.Fatalf("String() = %q", summary)
	}
}

// TestMemoryExporterWithoutLimit 验证上限为零时不丢弃片段。
func TestMemoryExporterWithoutLimit(t *testing.T) {
	exporter := NewMemoryExporter(0)
	if err := exporter.Export(context.Background(), []Span{sampleSpan("a"), sampleSpan("b")}); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	if spans := exporter.GetSpans(); len(spans) != 2 {
		t.Fatalf("片段数量 = %d, want 2", len(spans))
	}
}
