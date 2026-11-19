package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/leminewx/leego/response"
)

// TestRecorderMirrorsResponse 验证镜像副本与状态码快照。
func TestRecorderMirrorsResponse(t *testing.T) {
	inner := httptest.NewRecorder()
	writer := newRecorder(response.NewWriter(inner), 0)

	writer.Header().Set("Content-Type", "text/plain")
	writer.WriteHeader(http.StatusCreated)
	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	item := writer.snapshot("key")
	if item.key != "key" || item.status != http.StatusCreated {
		t.Fatalf("快照元信息异常: key=%q status=%d", item.key, item.status)
	}

	if string(item.body) != "abc" {
		t.Fatalf("镜像响应体 = %q", item.body)
	}

	if contentType := item.header.Get("Content-Type"); contentType != "text/plain" {
		t.Fatalf("镜像响应头 = %q", contentType)
	}

	if inner.Body.String() != "abc" {
		t.Fatalf("透传响应体 = %q", inner.Body.String())
	}
}

// TestRecorderDropsMirrorOnOverflow 验证镜像超限后放弃缓存但继续透传。
func TestRecorderDropsMirrorOnOverflow(t *testing.T) {
	inner := httptest.NewRecorder()
	writer := newRecorder(response.NewWriter(inner), 2)

	if _, err := writer.Write([]byte("abcd")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if !writer.overflow {
		t.Fatal("超过上限后应标记 overflow")
	}

	if body := writer.snapshot("key").body; len(body) != 0 {
		t.Fatalf("超限后不应保留半截镜像: %q", body)
	}

	if inner.Body.String() != "abcd" {
		t.Fatalf("透传响应体 = %q", inner.Body.String())
	}
}

// TestRecorderKeepsFirstStatus 验证重复 WriteHeader 只记录第一次状态码。
func TestRecorderKeepsFirstStatus(t *testing.T) {
	inner := httptest.NewRecorder()
	writer := newRecorder(response.NewWriter(inner), 0)

	writer.WriteHeader(http.StatusOK)
	writer.WriteHeader(http.StatusInternalServerError)

	if status := writer.snapshot("key").status; status != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", status)
	}
}

// TestCaptureWriterLimitsBody 验证后台刷新写入器的上限控制。
func TestCaptureWriterLimitsBody(t *testing.T) {
	writer := newCaptureWriter(3)

	if _, err := writer.Write([]byte("toolong")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if !writer.overflow {
		t.Fatal("超过上限后应标记 overflow")
	}

	if writer.body.Len() != 0 {
		t.Fatalf("超限后不应保留响应体: %q", writer.body.String())
	}

	if writer.status != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", writer.status)
	}

	// Flush 只用于满足 http.Flusher 语义，不应产生副作用。
	writer.Flush()
}
