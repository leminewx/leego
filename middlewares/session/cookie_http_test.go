package session

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"gitee.com/leminewx/leego"
)

// TestCookieSurvivesRealHTTP 验证真实 HTTP 服务器下 Cookie 能正常下发。
//
// net/http 在 WriteHeader 之后会忽略响应头修改，而会话持久化发生在处理函数
// 之后；这里通过带 Cookie 容器的客户端跑完整往返，防止回归。
func TestCookieSurvivesRealHTTP(t *testing.T) {
	store := NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	engine := newSessionEngine(store, WithSecret([]byte(_TEST_SECRET)))
	engine.GET("/login", func(ctx *leego.Context) {
		current, ok := GetFromContext(ctx.Request.Context())
		if !ok {
			ctx.ResponseError(http.StatusInternalServerError, "missing session")
			return
		}

		current.Set("name", "leminewx")
		_ = ctx.ResponseText(http.StatusOK, current.GetID())
	})
	engine.GET("/me", func(ctx *leego.Context) {
		current, ok := GetFromContext(ctx.Request.Context())
		if !ok {
			ctx.ResponseError(http.StatusInternalServerError, "missing session")
			return
		}

		value, _ := current.Get("name")
		name, _ := value.(string)
		_ = ctx.ResponseText(http.StatusOK, name)
	})

	server := httptest.NewServer(engine.GetHTTPHandler())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 Cookie 容器失败: %v", err)
	}

	client := &http.Client{Jar: jar}

	login, err := client.Get(server.URL + "/login")
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}
	loginBody, _ := io.ReadAll(login.Body)
	login.Body.Close()

	if login.StatusCode != http.StatusOK || len(loginBody) == 0 {
		t.Fatalf("登录响应异常: %d %s", login.StatusCode, loginBody)
	}

	if cookies := jar.Cookies(login.Request.URL); len(cookies) == 0 {
		t.Fatal("真实 HTTP 响应未下发会话 Cookie")
	}

	me, err := client.Get(server.URL + "/me")
	if err != nil {
		t.Fatalf("会话读取请求失败: %v", err)
	}
	defer me.Body.Close()

	body, _ := io.ReadAll(me.Body)
	if string(body) != "leminewx" {
		t.Fatalf("会话数据 = %q, want leminewx", body)
	}
}
