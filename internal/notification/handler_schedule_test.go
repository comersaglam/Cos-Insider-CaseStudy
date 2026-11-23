package notification

import (
	"net/http"
	"testing"
	"time"
)

// TestCreateTakesASchedule: scheduledAt, in any offset, reaches the service as an instant, and the answer shows it in
// UTC with the scheduled status.
func TestCreateTakesASchedule(t *testing.T) {
	at := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	scheduled := stored
	scheduled.Status, scheduled.ScheduledAt = StatusScheduled, &at
	svc := &fakeService{result: scheduled}

	rec := serve(t, svc, http.MethodPost, "/v1/notifications",
		`{"to":"+905551234567","channel":"sms","content":"Flash sale starts now.","scheduledAt":"2026-11-24T12:00:00+03:00"}`, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := svc.drafts[0].ScheduledAt; got == nil || !got.Equal(at) {
		t.Errorf("draft scheduledAt = %v, want %v", got, at)
	}
	if b := body(t, rec); b["scheduledAt"] != "2026-11-24T09:00:00Z" || b["status"] != "scheduled" {
		t.Errorf("answer scheduledAt %v, status %v", b["scheduledAt"], b["status"])
	}
}

func TestCreateRefusesAMalformedSchedule(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodPost, "/v1/notifications",
		`{"to":"+905551234567","channel":"sms","content":"hi","scheduledAt":"tomorrow at nine"}`, nil)
	if b := body(t, rec); rec.Code != http.StatusBadRequest || b["code"] != "invalid_json" {
		t.Errorf("status = %d %v, want 400 invalid_json", rec.Code, b["code"])
	}
}
