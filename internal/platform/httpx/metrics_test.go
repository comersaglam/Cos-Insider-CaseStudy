package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHTTPMetricsCountRequestsByRoute(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewHTTPMetrics(reg)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := m.Middleware(ProblemFallback(mux))

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/v1/things/1"}, {http.MethodGet, "/v1/things/2"}, // one route, two IDs
		{http.MethodGet, "/v1/nothing"},
		{"FOO", "/v1/things/3"}, // a method nobody defined
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), r.method, r.path, nil))
	}

	tests := []struct {
		method, route, status string
		want                  float64
	}{
		{"GET", "/v1/things/{id}", "200", 2}, // the route, never the path, so IDs make no new series
		{"GET", "unmatched", "404", 1},
		{"OTHER", "unmatched", "405", 1},
	}
	for _, tt := range tests {
		if got := testutil.ToFloat64(m.requests.WithLabelValues(tt.method, tt.route, tt.status)); got != tt.want {
			t.Errorf("requests{%s %s %s} = %v, want %v", tt.method, tt.route, tt.status, got, tt.want)
		}
	}
	if n := testutil.CollectAndCount(m.requests); n != 3 {
		t.Errorf("%d request series, want 3", n)
	}
	if n := testutil.CollectAndCount(m.duration); n != 3 {
		t.Errorf("%d duration series, want 3", n)
	}
}
