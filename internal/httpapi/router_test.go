package httpapi

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/health"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// noNotifications is a notification service that knows no notification. Embedding *notification.Service gives
// it every method the handlers need, however many there are; only Get and GetBatch are called here, and both are
// overridden, so the nil service underneath is never reached.
type noNotifications struct {
	*notification.Service
}

func (noNotifications) Get(context.Context, uuid.UUID) (notification.Notification, error) {
	return notification.Notification{}, notification.ErrNotFound
}

func (noNotifications) GetBatch(context.Context, uuid.UUID) (notification.Batch, error) {
	return notification.Batch{}, notification.ErrNotFound
}

// noProbes is a health handler with no dependency to check: always ready.
func noProbes(log *slog.Logger) *health.Handler { return health.NewHandler(nil, log) }

func TestRouter(t *testing.T) {
	tests := []struct {
		name, method, path string
		status             int
		code               string
	}{
		{"a module's route", http.MethodGet, "/v1/notifications/" + uuid.NewV7().String(), 404, "not_found"},
		{"another of its routes", http.MethodGet, "/v1/batches/" + uuid.NewV7().String(), 404, "not_found"},
		{"an unknown path", http.MethodGet, "/v1/unknown", 404, "not_found"},
		{"a known path with another method", http.MethodDelete, "/v1/notifications/x", 405, "method_not_allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := observability.NewLogger(&logs, slog.LevelInfo)
			h := NewRouter(log, observability.NewRegistry(), noProbes(log), notification.NewHandler(noNotifications{}, log, 1<<20))

			r := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			r.Header.Set(observability.CorrelationHeader, "trace-me")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			var p map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
				t.Fatalf("not JSON: %s", rec.Body)
			}
			if rec.Code != tt.status || p["code"] != tt.code {
				t.Errorf("got %d %v, want %d %s", rec.Code, p["code"], tt.status, tt.code)
			}
			// The middleware chain runs for every request, routed or not.
			if rec.Header().Get(observability.CorrelationHeader) != "trace-me" || p["correlationId"] != "trace-me" {
				t.Errorf("correlation ID not carried: header %q, body %v", rec.Header().Get(observability.CorrelationHeader), p["correlationId"])
			}
			if !strings.Contains(logs.String(), `"msg":"http request"`) || !strings.Contains(logs.String(), `"correlation_id":"trace-me"`) {
				t.Errorf("no access log line with the correlation ID:\n%s", logs.String())
			}
		})
	}
}

func TestRouterServesMetrics(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	h := NewRouter(log, observability.NewRegistry(), noProbes(log), notification.NewHandler(noNotifications{}, log, 1<<20))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/unknown", nil))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `notifications_http_requests_total{method="GET",route="unmatched",status="404"} 1`) {
		t.Errorf("GET /metrics = %d, want the request just made counted:\n%.500s", rec.Code, rec.Body)
	}
}

// TestRouterServesProbes: the probes are mounted, and a successful one stays out of the access log at info level.
func TestRouterServesProbes(t *testing.T) {
	var logs bytes.Buffer
	log := observability.NewLogger(&logs, slog.LevelInfo)
	down := health.NewHandler(map[string]health.Check{"postgres": func(context.Context) error { return errors.New("down") }}, log)
	for _, tt := range []struct {
		name   string
		probes *health.Handler
		path   string
		status int
		logged bool
	}{
		{"liveness", down, "/healthz", http.StatusOK, false},
		{"readiness", noProbes(log), "/readyz", http.StatusOK, false},
		{"failed readiness", down, "/readyz", http.StatusServiceUnavailable, true},
	} {
		logs.Reset()
		h := NewRouter(log, observability.NewRegistry(), tt.probes, notification.NewHandler(noNotifications{}, log, 1<<20))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil))
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d", tt.name, rec.Code, tt.status)
		}
		if logged := strings.Contains(logs.String(), `"msg":"http request"`); logged != tt.logged {
			t.Errorf("%s: access logged %v, want %v:\n%s", tt.name, logged, tt.logged, logs.String())
		}
	}
}
