package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFileOptionsNormalizeDefaults 验证轮转参数补齐与保留。
func TestFileOptionsNormalizeDefaults(t *testing.T) {
	options := FileOptions{}.normalize()
	if options.MaxBytes != _DEFAULT_MAX_BYTES || options.MaxFiles != _DEFAULT_MAX_FILES {
		t.Fatalf("零值配置未补齐默认值: %+v", options)
	}

	kept := FileOptions{MaxBytes: 1024, MaxFiles: 2}.normalize()
	if kept.MaxBytes != 1024 || kept.MaxFiles != 2 {
		t.Fatalf("合法配置被改写: %+v", kept)
	}
}

// TestMemoryStoreAppendPointAndClose 验证内存存储的写入、查询与可重复关闭。
func TestMemoryStoreAppendPointAndClose(t *testing.T) {
	store := NewMemoryStore(time.Minute, 60)
	base := time.Now().Add(-time.Second)

	if err := store.AppendPoint(Point{At: base, Total: 2}); err != nil {
		t.Fatalf("AppendPoint 失败: %v", err)
	}

	points, err := store.Query(base.Add(-time.Minute), base.Add(time.Minute), time.Second)
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}

	if len(points) != 1 || points[0].Total != 2 {
		t.Fatalf("查询结果异常: %+v", points)
	}

	if err = store.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	if err = store.Close(); err != nil {
		t.Fatalf("Close 应可重复调用: %v", err)
	}
}

// TestNewFileStoreRejectsEmptyPath 验证空路径被拒绝。
func TestNewFileStoreRejectsEmptyPath(t *testing.T) {
	if _, err := NewFileStore("   ", time.Minute, 60); err == nil {
		t.Fatal("空路径应返回错误")
	}
}

// TestFileStoreCreatesDirectories 验证父目录不存在时自动创建。
func TestFileStoreCreatesDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "metrics.jsonl")

	store, err := NewFileStore(path, time.Minute, 60)
	if err != nil {
		t.Fatalf("创建 FileStore 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	if _, err = os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("父目录未创建: %v", err)
	}
}
