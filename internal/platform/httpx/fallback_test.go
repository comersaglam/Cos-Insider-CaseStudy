package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProblemFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := ProblemFallback(mux)

	tests := []struct {
		name, method, path string
		status             int
		code, allow        string
	}{
		{"a route answers", http.MethodGet, "/v1/things/1", http.StatusOK, "", ""},
		{"an unknown path", http.MethodGet, "/v1/nothing", http.StatusNotFound, "not_found", ""},
		{"a known path with another method", http.MethodDelete, "/v1/things/1", http.StatusMethodNotAllowed,
			"method_not_allowed", "GET, HEAD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.code == "" {
				return
			}
			if p := decode(t, rec); p.Code != tt.code {
				t.Errorf("code = %q, want %q", p.Code, tt.code)
			}
			if got := rec.Header().Get("Allow"); got != tt.allow {
				t.Errorf("Allow = %q, want %q", got, tt.allow)
			}
		})
	}
}

// TestProblemFallbackLeavesRedirectsToTheMux checks that an unclean path is still redirected to its clean form
// exactly as the mux alone would redirect it, whatever status the Go version uses for that.
func TestProblemFallbackLeavesRedirectsToTheMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(http.ResponseWriter, *http.Request) {})

	alone, through := httptest.NewRecorder(), httptest.NewRecorder()
	mux.ServeHTTP(alone, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1//nothing", nil))
	ProblemFallback(mux).ServeHTTP(through, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1//nothing", nil))

	if alone.Code < 300 || alone.Code > 399 {
		t.Fatalf("the mux answered %d, not a redirect; the test no longer exercises a redirect", alone.Code)
	}
	if through.Code != alone.Code || through.Header().Get("Location") != alone.Header().Get("Location") {
		t.Errorf("through the fallback: %d %q; the mux alone: %d %q",
			through.Code, through.Header().Get("Location"), alone.Code, alone.Header().Get("Location"))
	}
}
