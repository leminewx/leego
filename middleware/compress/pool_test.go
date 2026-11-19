package compress

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

// TestPoolForCachesByLevel 验证不同压缩级别使用独立对象池。
func TestPoolForCachesByLevel(t *testing.T) {
	if poolFor(gzip.BestSpeed) != poolFor(gzip.BestSpeed) {
		t.Fatal("同一压缩级别应复用对象池")
	}

	if poolFor(gzip.BestSpeed) == poolFor(gzip.BestCompression) {
		t.Fatal("不同压缩级别不应共用对象池")
	}
}

// TestAcquireAndReleaseCompressor 验证对象池取出的写入器可产出合法 gzip。
func TestAcquireAndReleaseCompressor(t *testing.T) {
	payload := []byte("leego compress pool")

	buffer := new(bytes.Buffer)
	writer := acquireCompressor(buffer, gzip.BestSpeed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("关闭写入器失败: %v", err)
	}
	releaseCompressor(writer, gzip.BestSpeed)

	reader, err := gzip.NewReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("压缩输出非法: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
	})

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}

	if !bytes.Equal(decoded, payload) {
		t.Fatalf("解压内容 = %q, want %q", decoded, payload)
	}
}

// TestReleasedCompressorIsReusable 验证归还后的写入器可被再次复用。
func TestReleasedCompressorIsReusable(t *testing.T) {
	first := acquireCompressor(io.Discard, gzip.BestSpeed)
	if err := first.Close(); err != nil {
		t.Fatalf("关闭写入器失败: %v", err)
	}
	releaseCompressor(first, gzip.BestSpeed)

	buffer := new(bytes.Buffer)
	second := acquireCompressor(buffer, gzip.BestSpeed)
	if _, err := second.Write([]byte("again")); err != nil {
		t.Fatalf("复用写入失败: %v", err)
	}

	if err := second.Close(); err != nil {
		t.Fatalf("关闭复用写入器失败: %v", err)
	}
	releaseCompressor(second, gzip.BestSpeed)

	if buffer.Len() == 0 {
		t.Fatal("复用后的写入器未产生输出")
	}
}
