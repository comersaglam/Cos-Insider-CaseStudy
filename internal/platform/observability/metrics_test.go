package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerServesTheRegistry(t *testing.T) {
	reg := NewRegistry()
	rec := httptest.NewRecorder()
	MetricsHandler(reg).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))

	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), "go_goroutines") || !strings.Contains(string(body), "process_") {
		t.Errorf("got %d, want the Go runtime's and the process's metrics:\n%.300s", rec.Code, body)
	}
}
