package notification

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"
)

// fakeService records the drafts it receives and answers as configured.
type fakeService struct {
	drafts []Draft
	keys   []string
	result Notification
	replay bool
	err    error
}

func (f *fakeService) Create(_ context.Context, d Draft, key string) (Notification, bool, error) {
	f.drafts = append(f.drafts, d)
	f.keys = append(f.keys, key)
	if f.err != nil {
		return Notification{}, false, f.err
	}
	return f.result, !f.replay, nil
}

func (f *fakeService) Get(_ context.Context, id uuid.UUID) (Notification, error) {
	if f.err != nil {
		return Notification{}, f.err
	}
	if id != f.result.ID {
		return Notification{}, ErrNotFound
	}
	return f.result, nil
}

var stored = Notification{
	ID: uuid.MustParse("01938f2e-7c1e-7b3a-9d4e-2f1a3b4c5d6e"), To: "+905551234567", Channel: ChannelSMS,
	Content: "Your order has shipped.", Priority: PriorityNormal, Status: StatusQueued, CorrelationID: "corr-1",
	CreatedAt: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC),
}

// serve sends one request through a mux with the module's routes.
func serve(t *testing.T, svc service, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(svc, slog.New(slog.DiscardHandler)).RegisterRoutes(mux)
	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

// body decodes a JSON response into a map.
func body(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body)
	}
	return m
}

const validBody = `{"to":"+905551234567","channel":"sms","content":"Your order has shipped."}`

func TestCreateAcceptsANotification(t *testing.T) {
	svc := &fakeService{result: stored}
	rec := serve(t, svc, http.MethodPost, "/v1/notifications", validBody, map[string]string{"Idempotency-Key": "order-42-sms"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/v1/notifications/"+stored.ID.String() {
		t.Errorf("Location = %q", loc)
	}
	if rec.Header().Get("Idempotent-Replayed") != "" {
		t.Error("a first request is marked as replayed")
	}
	want := map[string]any{
		"id": stored.ID.String(), "to": "+905551234567", "channel": "sms", "content": "Your order has shipped.",
		"priority": "normal", "status": "queued", "correlationId": "corr-1",
		"createdAt": "2026-11-23T12:00:00Z", "updatedAt": "2026-11-23T12:00:00Z",
	}
	got := body(t, rec)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("response has %d members, want %d: %v", len(got), len(want), got)
	}
	if svc.keys[0] != "order-42-sms" {
		t.Errorf("key passed = %q", svc.keys[0])
	}
}

func TestCreateDefaultsThePriorityToNormal(t *testing.T) {
	svc := &fakeService{result: stored}
	serve(t, svc, http.MethodPost, "/v1/notifications", validBody, nil)
	if svc.drafts[0].Priority != PriorityNormal {
		t.Errorf("priority = %q, want normal", svc.drafts[0].Priority)
	}

	serve(t, svc, http.MethodPost, "/v1/notifications",
		`{"to":"+905551234567","channel":"sms","content":"hi","priority":"high"}`, nil)
	if svc.drafts[1].Priority != PriorityHigh {
		t.Errorf("priority = %q, want high as sent", svc.drafts[1].Priority)
	}
}

func TestCreateMarksARepeatedRequest(t *testing.T) {
	rec := serve(t, &fakeService{result: stored, replay: true}, http.MethodPost, "/v1/notifications", validBody,
		map[string]string{"Idempotency-Key": "order-42-sms"})
	if rec.Code != http.StatusAccepted || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Errorf("got %d, Idempotent-Replayed %q; want 202 and true", rec.Code, rec.Header().Get("Idempotent-Replayed"))
	}
}

func TestCreateErrors(t *testing.T) {
	invalid := &ValidationError{Fields: []FieldError{
		{Field: "to", Code: CodeInvalid, Message: "must be an E.164 phone number"},
		{Field: "content", Code: CodeRequired, Message: "is required"},
	}}
	tests := []struct {
		name       string
		svc        *fakeService
		body       string
		header     map[string]string
		wantStatus int
		wantCode   string
		calls      int // how many times the service is reached
	}{
		{"invalid notification", &fakeService{err: invalid}, validBody, nil, 422, "validation_failed", 1},
		{"key reused for another request", &fakeService{err: ErrIdempotencyKeyReused}, validBody,
			map[string]string{"Idempotency-Key": "k"}, 422, "idempotency_key_reused", 1},
		{"key with a space", &fakeService{}, validBody, map[string]string{"Idempotency-Key": "a b"}, 400,
			"invalid_idempotency_key", 0},
		{"key too long", &fakeService{}, validBody, map[string]string{"Idempotency-Key": strings.Repeat("k", 256)}, 400,
			"invalid_idempotency_key", 0},
		{"malformed JSON", &fakeService{}, `{"to":`, nil, 400, "invalid_json", 0},
		{"body over 1 MiB", &fakeService{}, `{"content":"` + strings.Repeat("a", 1<<20) + `"}`, nil, 413, "body_too_large", 0},
		{"unexpected failure", &fakeService{err: errors.New("database down")}, validBody, nil, 500, "internal_error", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, tt.svc, http.MethodPost, "/v1/notifications", tt.body, tt.header)
			got := body(t, rec)
			if rec.Code != tt.wantStatus || got["code"] != tt.wantCode {
				t.Errorf("got %d %v, want %d %s", rec.Code, got["code"], tt.wantStatus, tt.wantCode)
			}
			if len(tt.svc.drafts) != tt.calls {
				t.Errorf("service reached %d times, want %d", len(tt.svc.drafts), tt.calls)
			}
		})
	}
}

// TestCreateLocatesEachInvalidField turns the domain's field errors into JSON Pointers to the request's members.
func TestCreateLocatesEachInvalidField(t *testing.T) {
	invalid := &ValidationError{Fields: []FieldError{{Field: "to", Code: CodeInvalid, Message: "must be E.164"}}}
	rec := serve(t, &fakeService{err: invalid}, http.MethodPost, "/v1/notifications", validBody, nil)

	errs, _ := body(t, rec)["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one", errs)
	}
	first, _ := errs[0].(map[string]any)
	if first["pointer"] != "/to" || first["code"] != "invalid" || first["detail"] != "must be E.164" {
		t.Errorf("field error = %v", first)
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name, path string
		svc        *fakeService
		wantStatus int
		wantCode   string
	}{
		{"found", "/v1/notifications/" + stored.ID.String(), &fakeService{result: stored}, 200, ""},
		{"unknown ID", "/v1/notifications/" + uuid.NewV7().String(), &fakeService{result: stored}, 404, "not_found"},
		{"not a UUID", "/v1/notifications/42", &fakeService{result: stored}, 404, "not_found"},
		{"unexpected failure", "/v1/notifications/" + stored.ID.String(), &fakeService{err: errors.New("down")}, 500,
			"internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, tt.svc, http.MethodGet, tt.path, "", nil)
			got := body(t, rec)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantCode != "" && got["code"] != tt.wantCode {
				t.Errorf("code = %v, want %s", got["code"], tt.wantCode)
			}
			if tt.wantCode == "" && got["id"] != stored.ID.String() {
				t.Errorf("id = %v", got["id"])
			}
		})
	}
}
