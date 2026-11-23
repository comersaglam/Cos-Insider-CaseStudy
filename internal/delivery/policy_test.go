package delivery

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
)

var testPolicy = RetryPolicy{Base: 10 * time.Second, Cap: 15 * time.Minute, MaxAttempts: 8}

// TestDelayStaysUnderItsCeiling draws many delays per attempt: each lies in [0, min(Cap, Base·2^(n−1))], and they
// spread out, which is the point of the jitter.
func TestDelayStaysUnderItsCeiling(t *testing.T) {
	for n, ceiling := range map[int]time.Duration{
		1: 10 * time.Second, 2: 20 * time.Second, 4: 80 * time.Second, 7: 640 * time.Second, 8: 15 * time.Minute,
		100: 15 * time.Minute, // no overflow however far it goes
	} {
		seen := map[time.Duration]bool{}
		for range 500 {
			d := testPolicy.Delay(n, 0)
			if d < 0 || d > ceiling {
				t.Fatalf("Delay(%d) = %v, want within [0, %v]", n, d, ceiling)
			}
			seen[d] = true
		}
		if len(seen) < 100 {
			t.Errorf("Delay(%d) gave %d distinct waits in 500, want them spread", n, len(seen))
		}
	}
}

func TestDelayWaitsAtLeastWhatTheProviderAsked(t *testing.T) {
	for range 100 {
		if d := testPolicy.Delay(1, time.Minute); d < time.Minute {
			t.Fatalf("Delay with Retry-After 1m = %v, want at least 1m", d)
		}
	}
}

func TestAfterDecidesWhatBecomesOfTheNotification(t *testing.T) {
	id := uuid.NewV7()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	fail := errors.New("provider answered 503: busy")
	tests := []struct {
		name    string
		attempt int
		out     Outcome
		next    notification.Status
		reason  notification.FailureReason
	}{
		{"success", 1, Outcome{Class: Success, StatusCode: 202, MessageID: "m-1"}, notification.StatusSent, ""},
		{"permanent", 1, Outcome{Class: Permanent, StatusCode: 422, Err: fail}, notification.StatusFailed, notification.FailureProviderRejected},
		{"our credentials", 1, Outcome{Class: PermanentAlert, StatusCode: 401, Err: fail}, notification.StatusFailed, notification.FailureProviderRejected},
		{"transient with attempts left", 3, Outcome{Class: Transient, StatusCode: 503, Err: fail}, notification.StatusRetrying, ""},
		{"transient on the last attempt", 8, Outcome{Class: Transient, StatusCode: 503, Err: fail}, notification.StatusFailed, notification.FailureAttemptsExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.out.Latency = 300 * time.Millisecond
			a := testPolicy.After(id, tt.attempt, tt.out, now)
			if a.Next != tt.next || a.FailureReason != tt.reason || a.NotificationID != id || a.Number != tt.attempt ||
				a.Outcome != notification.AttemptOutcome(tt.out.Class) || a.StatusCode != tt.out.StatusCode || a.Duration != 300*time.Millisecond {
				t.Errorf("After = %+v", a)
			}
			if tt.out.Err != nil && a.Error != tt.out.Err.Error() {
				t.Errorf("error = %q, want the outcome's", a.Error)
			}
			if a.Next == notification.StatusRetrying && (a.NextAttemptAt.Before(now) || a.NextAttemptAt.After(now.Add(40*time.Second))) {
				t.Errorf("next attempt at %v, want within the third attempt's ceiling of 40s", a.NextAttemptAt)
			}
			if tt.out.Class == Success && a.ProviderMessageID != "m-1" {
				t.Errorf("provider message ID = %q", a.ProviderMessageID)
			}
		})
	}
}
