package cache

import (
	"net/http"
	"testing"
	"time"
)

// newTestEntry 构造一条测试条目。
func newTestEntry(key string, size int) *entry {
	return &entry{
		key:    key,
		status: http.StatusOK,
		header: http.Header{"Content-Type": []string{"text/plain"}},
		body:   make([]byte, size),
	}
}

func TestStoreEvictsLeastRecentlyUsed(t *testing.T) {
	// 容量 20 字节，每条 10 字节，最多容纳 2 条。
	storage := newStore(time.Minute, 0, 20)
	storage.put(newTestEntry("a", 10))
	storage.put(newTestEntry("b", 10))

	// 访问 a 使其成为最新，随后 c 的写入应淘汰 b。
	if _, fresh, found := storage.get("a", time.Now()); !found || !fresh {
		t.Fatal("a 应命中")
	}

	storage.put(newTestEntry("c", 10))

	if _, _, found := storage.get("b", time.Now()); found {
		t.Fatal("b 应被淘汰")
	}

	if _, _, found := storage.get("a", time.Now()); !found {
		t.Fatal("a 应保留")
	}

	if storage.len() != 2 {
		t.Fatalf("条目数 = %d, want 2", storage.len())
	}
}

func TestStoreRejectsOversizedEntry(t *testing.T) {
	storage := newStore(time.Minute, 0, 10)
	storage.put(newTestEntry("big", 11))

	if storage.len() != 0 {
		t.Fatalf("条目数 = %d, want 0", storage.len())
	}
}

func TestStoreReportsStaleThenRemoves(t *testing.T) {
	storage := newStore(10*time.Millisecond, 20*time.Millisecond, 1024)
	storage.put(newTestEntry("a", 1))

	time.Sleep(15 * time.Millisecond)
	if _, fresh, found := storage.get("a", time.Now()); !found || fresh {
		t.Fatal("应命中陈旧条目")
	}

	time.Sleep(25 * time.Millisecond)
	if _, _, found := storage.get("a", time.Now()); found {
		t.Fatal("彻底过期后应删除")
	}

	if storage.len() != 0 {
		t.Fatalf("条目数 = %d, want 0", storage.len())
	}
}

func TestStoreRefreshDeduplicates(t *testing.T) {
	storage := newStore(time.Minute, time.Minute, 1024)

	if !storage.beginRefresh("a") {
		t.Fatal("首次刷新应成功占位")
	}

	if storage.beginRefresh("a") {
		t.Fatal("重复刷新应被拒绝")
	}

	storage.endRefresh("a")
	if !storage.beginRefresh("a") {
		t.Fatal("刷新结束后应可再次占位")
	}
}

func TestBuildKeyIncludesVaryAndQuery(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://leego.local/data?page=2", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}

	request.Header.Set("Authorization", "Bearer alice")
	base := buildKey(request, []string{"Authorization"})

	request.Header.Set("Authorization", "Bearer bob")
	if other := buildKey(request, []string{"Authorization"}); other == base {
		t.Fatal("不同 Authorization 必须生成不同缓存键")
	}

	otherQuery, err := http.NewRequest(http.MethodGet, "http://leego.local/data?page=3", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}

	otherQuery.Header.Set("Authorization", "Bearer alice")
	if buildKey(otherQuery, []string{"Authorization"}) == base {
		t.Fatal("不同查询串必须生成不同缓存键")
	}

	if len(base) != _KEY_LENGTH*2 {
		t.Fatalf("缓存键长度 = %d, want %d", len(base), _KEY_LENGTH*2)
	}
}

func TestForbidsCache(t *testing.T) {
	cases := map[string]bool{
		"":                            false,
		"public, max-age=60":          false,
		"no-store":                    true,
		"private, max-age=10":         true,
		"Public, No-Cache":            true,
		"max-age=60, must-revalidate": false,
	}

	for value, want := range cases {
		if got := forbidsCache(value); got != want {
			t.Fatalf("forbidsCache(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestConfigNormalize(t *testing.T) {
	config := Config{MaxBody: -1}.normalize()

	if config.TTL <= 0 || config.MaxBytes <= 0 {
		t.Fatalf("默认值未填充: %+v", config)
	}

	if config.MaxBody != 0 {
		t.Fatalf("MaxBody = %d, want 0（不限制）", config.MaxBody)
	}

	if len(config.Vary) == 0 || len(config.Methods) == 0 {
		t.Fatalf("Vary/Methods 未填充默认值: %+v", config)
	}
}
