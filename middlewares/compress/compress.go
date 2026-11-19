// Package compress 提供基于 gzip 的响应压缩中间件。
//
// 设计要点：
//   - 只使用标准库 compress/gzip，不引入 brotli 等第三方实现；
//   - 包装器实现 response.Writer，可与 SSE、WebSocket、文件下载共存；
//   - 判定顺序集中在 WriteHeader/首次 Write，之后不再改变，避免响应头与实体不一致。
package compress

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"net"
	"net/http"
	"strings"

	"gitee.com/leminewx/leego"
	"gitee.com/leminewx/leego/response"
)

const (
	// _DEFAULT_MIN_SIZE 为默认的最小压缩阈值（1 KiB）。
	//
	// 只有响应显式设置了 Content-Length 且小于该值时才会跳压缩；
	// 分块响应无法预知大小，按“压缩”处理。
	_DEFAULT_MIN_SIZE = 1024
)

// Config 定义压缩行为。
type Config struct {
	// Level 为 gzip 压缩级别，取值 -2..9；非法值回退到 gzip.DefaultCompression。
	Level int
	// MinSize 为最小压缩字节数，0 表示不限制。
	MinSize int
	// Types 为参与压缩的媒体类型，支持 "text/*" 形式的通配。
	Types []string
}

// NewDefaultConfig 返回推荐配置。
func NewDefaultConfig() Config {
	return Config{
		Level:   gzip.DefaultCompression,
		MinSize: _DEFAULT_MIN_SIZE,
		Types: []string{
			"text/*",
			"application/json",
			"application/xml",
			"application/javascript",
			"application/xhtml+xml",
			"image/svg+xml",
		},
	}
}

// normalize 修正非法配置并填充默认值。
func (own Config) normalize() Config {
	defaults := NewDefaultConfig()

	if own.Level < gzip.HuffmanOnly || own.Level > gzip.BestCompression {
		own.Level = defaults.Level
	}

	if own.MinSize < 0 {
		own.MinSize = 0
	}

	if len(own.Types) == 0 {
		own.Types = defaults.Types
	}

	return own
}

// Compress 响应压缩中间件。
type Compress struct {
	// config 为规范化后的配置。
	config Config
}

// NewCompress 创建压缩中间件。
//
// 参数 config 可省略；多个配置时仅第一个生效，与其它中间件保持一致。
// 中间件会在响应完成后关闭 gzip 流；业务处理器无需感知。
// 客户端未声明支持 gzip 时不压缩，遵循 HTTP 协商语义。
func NewCompress(config ...Config) *Compress {
	options := NewDefaultConfig()
	if len(config) > 0 {
		options = config[0].normalize()
	}

	return &Compress{config: options}
}

// AdaptToMiddleware 将响应压缩适配为 leego.MiddlewareFunc。
func (own *Compress) AdaptToMiddleware() leego.MiddlewareFunc {
	options := own.config

	return func(next leego.HandleFunc) leego.HandleFunc {
		return func(ctx *leego.Context) {
			original := ctx.Response
			wrapped := newWriter(original, options, ctx.Request)
			ctx.Response = wrapped

			// 处理器 panic 时也要收尾：结束 gzip 流并还原写入器，
			// 否则恢复中间件拿到的是压缩包装器，后续写入会被重复压缩。
			defer func() {
				wrapped.close()
				ctx.Response = original
			}()

			next(ctx)
		}
	}
}

// writer 是压缩包装器，实现 response.Writer。
type writer struct {
	// inner 为原始响应包装器。
	inner response.Writer
	// config 为压缩配置。
	config Config
	// request 为当前请求，用于判断方法、Accept-Encoding 与是否跳过。
	request *http.Request
	// compressor 为惰性创建的 gzip 写入器。
	compressor *gzip.Writer
	// level 为 compressor 对应的压缩级别，归还对象池时必须一致。
	level int
	// decided 标记是否已完成压缩判定。
	decided bool
	// enabled 标记本次响应是否压缩。
	enabled bool
	// hijacked 标记连接已被劫持，后续写入不再压缩。
	hijacked bool
}

// newWriter 创建压缩包装器。
func newWriter(inner response.Writer, config Config, request *http.Request) *writer {
	return &writer{inner: inner, config: config, request: request}
}

// Header 返回底层响应头。
func (own *writer) Header() http.Header {
	return own.inner.Header()
}

// WriteHeader 写入状态码，并在必要时切换为压缩输出。
func (own *writer) WriteHeader(status int) {
	if own.decided {
		own.inner.WriteHeader(status)
		return
	}

	own.decided = true
	if own.shouldCompress(status, own.inner.Header().Get("Content-Type")) {
		own.enable()
	}

	own.inner.WriteHeader(status)
}

// Write 写入响应体；未完成判定时先按数据嗅探内容类型。
func (own *writer) Write(data []byte) (int, error) {
	if own.hijacked {
		return own.inner.Write(data)
	}

	if !own.decided {
		own.decided = true

		contentType := own.inner.Header().Get("Content-Type")
		if contentType == "" && len(data) > 0 {
			// 仅用于判定，不写回响应头：真正的嗅探仍由 net/http 完成。
			contentType = http.DetectContentType(data)
		}

		if own.shouldCompress(http.StatusOK, contentType) {
			own.enable()
		}
	}

	if !own.enabled {
		// 判定为不压缩时必须直写原始响应体，
		// 否则会额外包一层 gzip 却不在响应头声明，客户端拿到的是乱码。
		return own.inner.Write(data)
	}

	if own.compressor == nil {
		// 首个字节到达时才真正创建 gzip 流：
		// 这样“头已完成但无响应体”的场景不会额外写出空 gzip 流。
		own.level = own.config.Level
		own.compressor = acquireCompressor(own.inner, own.level)
	}

	return own.compressor.Write(data)
}

// Flush 先刷 gzip 缓冲再刷底层，避免出现“头到了、数据没到”。
func (own *writer) Flush() {
	if own.compressor != nil {
		_ = own.compressor.Flush()
	}

	own.inner.Flush()
}

// Hijack 将连接控制权交给调用方（WebSocket 等），并关闭压缩。
func (own *writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	own.hijacked = true
	if own.compressor != nil {
		_ = own.compressor.Close()
		own.compressor = nil
	}

	return own.inner.Hijack()
}

// GetStatusCode 返回底层记录的状态码。
func (own *writer) GetStatusCode() int {
	return own.inner.GetStatusCode()
}

// GetSize 返回实际写出的压缩后字节数，与访问日志口径一致。
func (own *writer) GetSize() int64 {
	return own.inner.GetSize()
}

// Unwrap 返回底层原始 http.ResponseWriter。
func (own *writer) Unwrap() http.ResponseWriter {
	return own.inner.Unwrap()
}

// close 结束压缩流并归还 gzip 写入器。
func (own *writer) close() {
	if own.compressor == nil {
		return
	}

	_ = own.compressor.Close()
	releaseCompressor(own.compressor, own.level)
	own.compressor = nil
}

// enable 设置压缩相关响应头。
func (own *writer) enable() {
	if own.enabled {
		return
	}

	header := own.inner.Header()
	header.Set("Content-Encoding", "gzip")
	header.Add("Vary", "Accept-Encoding")
	// 压缩后长度不可预测，必须移除已设置的长度，否则客户端会截断读取。
	header.Del("Content-Length")

	own.enabled = true
}

// shouldCompress 判断本次响应是否应压缩。
func (own *writer) shouldCompress(status int, contentType string) bool {
	if status == http.StatusSwitchingProtocols ||
		status == http.StatusNoContent ||
		status == http.StatusNotModified ||
		status < http.StatusOK {
		return false
	}

	if own.request != nil && own.request.Method == http.MethodHead {
		return false
	}

	header := own.inner.Header()
	if header.Get("Content-Encoding") != "" {
		return false
	}

	if !acceptsGzip(own.request) {
		return false
	}

	contentType = mediaType(contentType)
	if contentType == "" {
		return false
	}

	// SSE 依赖逐条推送，压缩会破坏分帧语义。
	if contentType == "text/event-stream" {
		return false
	}

	if own.config.MinSize > 0 {
		if length := header.Get("Content-Length"); length != "" {
			var size int
			if _, err := fmt.Sscanf(length, "%d", &size); err == nil && size < own.config.MinSize {
				return false
			}
		}
	}

	if header.Get("Content-Length") == "0" {
		return false
	}

	for _, candidate := range own.config.Types {
		if matchType(contentType, strings.ToLower(strings.TrimSpace(candidate))) {
			return true
		}
	}

	return false
}

// acceptsGzip 判断客户端是否接受 gzip。
func acceptsGzip(request *http.Request) bool {
	if request == nil {
		return true
	}

	for _, item := range strings.Split(request.Header.Get("Accept-Encoding"), ",") {
		encoding := strings.TrimSpace(strings.SplitN(item, ";", 2)[0])
		if strings.EqualFold(encoding, "gzip") || encoding == "*" {
			return true
		}
	}

	return false
}

// mediaType 去掉媒体类型参数并转小写。
func mediaType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if index := strings.IndexByte(contentType, ';'); index >= 0 {
		contentType = strings.TrimSpace(contentType[:index])
	}

	return contentType
}

// matchType 判断媒体类型是否命中配置项。
func matchType(contentType, candidate string) bool {
	if candidate == "*" || candidate == "*/*" {
		return true
	}

	if strings.HasSuffix(candidate, "/*") {
		return strings.HasPrefix(contentType, strings.TrimSuffix(candidate, "*"))
	}

	return contentType == candidate
}
