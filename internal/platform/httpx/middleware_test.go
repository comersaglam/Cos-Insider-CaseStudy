package httpx

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

func TestCorrelation(t *testing.T) {
	tests := []struct {
		name, header string
		keep         bool
	}{
		{"a valid ID is kept", "checkout-42", true},
		{"a missing ID is generated", "", false},
		{"an invalid ID is replaced", "has space", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			h := Correlation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = observability.CorrelationID(r.Context())
			}))
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set(observability.CorrelationHeader, tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			answered := rec.Header().Get(observability.CorrelationHeader)
			if seen == "" || seen != answered {
				t.Errorf("handler saw %q, response carries %q; want one non-empty ID", seen, answered)
			}
			if tt.keep != (seen == tt.header) {
				t.Errorf("ID = %q from header %q, keep = %v", seen, tt.header, tt.keep)
			}
			if !observability.ValidCorrelationID(seen) {
				t.Errorf("ID %q is not valid", seen)
			}
		})
	}
}

func TestRecoverAnswersAPanicWith500(t *testing.T) {
	var logs bytes.Buffer
	h := Recover(observability.NewLogger(&logs, slog.LevelInfo))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("nil map write")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if p := decode(t, rec); rec.Code != http.StatusInternalServerError || p.Code != "internal_error" {
		t.Errorf("got %d %+v, want 500 internal_error", rec.Code, p)
	}
	if !strings.Contains(logs.String(), "nil map write") || !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("the panic was not logged as an error: %s", logs.String())
	}
}

// TestRecoverLetsAbortHandlerThrough keeps http.ErrAbortHandler working: net/http uses that panic to abort a
// response on purpose, and expects it to reach the server.
func TestRecoverLetsAbortHandlerThrough(t *testing.T) {
	h := Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if v, _ := recover().(error); !errors.Is(v, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

func TestAccessLog(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantLevel string
	}{
		{"success", http.StatusAccepted, "INFO"},
		{"client error", http.StatusNotFound, "INFO"},
		{"server error", http.StatusServiceUnavailable, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := Correlation(AccessLog(observability.NewLogger(&logs, slog.LevelInfo))(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte("hello"))
				})))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/notifications?x=1", nil))

			var line map[string]any
			if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
				t.Fatalf("access log is not one JSON line: %v\n%s", err, logs.String())
			}
			for key, want := range map[string]any{
				"level": tt.wantLevel, "method": "POST", "path": "/v1/notifications",
				"status": float64(tt.status), "bytes": float64(5),
			} {
				if line[key] != want {
					t.Errorf("%s = %v, want %v", key, line[key], want)
				}
			}
			if _, ok := line["duration_ms"]; !ok || line["correlation_id"] == nil {
				t.Errorf("access log lacks duration_ms or correlation_id: %v", line)
			}
		})
	}
}

func TestAccessLogCountsAnUnwrittenResponseAs200(t *testing.T) {
	var logs bytes.Buffer
	h := AccessLog(observability.NewLogger(&logs, slog.LevelInfo))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if !strings.Contains(logs.String(), `"status":200`) {
		t.Errorf("access log = %s, want status 200", logs.String())
	}
}

func TestRecoverPassesANormalResponseThrough(t *testing.T) {
	h := Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

// TestRecorderCountsABodyWrittenWithoutAHeader matches net/http, which sends 200 on the first Write.
func TestRecorderCountsABodyWrittenWithoutAHeader(t *testing.T) {
	var logs bytes.Buffer
	h := AccessLog(observability.NewLogger(&logs, slog.LevelInfo))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if !strings.Contains(logs.String(), `"status":200`) || !strings.Contains(logs.String(), `"bytes":2`) {
		t.Errorf("access log = %s, want status 200 and 2 bytes", logs.String())
	}
}

// TestRecorderKeepsFlushingAvailable checks Unwrap: without it, http.ResponseController cannot reach the
// writer below the recorder, and Flush fails with http.ErrNotSupported.
func TestRecorderKeepsFlushingAvailable(t *testing.T) {
	var flushErr error
	h := AccessLog(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flushErr = http.NewResponseController(w).Flush()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if flushErr != nil {
		t.Errorf("Flush through the recorder = %v, want nil", flushErr)
	}
}

// TestAccessLogQuietsSuccessfulProbes: probes and scrapes come every few seconds; while they succeed they are logged
// at debug level, and a failing one is logged as any other request.
func TestAccessLogQuietsSuccessfulProbes(t *testing.T) {
	tests := []struct {
		name, path string
		status     int
		wantLevel  string // "" when nothing is logged at info level
	}{
		{"a successful probe", "/readyz", http.StatusOK, ""},
		{"a failing probe", "/readyz", http.StatusServiceUnavailable, "ERROR"},
		{"another path", "/v1/notifications", http.StatusOK, "INFO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
				var logs bytes.Buffer
				h := AccessLog(observability.NewLogger(&logs, level), "/healthz", "/readyz")(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) }))
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil))

				want := tt.wantLevel
				if want == "" && level == slog.LevelDebug {
					want = "DEBUG"
				}
				got := ""
				if logs.Len() > 0 {
					var line map[string]any
					if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
						t.Fatal(err)
					}
					got, _ = line["level"].(string)
				}
				if got != want {
					t.Errorf("logged at %v: level %q, want %q", level, got, want)
				}
			}
		})
	}
}
