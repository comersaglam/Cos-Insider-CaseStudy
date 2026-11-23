package health

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/api/contract"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

func serve(t *testing.T, h *Handler, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	mux.ServeHTTP(rec, r)
	contract.Check(t, r, rec.Code, rec.Header(), rec.Body.Bytes())
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: not JSON: %s", path, rec.Body)
	}
	return rec, body
}

var discard = slog.New(slog.DiscardHandler)

func ok(context.Context) error { return nil }

func TestReadyWhenEveryDependencyAnswers(t *testing.T) {
	rec, body := serve(t, NewHandler(map[string]Check{"postgres": ok, "rabbitmq": ok}, discard), "/readyz")
	if rec.Code != http.StatusOK || body["status"] != "ready" || fmt.Sprint(body["checks"]) != "map[postgres:ok rabbitmq:ok]" {
		t.Errorf("readyz = %d %v", rec.Code, body)
	}
}

// TestNotReadyNamesTheDependencyAndLogsWhy: the answer is the usual problem and names what failed; the error itself,
// which may hold a host or a user name, goes to the log only.
func TestNotReadyNamesTheDependencyAndLogsWhy(t *testing.T) {
	var logs bytes.Buffer
	down := func(context.Context) error { return errors.New("dial tcp 10.0.0.7:5672: connection refused") }
	h := NewHandler(map[string]Check{"postgres": ok, "rabbitmq": down}, observability.NewLogger(&logs, slog.LevelInfo))
	rec, body := serve(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Content-Type") != "application/problem+json" ||
		body["code"] != "not_ready" || body["detail"] != "rabbitmq did not answer" {
		t.Errorf("readyz = %d %s %v", rec.Code, rec.Header().Get("Content-Type"), body)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Errorf("the answer reveals the error: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), `"check":"rabbitmq"`) || !strings.Contains(logs.String(), "connection refused") {
		t.Errorf("the failure is not logged:\n%s", logs.String())
	}
}

// TestNotReadyWhenADependencyHangs: a check that does not answer within the timeout fails, so the probe itself
// answers in time.
func TestNotReadyWhenADependencyHangs(t *testing.T) {
	hang := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	h := NewHandler(map[string]Check{"postgres": hang}, discard)
	h.timeout = 50 * time.Millisecond
	start := time.Now()
	rec, _ := serve(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable || time.Since(start) > time.Second {
		t.Errorf("readyz = %d after %v, want 503 within the timeout", rec.Code, time.Since(start))
	}
}

// TestAliveWhateverTheDependencies: liveness says the process runs; restarting it would not bring a database back.
func TestAliveWhateverTheDependencies(t *testing.T) {
	down := func(context.Context) error { return errors.New("down") }
	if rec, body := serve(t, NewHandler(map[string]Check{"postgres": down}, discard), "/healthz"); rec.Code != http.StatusOK || body["status"] != "ok" {
		t.Errorf("healthz = %d %v", rec.Code, body)
	}
}
