package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbe(t *testing.T) {
	for _, tt := range []struct {
		status int
		ok     bool
	}{{200, true}, {503, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) }))
		addr := strings.TrimPrefix(srv.URL, "http://")
		if err := Probe(t.Context(), addr, "/readyz"); (err == nil) != tt.ok {
			t.Errorf("Probe against %d = %v", tt.status, err)
		}
		srv.Close()
	}
}

func TestLocalURL(t *testing.T) {
	for addr, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/readyz",
		"0.0.0.0:9090":   "http://127.0.0.1:9090/readyz",
		"127.0.0.1:8081": "http://127.0.0.1:8081/readyz",
		"[::]:8080":      "http://127.0.0.1:8080/readyz",
	} {
		if got, err := localURL(addr, "/readyz"); err != nil || got != want {
			t.Errorf("localURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
}
