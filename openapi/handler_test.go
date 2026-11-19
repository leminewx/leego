package openapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego/openapi"
)

// TestOpenAPIHandlerMinimalDocument 验证未提供文档时输出最小可用文档。
func TestOpenAPIHandlerMinimalDocument(t *testing.T) {
	recorder := httptest.NewRecorder()
	openapi.NewHandler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("Content-Type = %q", contentType)
	}

	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}

	var document map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}

	if document["openapi"] != "3.1.0" {
		t.Fatalf("openapi 版本 = %v", document["openapi"])
	}
}

// TestOpenAPIUIHandlerServesEmbeddedPage 验证文档页面内嵌且禁止缓存。
func TestOpenAPIUIHandlerServesEmbeddedPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	openapi.NewUIHandler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q", contentType)
	}

	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}

	if body := recorder.Body.String(); !strings.Contains(body, "openapi.json") {
		t.Fatalf("页面未引用 openapi.json: %s", body)
	}
}
