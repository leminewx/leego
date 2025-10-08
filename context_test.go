package leego

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkContext_NewContext(b *testing.B) {
	engine := NewEngine("test")
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/users/123", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = newContext(engine, resp, req, nil)
	}
}
