package upload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeNameStripsPath 验证文件名中的目录成分被剥离。
func TestSanitizeNameStripsPath(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"   ":              "",
		"report.pdf":       "report.pdf",
		"dir/report.txt":   "report.txt",
		"../../etc/passwd": "passwd",
	}

	for input, want := range cases {
		if got := sanitizeName(input); got != want {
			t.Fatalf("sanitizeName(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestFinalPathKeepsSafeExtension 验证最终路径只保留合法扩展名。
func TestFinalPathKeepsSafeExtension(t *testing.T) {
	sum := strings.Repeat("a", 64)

	if got := finalPath(sum, "report.pdf"); got != _FILES_DIR+"/"+sum+".pdf" {
		t.Fatalf("finalPath() = %q", got)
	}

	if got := finalPath(sum, "noext"); got != _FILES_DIR+"/"+sum {
		t.Fatalf("无扩展名时 finalPath() = %q", got)
	}

	long := "file." + strings.Repeat("x", _MAX_EXTENSION+1)
	if got := finalPath(sum, long); got != _FILES_DIR+"/"+sum {
		t.Fatalf("超长扩展名应被丢弃: %q", got)
	}
}

// TestIsSafeID 验证会话 ID 必须是 32 位小写十六进制串。
func TestIsSafeID(t *testing.T) {
	if !isSafeID("0123456789abcdef0123456789abcdef") {
		t.Fatal("合法会话 ID 被拒绝")
	}

	for _, invalid := range []string{
		"",
		"0123456789abcdef",
		"0123456789ABCDEF0123456789ABCDEF",
		"0123456789abcdef0123456789abcdeg",
	} {
		if isSafeID(invalid) {
			t.Fatalf("isSafeID(%q) 应返回 false", invalid)
		}
	}
}

// TestExpectedPartSize 验证最后一片按剩余字节数计算。
func TestExpectedPartSize(t *testing.T) {
	session := Session{Size: 10, PartSize: 4, Parts: make([]bool, 3)}

	if size := expectedPartSize(session, 0); size != 4 {
		t.Fatalf("首片大小 = %d, want 4", size)
	}

	if size := expectedPartSize(session, 1); size != 4 {
		t.Fatalf("中间片大小 = %d, want 4", size)
	}

	if size := expectedPartSize(session, 2); size != 2 {
		t.Fatalf("末尾片大小 = %d, want 2", size)
	}
}

// TestNewLocalStorageCreatesLayout 验证存储目录布局在创建时补齐。
func TestNewLocalStorageCreatesLayout(t *testing.T) {
	root := t.TempDir()

	storage, err := NewLocalStorage(root)
	if err != nil {
		t.Fatalf("创建 LocalStorage 失败: %v", err)
	}

	if storage.Root() == "" {
		t.Fatal("Root 不应为空")
	}

	for _, name := range []string{_SESSIONS_DIR, _PARTS_DIR, _FILES_DIR, _META_DIR} {
		if _, err = os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("目录 %s 未创建: %v", name, err)
		}
	}
}
