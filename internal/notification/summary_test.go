package notification

import (
	"testing"
	"time"
)

func TestStoreSummary(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	sms := mustCreate(t, store, newQueued(testDraft))
	mustCreate(t, store, newQueued(testDraft))
	for i, ms := range []int{100, 200, 300} {
		if _, err := pool.Exec(t.Context(),
			"INSERT INTO delivery_attempts (notification_id, attempt, outcome, status_code, duration_ms) VALUES ($1, $2, 'success', 202, $3)",
			sms.ID, i+1, ms); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"INSERT INTO delivery_attempts (notification_id, attempt, outcome, status_code, duration_ms) VALUES ($1, 4, 'transient', 503, 50)",
		"INSERT INTO delivery_attempts (notification_id, attempt, outcome, duration_ms) VALUES ($1, 5, 'unknown', 0)",
		// Outside the window: not counted.
		"INSERT INTO delivery_attempts (notification_id, attempt, outcome, status_code, duration_ms, created_at) VALUES ($1, 6, 'success', 202, 9000, now() - interval '1 hour')",
	} {
		if _, err := pool.Exec(t.Context(), q, sms.ID); err != nil {
			t.Fatal(err)
		}
	}

	s, err := store.Summary(t.Context(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Channels[ChannelSMS]
	if got.Waiting[StatusQueued] != 2 || got.Waiting[StatusSending] != 0 {
		t.Errorf("waiting = %v, want 2 queued", got.Waiting)
	}
	if got.Attempts[OutcomeSuccess] != 3 || got.Attempts[OutcomeTransient] != 1 || got.Attempts[OutcomeUnknown] != 1 {
		t.Errorf("attempts = %v, want 3 success, 1 transient, 1 unknown in the window", got.Attempts)
	}
	// Unknown attempts have no latency; the hour-old one is outside the window: 50, 100, 200, 300 ms remain.
	if got.Latency == nil || got.Latency.P50 != 150*time.Millisecond || got.Latency.P95 != 285*time.Millisecond ||
		got.Latency.P99 != 297*time.Millisecond {
		t.Errorf("latency = %+v, want p50 150ms, p95 285ms, p99 297ms", got.Latency)
	}
	if email := s.Channels[ChannelEmail]; email.Latency != nil || len(email.Attempts) != 0 {
		t.Errorf("email = %+v, want no attempts and no latency", email)
	}
	if len(s.Channels) != 3 {
		t.Errorf("%d channels, want all three", len(s.Channels))
	}
}
