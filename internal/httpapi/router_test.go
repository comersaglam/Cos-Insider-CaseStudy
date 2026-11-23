package httpapi

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

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
			h := NewRouter(log, notification.NewHandler(noNotifications{}, log, 1<<20))

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
