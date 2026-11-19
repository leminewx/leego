package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/leminewx/leego/middleware/collector"
)

// TestPrometheusHandlerWithoutSource 验证未接线时输出空指标集而不是报错。
func TestPrometheusHandlerWithoutSource(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewPrometheusHandler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics/prometheus", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != _PROMETHEUS_CONTENT_TYPE {
		t.Fatalf("Content-Type = %q, want %q", contentType, _PROMETHEUS_CONTENT_TYPE)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"# TYPE leego_http_requests_total counter",
		"# TYPE leego_http_request_duration_seconds summary",
		"# TYPE leego_http_requests_in_flight gauge",
		"# TYPE leego_http_requests_failed_total counter",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("输出缺少 %q\n%s", want, body)
		}
	}

	if strings.Contains(body, "leego_http_requests_total{") {
		t.Fatalf("空报告不应输出样本行\n%s", body)
	}
}

// TestFormatSeconds 验证微秒到秒的换算精度。
func TestFormatSeconds(t *testing.T) {
	cases := map[int64]string{
		0:       "0.000000",
		1_500:   "0.001500",
		500_000: "0.500000",
		_SECOND: "1.000000",
	}

	for micro, want := range cases {
		if got := formatSeconds(micro); got != want {
			t.Fatalf("formatSeconds(%d) = %q, want %q", micro, got, want)
		}
	}
}

// TestRouteLabelsWithoutName 验证名称为空时不输出 name 标签。
func TestRouteLabelsWithoutName(t *testing.T) {
	labels := routeLabels(collector.Snapshot{Method: http.MethodGet, Pattern: "/users/:id"})
	if want := `method="GET",pattern="/users/:id"`; labels != want {
		t.Fatalf("routeLabels() = %q, want %q", labels, want)
	}

	withName := routeLabels(collector.Snapshot{Method: http.MethodGet, Pattern: "/ping", Name: "ping"})
	if want := `method="GET",pattern="/ping",name="ping"`; withName != want {
		t.Fatalf("routeLabels() = %q, want %q", withName, want)
	}
}
