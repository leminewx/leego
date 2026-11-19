package upload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	if storage.GetRoot() == "" {
		t.Fatal("Root 不应为空")
	}

	for _, name := range []string{_SESSIONS_DIR, _PARTS_DIR, _FILES_DIR, _META_DIR} {
		if _, err = os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("目录 %s 未创建: %v", name, err)
		}
	}
}

// TestIndexSurvivesReload 验证秒传索引以追加日志持久化且能跨实例加载。
func TestIndexSurvivesReload(t *testing.T) {
	root := t.TempDir()
	storage, err := NewLocalStorage(root)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}

	sum := strings.Repeat("b", 64)
	relative := _FILES_DIR + "/" + sum + ".bin"
	if err = storage.rememberIndex(sum, relative); err != nil {
		t.Fatalf("写入索引失败: %v", err)
	}

	// 重复写入同一摘要不应追加第二行。
	if err = storage.rememberIndex(sum, relative); err != nil {
		t.Fatalf("重复写入索引失败: %v", err)
	}

	reopened, err := NewLocalStorage(root)
	if err != nil {
		t.Fatalf("重新打开存储失败: %v", err)
	}

	if got, ok := reopened.lookupIndex(sum); !ok || got != relative {
		t.Fatalf("重载后索引 = (%q, %v), want (%q, true)", got, ok, relative)
	}

	data, err := os.ReadFile(filepath.Join(root, _META_DIR, _INDEX_FILE))
	if err != nil {
		t.Fatalf("读取索引文件失败: %v", err)
	}

	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 1 {
		t.Fatalf("索引行数 = %d, want 1（重复摘要不应重复追加）", lines)
	}
}

// TestIndexSkipsCorruptedLines 验证损坏的索引行不会妨碍其余记录加载。
func TestIndexSkipsCorruptedLines(t *testing.T) {
	root := t.TempDir()
	storage, err := NewLocalStorage(root)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}

	sum := strings.Repeat("c", 64)
	relative := _FILES_DIR + "/" + sum + ".bin"
	content := "{not json}\n\n" + `{"sum":"` + sum + `","path":"` + relative + `"}` + "\n"
	if err = os.WriteFile(filepath.Join(root, _META_DIR, _INDEX_FILE), []byte(content), _FILE_MODE); err != nil {
		t.Fatalf("写入索引失败: %v", err)
	}

	if got, ok := storage.lookupIndex(sum); !ok || got != relative {
		t.Fatalf("损坏行后索引 = (%q, %v), want (%q, true)", got, ok, relative)
	}
}

// TestLockSessionReclaimsEntry 验证会话锁在无人使用后被回收。
func TestLockSessionReclaimsEntry(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("创建 LocalStorage 失败: %v", err)
	}

	const id = "0123456789abcdef0123456789abcdef"
	storage.lockSession(id)()

	count := 0
	storage.locks.Range(func(_, _ any) bool {
		count++
		return true
	})

	if count != 0 {
		t.Fatalf("会话锁未回收，残留 %d 个条目", count)
	}
}

// TestLockSessionSerializes 验证同一会话的并发操作仍然互斥。
func TestLockSessionSerializes(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("创建 LocalStorage 失败: %v", err)
	}

	const id = "0123456789abcdef0123456789abcdef"
	unlock := storage.lockSession(id)

	acquired := make(chan struct{})
	go func() {
		release := storage.lockSession(id)
		close(acquired)
		release()
	}()

	select {
	case <-acquired:
		t.Fatal("同一会话的锁未能互斥")
	case <-time.After(20 * time.Millisecond):
	}

	unlock()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("释放后等待者未被唤醒")
	}
}
