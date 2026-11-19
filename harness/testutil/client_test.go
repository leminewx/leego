package testutil_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"gitee.com/leminewx/leego/harness/testutil"
)

// newDemoHandler 构造覆盖多种响应形态的处理器。
func newDemoHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/ping", func(resp http.ResponseWriter, req *http.Request) {
		resp.Header().Set("X-Trace", "abc")
		_, _ = resp.Write([]byte("pong"))
	})

	mux.HandleFunc("/echo", func(resp http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
		}

		_ = json.NewEncoder(resp).Encode(map[string]string{
			"method": req.Method,
			"query":  req.URL.Query().Get("q"),
			"header": req.Header.Get("X-Token"),
			"body":   string(body),
			"cookie": req.Header.Get("Cookie"),
		})
	})

	mux.HandleFunc("/cookie", func(resp http.ResponseWriter, req *http.Request) {
		http.SetCookie(resp, &http.Cookie{Name: "sid", Value: "v1", Path: "/"})
		resp.WriteHeader(http.StatusCreated)
	})

	return mux
}

func TestClientGet(t *testing.T) {
	client := testutil.NewClient(newDemoHandler())
	response := client.Get("/ping").AssertStatus(t, http.StatusOK).AssertHeader(t, "X-Trace", "abc")
	response.AssertContains(t, "pong")

	if got := response.String(); got != "pong" {
		t.Fatalf("响应体 = %q, 期望 %q", got, "pong")
	}
}

func TestClientJSONAndOptions(t *testing.T) {
	client := testutil.NewClient(newDemoHandler()).UseHeader("X-Token", "default")

	response := client.JSON(http.MethodPost, "/echo?q=keep", map[string]string{"name": "leego"},
		testutil.WithQuery("q", "added"),
		testutil.WithCookie(&http.Cookie{Name: "sid", Value: "v1"}),
	)
	response.AssertStatus(t, http.StatusOK)

	payload := response.JSONMap(t)
	if payload["method"] != http.MethodPost {
		t.Fatalf("method = %v", payload["method"])
	}

	// 默认头与查询参数都应生效。
	if payload["header"] != "default" {
		t.Fatalf("header = %v", payload["header"])
	}

	if payload["query"] != "keep" {
		t.Fatalf("query = %v", payload["query"])
	}

	if payload["body"] != `{"name":"leego"}` {
		t.Fatalf("body = %v", payload["body"])
	}

	if payload["cookie"] == "" {
		t.Fatal("默认 Cookie 未发送")
	}
}

func TestClientWithoutLeadingSlash(t *testing.T) {
	client := testutil.NewClient(newDemoHandler())
	client.Get("ping").AssertStatus(t, http.StatusOK)
}

func TestClientCookie(t *testing.T) {
	client := testutil.NewClient(newDemoHandler())
	response := client.Post("/cookie", nil).AssertStatus(t, http.StatusCreated)

	cookie, ok := response.GetCookie("sid")
	if !ok || cookie.Value != "v1" {
		t.Fatalf("Cookie = %v, ok = %v", cookie, ok)
	}
}

func TestClientNilHandler(t *testing.T) {
	client := testutil.NewClient(nil)
	client.Get("/").AssertStatus(t, http.StatusInternalServerError)
}

func TestClientJSONMarshalError(t *testing.T) {
	client := testutil.NewClient(newDemoHandler())
	response := client.JSON(http.MethodPost, "/echo", make(chan int))
	if response.Err == nil {
		t.Fatal("期望序列化失败")
	}
}

func TestResponseBindJSON(t *testing.T) {
	client := testutil.NewClient(newDemoHandler())
	response := client.JSON(http.MethodGet, "/echo", nil)

	var payload map[string]string
	response.BindJSON(t, &payload)
	if payload["method"] != http.MethodGet {
		t.Fatalf("method = %q", payload["method"])
	}
}

func TestFrozenClock(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	clock := testutil.NewFrozenClock(start)

	if !clock.Now().Equal(start) {
		t.Fatalf("Now = %v, 期望 %v", clock.Now(), start)
	}

	clock.Advance(time.Minute)
	if got := clock.Now().Sub(start); got != time.Minute {
		t.Fatalf("推进后差值 = %v", got)
	}

	clock.Set(time.Time{})
	if !clock.Now().Equal(start.Add(time.Minute)) {
		t.Fatal("零值时间应当被忽略")
	}

	if !testutil.NewFrozenClock(time.Time{}).Now().Equal(time.Unix(0, 0).UTC()) {
		t.Fatal("零值时间应回退到 Unix 纪元")
	}
}
