package assetpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGetDir 验证解析出的目录存在且包含演示数据文件。
func TestGetDir(t *testing.T) {
	dir := GetDir()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("演示数据目录不存在: %v", err)
	}

	if !info.IsDir() {
		t.Fatalf("演示数据路径不是目录: %s", dir)
	}

	for _, name := range []string{"data.txt", "stress.html", "ws-client.html"} {
		if _, err = os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("演示数据目录缺少 %s: %v", name, err)
		}
	}
}

// TestResolve 验证路径归一：普通文件命中、空路径与越界路径回落。
func TestResolve(t *testing.T) {
	root := GetDir()

	cases := map[string]string{
		"data.txt":         filepath.Join(root, "data.txt"),
		"/data.txt":        filepath.Join(root, "data.txt"),
		"":                 filepath.Join(root, "data.txt"),
		"../../etc/secret": filepath.Join(root, "data.txt"),
	}

	for relative, want := range cases {
		if got := Resolve(relative); got != want {
			t.Fatalf("Resolve(%q) = %q，期望 %q", relative, got, want)
		}
	}

	// 落在演示目录内部的子路径应原样返回。
	inner := filepath.Join("stress", "..", "stress.html")
	if got := Resolve(inner); !strings.HasSuffix(got, "stress.html") {
		t.Fatalf("Resolve(%q) = %q，未命中演示文件", inner, got)
	}
}
