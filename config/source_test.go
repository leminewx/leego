package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUseWatchInterval 验证轮询间隔只接受正数。
func TestUseWatchInterval(t *testing.T) {
	settings := sourceConfig{}
	WithWatchInterval(-time.Second)(&settings)
	if settings.interval != 0 {
		t.Fatalf("负数间隔 = %v, want 0", settings.interval)
	}

	WithWatchInterval(time.Second)(&settings)
	if settings.interval != time.Second {
		t.Fatalf("间隔 = %v, want 1s", settings.interval)
	}
}

// TestMapSourceCopiesValues 验证构造与加载都会复制映射。
func TestMapSourceCopiesValues(t *testing.T) {
	origin := map[string]any{"Server.Port": 8080}
	source := NewMapSource(origin)

	origin["Server.Port"] = 9090
	values, err := source.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if values["Server.Port"] != 8080 {
		t.Fatalf("Load() = %v, want 原始值 8080", values["Server.Port"])
	}

	values["Server.Port"] = 7070
	again, _ := source.Load()
	if again["Server.Port"] != 8080 {
		t.Fatalf("第二次 Load() = %v, want 8080", again["Server.Port"])
	}
}

// TestEnvSourceLoadsWithPrefix 验证前缀裁剪与键规范化。
func TestEnvSourceLoadsWithPrefix(t *testing.T) {
	t.Setenv("LEEGO_TEST_SERVER_PORT", "8080")
	t.Setenv("LEEGO_TEST_", "ignored")
	t.Setenv("OTHER_ENV_KEY", "ignored")

	values, err := NewEnvSource("LEEGO_TEST_").Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if values["server.port"] != "8080" {
		t.Fatalf("server.port = %v, want 8080", values["server.port"])
	}

	for key := range values {
		if key == "" || key == "other.env.key" {
			t.Fatalf("不应包含键 %q", key)
		}
	}
}

// TestJSONSourceLoadsFlattened 验证嵌套对象被展平为点号键。
func TestJSONSourceLoadsFlattened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"HTTP":{"Addr":"127.0.0.1:8080","TLS":{"Enabled":true}},"Name":"leego"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	values, err := NewJSONSource(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if values["http.addr"] != "127.0.0.1:8080" {
		t.Fatalf("http.addr = %v", values["http.addr"])
	}

	if values["http.tls.enabled"] != true {
		t.Fatalf("http.tls.enabled = %v", values["http.tls.enabled"])
	}

	if values["name"] != "leego" {
		t.Fatalf("name = %v", values["name"])
	}
}

// TestFlattenMap 验证展平逻辑保留前缀与叶子值。
func TestFlattenMap(t *testing.T) {
	target := make(map[string]any)
	flattenMap("root", map[string]any{
		"A": 1,
		"B": map[string]any{"C": 2},
	}, target)

	if target["root.a"] != 1 || target["root.b.c"] != 2 {
		t.Fatalf("flattenMap() = %v", target)
	}
}

// TestDigestStable 验证摘要与键顺序无关、随内容变化。
func TestDigestStable(t *testing.T) {
	first := map[string]any{"a": 1, "b": "x"}
	second := map[string]any{"b": "x", "a": 1}

	if digest(first) != digest(second) {
		t.Fatal("相同内容不同顺序的摘要必须一致")
	}

	second["a"] = 2
	if digest(first) == digest(second) {
		t.Fatal("内容变化后摘要必须变化")
	}
}

// TestRawDigestTracksBytes 验证原始字节摘要区分格式差异。
func TestRawDigestTracksBytes(t *testing.T) {
	compact := []byte(`{"a":1}`)
	spaced := []byte(`{ "a" : 1 }`)

	if rawDigest(compact) == rawDigest(spaced) {
		t.Fatal("字节不同的文件原始摘要必须不同")
	}

	if rawDigest(compact) != rawDigest([]byte(`{"a":1}`)) {
		t.Fatal("相同字节的原始摘要必须一致")
	}
}

// TestSetWatchIntervalIgnoresNonPositive 验证非正间隔不会覆盖现有配置。
func TestSetWatchIntervalIgnoresNonPositive(t *testing.T) {
	source := NewJSONSource("config.json", WithWatchInterval(3*time.Second))
	if got := time.Duration(source.interval.Load()); got != 3*time.Second {
		t.Fatalf("构造后的间隔 = %v, want 3s", got)
	}

	source.setWatchInterval(0)
	source.setWatchInterval(-time.Second)
	if got := time.Duration(source.interval.Load()); got != 3*time.Second {
		t.Fatalf("非正间隔不应覆盖, got %v", got)
	}

	source.setWatchInterval(5 * time.Millisecond)
	if got := time.Duration(source.interval.Load()); got != 5*time.Millisecond {
		t.Fatalf("正间隔应生效, got %v", got)
	}
}
