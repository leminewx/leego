package compress

import (
	"compress/gzip"
	"io"
	"sync"
)

// compressorPools 按压缩级别缓存 gzip 写入器。
//
// 不同级别不能共用同一个池：gzip.Writer.Reset 不会改变压缩级别，
// 混用会导致配置“看起来生效、实际没生效”。
var compressorPools sync.Map

// poolFor 返回指定级别的对象池。
func poolFor(level int) *sync.Pool {
	if cached, ok := compressorPools.Load(level); ok {
		return cached.(*sync.Pool)
	}

	pool := &sync.Pool{
		New: func() any {
			writer, err := gzip.NewWriterLevel(io.Discard, level)
			if err != nil {
				// 级别已在 Config.normalize 中校验，这里只做兜底。
				writer = gzip.NewWriter(io.Discard)
			}

			return writer
		},
	}

	actual, _ := compressorPools.LoadOrStore(level, pool)
	return actual.(*sync.Pool)
}

// acquireCompressor 取出并重置一个 gzip 写入器。
func acquireCompressor(target io.Writer, level int) *gzip.Writer {
	writer := poolFor(level).Get().(*gzip.Writer)
	writer.Reset(target)
	return writer
}

// releaseCompressor 归还 gzip 写入器；level 必须与 acquire 时一致。
//
// 调用前必须已经 Close：Close 会写出尾部校验信息，遗漏会导致客户端一直等待。
func releaseCompressor(writer *gzip.Writer, level int) {
	writer.Reset(io.Discard)
	poolFor(level).Put(writer)
}
