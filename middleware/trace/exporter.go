package trace

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Exporter 定义链路导出器。
//
// Export 会被导出协程串行调用，实现方无需考虑并发；
// 但单次调用耗时会计入队列积压，长时间阻塞应换成异步实现。
type Exporter interface {
	// Export 导出一批片段；返回错误只被丢弃，不会中断后续导出。
	Export(ctx context.Context, spans []Span) error
}

// NewNoopExporter 返回全丢弃导出器，用于关闭导出。
func NewNoopExporter() Exporter {
	return noopExporter{}
}

// noopExporter 丢弃所有片段。
type noopExporter struct{}

// Export 直接返回 nil。
func (noopExporter) Export(context.Context, []Span) error {
	return nil
}

// logExporter 把片段按行打印到日志。
type logExporter struct {
	// logger 为输出目标。
	logger *log.Logger
}

// NewLogExporter 创建日志导出器；logger 为 nil 时使用 log.Default()。
func NewLogExporter(logger *log.Logger) Exporter {
	if logger == nil {
		logger = log.Default()
	}

	return &logExporter{logger: logger}
}

// Export 逐条打印片段摘要。
func (own *logExporter) Export(_ context.Context, spans []Span) error {
	for _, span := range spans {
		own.logger.Printf(
			"trace=%s span=%s parent=%s name=%s status=%d duration=%s",
			span.TraceID, span.SpanID, span.ParentID, span.Name, span.Status, span.Duration(),
		)
	}

	return nil
}

// MemoryExporter 在内存中保留最近的一批片段，便于测试与本地演示。
type MemoryExporter struct {
	// mu 保护 spans。
	mu sync.Mutex
	// spans 保存已导出的片段。
	spans []Span
	// limit 为保留上限，非正数表示不限制。
	limit int
}

// NewMemoryExporter 创建内存导出器。
//
// limit 为保留条数上限，超出后按先进先出丢弃；传入 0 表示不限制。
func NewMemoryExporter(limit int) *MemoryExporter {
	return &MemoryExporter{limit: limit}
}

// Export 追加片段。
func (own *MemoryExporter) Export(_ context.Context, spans []Span) error {
	own.mu.Lock()
	own.spans = append(own.spans, spans...)
	if own.limit > 0 && len(own.spans) > own.limit {
		own.spans = append([]Span(nil), own.spans[len(own.spans)-own.limit:]...)
	}
	own.mu.Unlock()

	return nil
}

// Spans 返回当前已导出片段的副本。
func (own *MemoryExporter) Spans() []Span {
	own.mu.Lock()
	copied := append([]Span(nil), own.spans...)
	own.mu.Unlock()

	return copied
}

// String 返回便于断言的可读摘要。
func (own *MemoryExporter) String() string {
	return fmt.Sprintf("memory exporter: %d spans", len(own.Spans()))
}
