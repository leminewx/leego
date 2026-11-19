package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// TestLogInfo 验证 INFO 日志的级别前缀与自动换行。
func TestLogInfo(t *testing.T) {
	buffer := new(bytes.Buffer)
	SetOutput(buffer)
	defer SetOutput(nil)

	LogInfo("hello %s", "world")

	output := buffer.String()
	if !strings.Contains(output, "INFO") {
		t.Fatalf("输出缺少 INFO 前缀: %q", output)
	}

	if !strings.Contains(output, "hello world") {
		t.Fatalf("输出缺少消息体: %q", output)
	}

	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("输出缺少换行: %q", output)
	}
}

// TestLogWarn 验证 WARN 日志的级别前缀。
func TestLogWarn(t *testing.T) {
	buffer := new(bytes.Buffer)
	SetOutput(buffer)
	defer SetOutput(nil)

	LogWarn("disk usage %d%%", 90)

	output := buffer.String()
	if !strings.Contains(output, "WARN") {
		t.Fatalf("输出缺少 WARN 前缀: %q", output)
	}

	if !strings.Contains(output, "disk usage 90%") {
		t.Fatalf("输出内容不符合预期: %q", output)
	}

	if strings.Count(output, "\n") != 1 {
		t.Fatalf("已带换行的格式不应重复换行: %q", output)
	}
}

// TestSetOutputNilRestoresDefault 验证传入 nil 时恢复默认输出。
func TestSetOutputNilRestoresDefault(t *testing.T) {
	buffer := new(bytes.Buffer)
	SetOutput(buffer)

	if GetOutput() != buffer {
		t.Fatal("GetOutput 应返回刚设置的输出目标")
	}

	SetOutput(nil)
	if GetOutput() == buffer {
		t.Fatal("SetOutput(nil) 应恢复默认输出")
	}
}

// lockedBuffer 是并发安全的缓冲区，用于验证多协程日志写入。
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write 实现 io.Writer。
func (own *lockedBuffer) Write(data []byte) (int, error) {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.buffer.Write(data)
}

// String 返回当前缓冲内容的副本。
func (own *lockedBuffer) String() string {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.buffer.String()
}

// TestConcurrentLogging 验证并发写入不会 panic（配合 -race 使用）。
func TestConcurrentLogging(t *testing.T) {
	buffer := new(lockedBuffer)
	SetOutput(buffer)
	defer SetOutput(nil)

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		waitGroup.Add(1)

		go func(worker int) {
			defer waitGroup.Done()

			for index := 0; index < 100; index++ {
				LogInfo("worker=%d index=%d", worker, index)
				LogWarn("worker=%d warn=%d", worker, index)
			}
		}(worker)
	}

	waitGroup.Wait()

	if lines := strings.Count(buffer.String(), "\n"); lines != 1600 {
		t.Fatalf("日志行数 = %d, want 1600", lines)
	}
}
