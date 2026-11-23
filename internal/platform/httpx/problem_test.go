package httpx

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

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// decode reads a recorded response body as a Problem.
func decode(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a problem: %v\n%s", err, rec.Body)
	}
	return p
}

func TestWriteProblemFillsTheStandardMembers(t *testing.T) {
	r := httptest.NewRequestWithContext(
		observability.WithCorrelationID(context.Background(), "abc"), http.MethodPost, "/v1/notifications", nil)
	rec := httptest.NewRecorder()

	p := NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the notification is invalid")
	p.Errors = []FieldProblem{{Pointer: "/to", Code: "invalid", Detail: "must be an E.164 phone number"}}
	WriteProblem(rec, r, p)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	got := decode(t, rec)
	want := Problem{
		Type: "about:blank", Title: "Unprocessable Entity", Status: 422, Detail: "the notification is invalid",
		Code: "validation_failed", CorrelationID: "abc",
		Errors: []FieldProblem{{Pointer: "/to", Code: "invalid", Detail: "must be an E.164 phone number"}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("problem =\n%+v\nwant\n%+v", got, want)
	}
}

func TestHandleWritesAReturnedProblem(t *testing.T) {
	h := Handle(slog.New(slog.DiscardHandler), func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("cancel: %w", NewProblem(http.StatusConflict, "not_cancellable", "already sent"))
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil))

	if p := decode(t, rec); rec.Code != http.StatusConflict || p.Code != "not_cancellable" {
		t.Errorf("got %d %+v, want 409 not_cancellable", rec.Code, p)
	}
}

// TestHandleHidesAnUnexpectedError answers 500 without the error's text, which may hold internal details, and
// logs the error instead.
func TestHandleHidesAnUnexpectedError(t *testing.T) {
	var logs bytes.Buffer
	h := Handle(observability.NewLogger(&logs, slog.LevelInfo), func(http.ResponseWriter, *http.Request) error {
		return errors.New("query notifications: connection refused to 10.0.0.5")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	p := decode(t, rec)
	if rec.Code != http.StatusInternalServerError || p.Code != "internal_error" {
		t.Errorf("got %d %+v, want 500 internal_error", rec.Code, p)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("response reveals the error: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), "10.0.0.5") {
		t.Errorf("the error was not logged: %s", logs.String())
	}
}

func TestHandleDoesNotWriteTwiceAfterTheResponseStarted(t *testing.T) {
	h := Handle(slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusAccepted)
		return errors.New("failed after the header")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Errorf("got %d %q, want the 202 left alone", rec.Code, rec.Body)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := WriteJSON(rec, http.StatusAccepted, map[string]string{"id": "n1"}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted || rec.Header().Get("Content-Type") != "application/json" ||
		strings.TrimSpace(rec.Body.String()) != `{"id":"n1"}` {
		t.Errorf("got %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

func TestHandleLeavesASuccessfulResponseAlone(t *testing.T) {
	h := Handle(slog.New(slog.DiscardHandler), func(w http.ResponseWriter, _ *http.Request) error {
		return WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"ok":true}` {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}
