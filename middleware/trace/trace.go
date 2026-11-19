// Package trace 提供 W3C traceparent 传播与批量化链路导出能力。
//
// 设计要点：
//   - 只实现 W3C Trace Context 的最小可用集：解析、生成、向下游与客户端传播；
//   - ID 全部来自 crypto/rand，禁止用时间戳或伪随机拼接，避免跨进程碰撞；
//   - 导出走"带缓冲队列 + 批量 + 定时"的独立协程，追踪永远不会反压业务；
//   - 队列满或导出器 panic 只丢样本，不影响请求处理。
package trace

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/leego"
)

const (
	// _DEFAULT_HEADER 为默认的传播头名称。
	_DEFAULT_HEADER = "traceparent"
	// _DEFAULT_BATCH_SIZE 为默认单批导出条数。
	_DEFAULT_BATCH_SIZE = 32
	// _DEFAULT_BUFFER_SIZE 为默认队列容量。
	_DEFAULT_BUFFER_SIZE = 256
	// _DEFAULT_FLUSH_INTERVAL 为默认批量导出间隔。
	_DEFAULT_FLUSH_INTERVAL = time.Second
	// _DEFAULT_EXPORT_TIMEOUT 为默认单次导出超时。
	_DEFAULT_EXPORT_TIMEOUT = 2 * time.Second
)

// Span 描述一次请求的链路片段。
type Span struct {
	// TraceID 为 32 位小写十六进制链路 ID。
	TraceID string
	// SpanID 为 16 位小写十六进制片段 ID。
	SpanID string
	// ParentID 为上游片段 ID；链路起点为空。
	ParentID string
	// Name 为片段名称，优先使用路由 pattern。
	Name string
	// Start 为片段开始时间。
	Start time.Time
	// End 为片段结束时间。
	End time.Time
	// Status 为响应状态码；未写入响应时为 0。
	Status int
	// Attrs 为附加属性，例如 HTTP 方法、路径与客户端地址。
	Attrs map[string]string
}

// Duration 返回片段耗时。
func (own Span) Duration() time.Duration {
	if own.End.IsZero() {
		return 0
	}

	return own.End.Sub(own.Start)
}

// Options 保存追踪中间件的配置。
type Options struct {
	// Header 为传播头名称，默认 traceparent。
	Header string
	// BatchSize 为单批导出条数，达到后立即导出。
	BatchSize int
	// BufferSize 为队列容量，队满时丢弃新样本。
	BufferSize int
	// FlushInterval 为定时导出间隔。
	FlushInterval time.Duration
	// ExportTimeout 为单次导出的超时时间。
	ExportTimeout time.Duration
	// Sampler 决定是否采集当前请求；nil 表示全部采集。
	Sampler func(request *http.Request) bool
	// Propagation 为 true 时向下游与客户端传播 traceparent。
	Propagation bool
}

// NewDefaultOptions 返回推荐配置。
func NewDefaultOptions() Options {
	return Options{
		Header:        _DEFAULT_HEADER,
		BatchSize:     _DEFAULT_BATCH_SIZE,
		BufferSize:    _DEFAULT_BUFFER_SIZE,
		FlushInterval: _DEFAULT_FLUSH_INTERVAL,
		ExportTimeout: _DEFAULT_EXPORT_TIMEOUT,
		Propagation:   true,
	}
}

// normalize 修正非法配置并填充默认值。
func (own Options) normalize() Options {
	defaults := NewDefaultOptions()

	if own.Header == "" {
		own.Header = defaults.Header
	}

	if own.BatchSize <= 0 {
		own.BatchSize = defaults.BatchSize
	}

	if own.BufferSize <= 0 {
		own.BufferSize = defaults.BufferSize
	}

	// 队列容量小于批量时会永远凑不满一批，这里抬到批量大小。
	if own.BufferSize < own.BatchSize {
		own.BufferSize = own.BatchSize
	}

	if own.FlushInterval <= 0 {
		own.FlushInterval = defaults.FlushInterval
	}

	if own.ExportTimeout <= 0 {
		own.ExportTimeout = defaults.ExportTimeout
	}

	return own
}

// Option 修改 Options。
type Option func(*Options)

// WithHeader 设置传播头名称。
func WithHeader(name string) Option {
	return func(options *Options) {
		if name != "" {
			options.Header = name
		}
	}
}

// WithBatchSize 设置单批导出条数。
func WithBatchSize(size int) Option {
	return func(options *Options) {
		if size > 0 {
			options.BatchSize = size
		}
	}
}

// WithBufferSize 设置队列容量。
func WithBufferSize(size int) Option {
	return func(options *Options) {
		if size > 0 {
			options.BufferSize = size
		}
	}
}

// WithFlushInterval 设置定时导出间隔。
func WithFlushInterval(interval time.Duration) Option {
	return func(options *Options) {
		if interval > 0 {
			options.FlushInterval = interval
		}
	}
}

// WithSampler 设置采样器；传入 nil 表示全部采集。
func WithSampler(sampler func(request *http.Request) bool) Option {
	return func(options *Options) {
		options.Sampler = sampler
	}
}

// WithoutPropagation 关闭 traceparent 传播，只做本地采集。
func WithoutPropagation() Option {
	return func(options *Options) {
		options.Propagation = false
	}
}

// Tracer 是链路追踪中间件。
//
// 与其它中间件不同，New 返回 *Tracer 而不是直接返回中间件：
// 批量导出需要独立协程，必须提供 Shutdown 才能在进程退出前把数据刷干净。
type Tracer struct {
	// exporter 为数据导出器。
	exporter Exporter
	// options 为规范化后的配置。
	options Options
	// queue 为待导出片段队列。
	queue chan Span
	// flush 用于处理同步刷新请求。
	flush chan chan struct{}
	// stop 用于通知导出协程退出。
	stop chan struct{}
	// done 在导出协程退出后关闭。
	done chan struct{}
	// once 保证导出协程只启动一次。
	once sync.Once
	// stopOnce 保证 stop 只关闭一次。
	stopOnce sync.Once
	// started 标记导出协程是否已启动。
	started atomic.Bool
	// stopped 标记是否已请求关闭。
	stopped atomic.Bool
}

// New 创建链路追踪器。
//
// exporter 为 nil 时使用全丢弃导出器，便于在测试中关闭导出。
func New(exporter Exporter, options ...Option) *Tracer {
	if exporter == nil {
		exporter = NewNoopExporter()
	}

	settings := NewDefaultOptions()
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}

	settings = settings.normalize()

	return &Tracer{
		exporter: exporter,
		options:  settings,
		queue:    make(chan Span, settings.BufferSize),
		flush:    make(chan chan struct{}),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Middleware 返回中间件，可直接传给 Engine.UseMiddlewares。
func (own *Tracer) Middleware() leego.MiddlewareFunc {
	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			own.handle(next, ctx)
		}
	}
}

// Shutdown 停止导出协程并等待已采集数据落盘。
//
// ctx 超时后立即返回，未导出的数据会被丢弃。
func (own *Tracer) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	own.stopOnce.Do(func() {
		own.stopped.Store(true)
		close(own.stop)
	})

	if !own.started.Load() {
		return nil
	}

	select {
	case <-own.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Flush 立即导出队列中已采集的片段。
func (own *Tracer) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if !own.started.Load() || own.stopped.Load() {
		return nil
	}

	reply := make(chan struct{})
	select {
	case own.flush <- reply:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-reply:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handle 完成传播头解析、片段生命周期管理与异步导出。
func (own *Tracer) handle(next leego.HandleFunc, ctx *leego.Context) {
	request := ctx.Request
	if request == nil {
		next(ctx)
		return
	}

	traceID, parentID, _, ok := ParseTraceparent(request.Header.Get(own.options.Header))
	if !ok {
		traceID = newTraceID()
		parentID = ""
	}

	sampled := true
	if own.options.Sampler != nil {
		sampled = own.options.Sampler(request)
	}

	span := &Span{
		TraceID:  traceID,
		SpanID:   newSpanID(),
		ParentID: parentID,
		Name:     spanName(request),
		Start:    time.Now(),
		Attrs: map[string]string{
			"http.method":    request.Method,
			"http.target":    request.URL.RequestURI(),
			"http.user_ip":   request.RemoteAddr,
			"http.user_host": request.Host,
		},
	}

	if sampled {
		// 片段只挂在本次请求的 context 上，不进入对象池、不外泄到后台协程。
		ctx.Request = request.WithContext(context.WithValue(request.Context(), spanKey{}, span))
	}

	if own.options.Propagation {
		value := FormatTraceparent(traceID, span.SpanID, sampled)
		ctx.Request.Header.Set(own.options.Header, value)
		// 响应头在业务写响应之前设置，保证一定会被发送。
		ctx.Response.Header().Set(own.options.Header, value)
	}

	next(ctx)

	span.End = time.Now()
	span.Status = ctx.Response.GetStatusCode()
	if sampled {
		own.enqueue(*span)
	}
}

// enqueue 把片段放入队列；队满时丢弃，绝不阻塞请求。
func (own *Tracer) enqueue(span Span) {
	if own.stopped.Load() {
		return
	}

	own.once.Do(own.start)

	select {
	case own.queue <- span:
	default:
		// 采样率过高或导出过慢时优先保住请求延迟。
	}
}

// start 启动导出协程。
func (own *Tracer) start() {
	own.started.Store(true)
	go own.run()
}

// run 是导出协程主循环。
func (own *Tracer) run() {
	ticker := time.NewTicker(own.options.FlushInterval)
	batch := make([]Span, 0, own.options.BatchSize)

	for {
		select {
		case span := <-own.queue:
			batch = append(batch, span)
			if len(batch) >= own.options.BatchSize {
				own.export(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			own.export(batch)
			batch = batch[:0]
		case reply := <-own.flush:
			own.drain(&batch)
			own.export(batch)
			batch = batch[:0]
			close(reply)
		case <-own.stop:
			own.drain(&batch)
			own.export(batch)
			ticker.Stop()
			close(own.done)
			return
		}
	}
}

// drain 非阻塞地把队列中的剩余片段取出。
func (own *Tracer) drain(batch *[]Span) {
	for {
		select {
		case span := <-own.queue:
			*batch = append(*batch, span)
		default:
			return
		}
	}
}

// export 调用导出器；导出失败或 panic 都只丢样本。
func (own *Tracer) export(spans []Span) {
	if len(spans) == 0 {
		return
	}

	defer func() {
		_ = recover()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), own.options.ExportTimeout)
	err := own.exporter.Export(ctx, spans)
	cancel()

	_ = err
}

// spanName 返回片段名称，优先使用路由 pattern。
func spanName(request *http.Request) string {
	if request.Pattern != "" {
		return request.Pattern
	}

	if request.URL != nil && request.URL.Path != "" {
		return request.URL.Path
	}

	return "/"
}

// spanKey 是 context 中保存片段的私有键类型。
type spanKey struct{}

// TraceFrom 从 context 中取出片段。
//
// 只有采样命中的请求才会写入片段；未命中时返回 false。
func TraceFrom(ctx context.Context) (*Span, bool) {
	if ctx == nil {
		return nil, false
	}

	span, ok := ctx.Value(spanKey{}).(*Span)
	return span, ok && span != nil
}
