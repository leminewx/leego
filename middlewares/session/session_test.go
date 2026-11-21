package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/leminewx/leego"
)

const _TEST_SECRET = "0123456789abcdef0123456789abcdef"

// countingStore 统计写回次数，用于验证"未修改不写回"。
type countingStore struct {
	// MemoryStore 为底层存储。
	MemoryStore *MemoryStore
	// saves 为 Save 调用次数。
	saves atomic.Int64
	// destroys 为 Destroy 调用次数。
	destroys atomic.Int64
}

// Get 透传读取。
func (own *countingStore) Get(ctx context.Context, id string) (Values, error) {
	return own.MemoryStore.Get(ctx, id)
}

// Save 记录写入次数后透传。
func (own *countingStore) Save(ctx context.Context, id string, values Values, ttl time.Duration) error {
	own.saves.Add(1)
	return own.MemoryStore.Save(ctx, id, values, ttl)
}

// Destroy 记录销毁次数后透传。
func (own *countingStore) Destroy(ctx context.Context, id string) error {
	own.destroys.Add(1)
	return own.MemoryStore.Destroy(ctx, id)
}

// newSessionEngine 构造带会话中间件的引擎。
func newSessionEngine(store Store, options ...Option) *leego.Engine {
	engine := leego.NewEngine(leego.DisableStartupLog())
	// 默认注入测试密钥；调用方显式传入的选项排在后面，可以覆盖默认值。
	settings := append([]Option{WithSecret([]byte(_TEST_SECRET))}, options...)
	engine.UseMiddlewares(New(store, settings...).AdaptToMiddleware())
	return engine
}

// findCookie 从响应中取出指定 Cookie。
func findCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()

	response := &http.Response{Header: recorder.Header()}
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}

// doRequest 携带可选 Cookie 发起一次请求。
func doRequest(engine *leego.Engine, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestCookieRoundTrip(t *testing.T) {
	secret := []byte(_TEST_SECRET)

	value := encodeCookie(secret, "session-id")
	id, ok := decodeCookie(secret, value)
	if !ok || id != "session-id" {
		t.Fatalf("解码结果 = %q, %v", id, ok)
	}

	if _, ok = decodeCookie([]byte("another-secret-another-secret-0000"), value); ok {
		t.Fatal("密钥不匹配时必须拒绝")
	}

	if _, ok = decodeCookie(secret, value+"x"); ok {
		t.Fatal("签名被篡改时必须拒绝")
	}

	if _, ok = decodeCookie(secret, "no-separator"); ok {
		t.Fatal("缺少分隔符时必须拒绝")
	}

	if _, ok = decodeCookie(secret, ""); ok {
		t.Fatal("空值必须拒绝")
	}

	if got := encodeCookie(secret, ""); got != "" {
		t.Fatalf("空 ID 编码 = %q, want 空字符串", got)
	}
}

func TestSessionPersistsAcrossRequests(t *testing.T) {
	store := &countingStore{MemoryStore: NewMemoryStore()}
	t.Cleanup(func() { _ = store.MemoryStore.Close() })

	engine := newSessionEngine(store,
		WithSecret([]byte(_TEST_SECRET)),
		WithTTL(time.Hour),
	)
	engine.GET("/set", func(ctx *leego.Context) {
		current, ok := GetFromContext(ctx.Request.Context())
		if !ok {
			t.Error("处理函数中缺少会话")
			return
		}

		if !current.IsNew() {
			t.Error("首次请求应是新会话")
		}

		current.Set("user", "leminewx")
		_ = ctx.ResponseText(http.StatusOK, current.GetID())
	})
	engine.GET("/get", func(ctx *leego.Context) {
		current, ok := GetFromContext(ctx.Request.Context())
		if !ok {
			t.Error("处理函数中缺少会话")
			return
		}

		value, _ := current.Get("user")
		text, _ := value.(string)
		_ = ctx.ResponseText(http.StatusOK, text)
	})

	first := doRequest(engine, "/set", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("首次请求状态码 = %d", first.Code)
	}

	cookie := findCookie(t, first, _DEFAULT_NAME)
	if cookie == nil {
		t.Fatal("首次请求必须下发会话 Cookie")
	}

	if !cookie.HttpOnly {
		t.Fatal("Cookie 必须设置 HttpOnly")
	}

	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", cookie.SameSite)
	}

	if cookie.Path != "/" {
		t.Fatalf("Path = %q, want /", cookie.Path)
	}

	if cookie.MaxAge <= 0 {
		t.Fatalf("MaxAge = %d, want 正数", cookie.MaxAge)
	}

	second := doRequest(engine, "/get", cookie)
	if body := second.Body.String(); body != "leminewx" {
		t.Fatalf("第二次请求读到 %q, want leminewx", body)
	}

	// 未修改的请求不应再次写回。
	if saves := store.saves.Load(); saves != 1 {
		t.Fatalf("Save 次数 = %d, want 1", saves)
	}

	secondCookie := findCookie(t, second, _DEFAULT_NAME)
	if secondCookie != nil {
		t.Fatal("未修改会话不应刷新 Cookie")
	}
}

func TestSessionRegenerate(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	engine := newSessionEngine(store, WithSecret([]byte(_TEST_SECRET)))
	engine.GET("/init", func(ctx *leego.Context) {
		current, _ := GetFromContext(ctx.Request.Context())
		current.Set("stage", 1)
		_ = ctx.ResponseText(http.StatusOK, current.GetID())
	})
	engine.GET("/login", func(ctx *leego.Context) {
		current, _ := GetFromContext(ctx.Request.Context())
		current.Set("user", "leminewx")

		if err := current.Regenerate(ctx.Request.Context()); err != nil {
			t.Errorf("Regenerate() error = %v", err)
		}

		_ = ctx.ResponseText(http.StatusOK, current.GetID())
	})

	first := doRequest(engine, "/init", nil)
	sessionCookie := findCookie(t, first, _DEFAULT_NAME)
	if sessionCookie == nil {
		t.Fatal("必须下发会话 Cookie")
	}

	oldID, ok := decodeCookie([]byte(_TEST_SECRET), sessionCookie.Value)
	if !ok {
		t.Fatal("Cookie 无法解码")
	}

	second := doRequest(engine, "/login", sessionCookie)
	newCookie := findCookie(t, second, _DEFAULT_NAME)
	if newCookie == nil {
		t.Fatal("轮换后必须下发新 Cookie")
	}

	newID, ok := decodeCookie([]byte(_TEST_SECRET), newCookie.Value)
	if !ok || newID == oldID {
		t.Fatalf("轮换后 ID 未变化: old=%q new=%q", oldID, newID)
	}

	if _, getErr := store.Get(context.Background(), oldID); !errors.Is(getErr, ErrNotFound) {
		t.Fatalf("轮换后旧 ID 查询错误 = %v, want ErrNotFound", getErr)
	}

	values, getErr := store.Get(context.Background(), newID)
	if getErr != nil {
		t.Fatalf("新 ID 查询错误 = %v", getErr)
	}

	if values["user"] != "leminewx" || values["stage"] != 1 {
		t.Fatalf("轮换后数据丢失: %v", values)
	}
}

func TestSessionDestroy(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	engine := newSessionEngine(store, WithSecret([]byte(_TEST_SECRET)))
	engine.GET("/logout", func(ctx *leego.Context) {
		current, _ := GetFromContext(ctx.Request.Context())
		current.Set("user", "leminewx")
		current.Destroy()
		_ = ctx.ResponseText(http.StatusOK, "bye")
	})

	first := doRequest(engine, "/logout", nil)
	cookie := findCookie(t, first, _DEFAULT_NAME)
	if cookie == nil {
		t.Fatal("销毁会话时必须下发清理 Cookie")
	}

	if cookie.MaxAge != -1 {
		t.Fatalf("清理 Cookie MaxAge = %d, want -1", cookie.MaxAge)
	}

	if store.Count() != 0 {
		t.Fatalf("存储中仍有 %d 个会话", store.Count())
	}
}

func TestSessionRejectsTamperedCookie(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	engine := newSessionEngine(store, WithSecret([]byte(_TEST_SECRET)))
	engine.GET("/whoami", func(ctx *leego.Context) {
		current, _ := GetFromContext(ctx.Request.Context())
		if value, ok := current.Get("user"); ok {
			_ = ctx.ResponseText(http.StatusOK, value.(string))
			return
		}

		_ = ctx.ResponseText(http.StatusOK, "")
	})

	request := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	request.AddCookie(&http.Cookie{Name: _DEFAULT_NAME, Value: "Zm9yZ2Vk.signature"})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if body := recorder.Body.String(); body != "" {
		t.Fatalf("伪造 Cookie 不应携带数据，实际 = %q", body)
	}

	if cookie := findCookie(t, recorder, _DEFAULT_NAME); cookie == nil {
		t.Fatal("伪造 Cookie 必须被替换为新会话")
	}
}

func TestSessionFromWithoutMiddleware(t *testing.T) {
	if _, ok := GetFromContext(context.Background()); ok {
		t.Fatal("无中间件时应返回 false")
	}

	if _, ok := GetFromContext(nil); ok {
		t.Fatal("nil 上下文应返回 false")
	}

	if _, ok := GetFromRequest(nil); ok {
		t.Fatal("nil 请求应返回 false")
	}
}

func TestNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil 存储必须 panic")
		}
	}()

	_ = New(nil)
}

func TestMemoryStoreExpiry(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.Save(ctx, "id-1", Values{"user": "leego"}, 20*time.Millisecond); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	values, err := store.Get(ctx, "id-1")
	if err != nil || values["user"] != "leego" {
		t.Fatalf("Get() = %v, %v", values, err)
	}

	time.Sleep(40 * time.Millisecond)

	if _, err = store.Get(ctx, "id-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期会话错误 = %v, want ErrNotFound", err)
	}

	if store.Count() != 0 {
		t.Fatalf("过期条目未被删除，Count = %d", store.Count())
	}

	if _, err = store.Get(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空 ID 错误 = %v, want ErrNotFound", err)
	}

	if err = store.Save(ctx, "", Values{}, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空 ID 写入错误 = %v, want ErrNotFound", err)
	}
}
