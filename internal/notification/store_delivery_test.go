package notification

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testLease       = time.Minute
	testMaxAttempts = 3
)

// expireLease makes a claim's lease run out, as time passing would.
func expireLease(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET claimed_until = now() - interval '1 second' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
}

func TestClaimTakesAQueuedNotification(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	n := mustCreate(t, store, newQueued(testDraft))

	c, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if c.Notification.Status != StatusSending || c.Notification.Attempts != 1 || c.TakenOver || c.Notification.To != n.To {
		t.Errorf("claim = %+v", c)
	}
	// Another copy of the message, while the lease holds, finds nothing to send.
	if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); !errors.Is(err, ErrNotClaimable) {
		t.Errorf("a second claim under a live lease = %v, want ErrNotClaimable", err)
	}
}

func TestClaimRefusesWhatIsNotThereToSend(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	sent := mustCreate(t, store, newQueued(testDraft))
	setStatus(t, pool, sent.ID, StatusSent)
	cancelled := mustCreate(t, store, newQueued(testDraft))
	if _, err := store.Cancel(t.Context(), cancelled.ID); err != nil {
		t.Fatal(err)
	}

	for name, id := range map[string]uuid.UUID{"sent": sent.ID, "cancelled": cancelled.ID} {
		if _, err := store.Claim(t.Context(), id, testLease, testMaxAttempts); !errors.Is(err, ErrNotClaimable) {
			t.Errorf("claiming a %s notification = %v, want ErrNotClaimable", name, err)
		}
	}
	if _, err := store.Claim(t.Context(), uuid.NewV7(), testLease, testMaxAttempts); !errors.Is(err, ErrNotFound) {
		t.Errorf("claiming a missing notification = %v, want ErrNotFound", err)
	}
}

// TestClaimTakesOverAnExpiredLease: the worker holding the notification was lost; another claims it, spending an
// attempt, and says it is a takeover, a possible duplicate (ADR 0012).
func TestClaimTakesOverAnExpiredLease(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	n := mustCreate(t, store, newQueued(testDraft))
	if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); err != nil {
		t.Fatal(err)
	}
	expireLease(t, pool, n.ID)

	c, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts)
	if err != nil || !c.TakenOver || c.Notification.Attempts != 2 {
		t.Errorf("takeover = %+v, %v; want taken over at attempt 2", c, err)
	}
}

// TestClaimStopsAtTheLastAttempt: a message that crashes its worker every time spends an attempt per claim and
// stops being claimed when none is left (ADR 0012).
func TestClaimStopsAtTheLastAttempt(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	n := mustCreate(t, store, newQueued(testDraft))
	for range testMaxAttempts {
		if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); err != nil {
			t.Fatal(err)
		}
		expireLease(t, pool, n.ID)
	}
	if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); !errors.Is(err, ErrNotClaimable) {
		t.Errorf("claim after every attempt = %v, want ErrNotClaimable", err)
	}
}

func TestFinishAttempt(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	retryAt := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
	tests := []struct {
		name  string
		after Attempt
		check func(t *testing.T, n Notification)
	}{
		{"sent", Attempt{Outcome: OutcomeSuccess, StatusCode: 202, Next: StatusSent, ProviderMessageID: "m-1"},
			func(t *testing.T, n Notification) {
				if n.Status != StatusSent || n.ProviderMessageID != "m-1" {
					t.Errorf("notification = %+v, want sent with the provider's ID", n)
				}
			}},
		{"retrying", Attempt{Outcome: OutcomeTransient, StatusCode: 503, Error: "provider answered 503", Next: StatusRetrying, NextAttemptAt: retryAt},
			func(t *testing.T, n Notification) {
				if n.Status != StatusRetrying {
					t.Errorf("status = %s, want retrying", n.Status)
				}
			}},
		{"failed", Attempt{Outcome: OutcomePermanent, StatusCode: 422, Error: "provider answered 422", Next: StatusFailed, FailureReason: FailureProviderRejected},
			func(t *testing.T, n Notification) {
				if n.Status != StatusFailed || n.FailureReason != FailureProviderRejected || n.FailedAt == nil {
					t.Errorf("notification = %+v, want failed, rejected, with its time", n)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := mustCreate(t, store, newQueued(testDraft))
			c, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts)
			if err != nil {
				t.Fatal(err)
			}
			a := tt.after
			a.NotificationID, a.Number, a.Duration = n.ID, c.Notification.Attempts, 120*time.Millisecond
			if err := store.FinishAttempt(t.Context(), a); err != nil {
				t.Fatalf("FinishAttempt: %v", err)
			}
			got, err := store.Get(t.Context(), n.ID)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, got)

			records, err := store.Attempts(t.Context(), n.ID)
			if err != nil || len(records) != 1 {
				t.Fatalf("Attempts = %v, %v; want one", records, err)
			}
			r := records[0]
			if r.Number != 1 || r.Outcome != tt.after.Outcome || r.StatusCode != tt.after.StatusCode ||
				r.Error != tt.after.Error || r.Duration != 120*time.Millisecond {
				t.Errorf("record = %+v, want %+v", r, tt.after)
			}
		})
	}
}

// TestFinishAttemptAfterATakeover: the lease ran out during the attempt and the reaper took the notification over.
// The late outcome is kept in the record but does not overwrite the newer state.
func TestFinishAttemptAfterATakeover(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	n := mustCreate(t, store, newQueued(testDraft))
	if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); err != nil {
		t.Fatal(err)
	}
	expireLease(t, pool, n.ID)
	if _, err := store.ReapExpiredClaims(t.Context(), testMaxAttempts, 10); err != nil {
		t.Fatal(err)
	}

	err := store.FinishAttempt(t.Context(), Attempt{NotificationID: n.ID, Number: 1, Outcome: OutcomeSuccess, Next: StatusSent})
	if !errors.Is(err, ErrClaimLost) {
		t.Errorf("FinishAttempt = %v, want ErrClaimLost", err)
	}
	if got, _ := store.Get(t.Context(), n.ID); got.Status != StatusRetrying {
		t.Errorf("status = %s, want retrying as the reaper left it", got.Status)
	}
	if records, _ := store.Attempts(t.Context(), n.ID); len(records) != 2 {
		t.Errorf("%d records, want the reaper's unknown and the late success", len(records))
	}
}

func TestMarkUnprocessable(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	queued := mustCreate(t, store, newQueued(testDraft))
	sent := mustCreate(t, store, newQueued(testDraft))
	setStatus(t, pool, sent.ID, StatusSent)

	for _, id := range []uuid.UUID{queued.ID, sent.ID} {
		if err := store.MarkUnprocessable(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := store.Get(t.Context(), queued.ID); n.Status != StatusFailed || n.FailureReason != FailureUnprocessable {
		t.Errorf("queued notification = %s %s, want failed as unprocessable", n.Status, n.FailureReason)
	}
	if n, _ := store.Get(t.Context(), sent.ID); n.Status != StatusSent {
		t.Errorf("sent notification = %s, want left sent", n.Status)
	}
}

func TestReapExpiredClaims(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	claim := func(times int) Notification {
		n := mustCreate(t, store, newQueued(testDraft))
		for range times {
			if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); err != nil {
				t.Fatal(err)
			}
			expireLease(t, pool, n.ID)
		}
		return n
	}
	early := claim(1)              // attempts left: back to retrying
	last := claim(testMaxAttempts) // none left: failed
	live := mustCreate(t, store, newQueued(testDraft))
	if _, err := store.Claim(t.Context(), live.ID, testLease, testMaxAttempts); err != nil {
		t.Fatal(err)
	}

	if n, err := store.ReapExpiredClaims(t.Context(), testMaxAttempts, 10); err != nil || n != 2 {
		t.Fatalf("ReapExpiredClaims = %d, %v; want the 2 expired", n, err)
	}
	if n, _ := store.Get(t.Context(), early.ID); n.Status != StatusRetrying {
		t.Errorf("early = %s, want retrying", n.Status)
	}
	if n, _ := store.Get(t.Context(), last.ID); n.Status != StatusFailed || n.FailureReason != FailureAttemptsExhausted {
		t.Errorf("last = %s %s, want failed with attempts exhausted", n.Status, n.FailureReason)
	}
	if n, _ := store.Get(t.Context(), live.ID); n.Status != StatusSending {
		t.Errorf("live = %s, want still sending", n.Status)
	}
	if r, _ := store.Attempts(t.Context(), early.ID); len(r) != 1 || r[0].Outcome != OutcomeUnknown {
		t.Errorf("early's records = %+v, want one unknown", r)
	}
	// The reaped notification is due at once: the scheduler queues it on its next round.
	if q, err := store.QueueDue(t.Context(), 10); err != nil || q != 1 {
		t.Errorf("QueueDue after reaping = %d, %v; want the retrying one", q, err)
	}
}

func TestRetryGivesBackEveryAttempt(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	n := mustCreate(t, store, newQueued(testDraft))
	for range testMaxAttempts {
		if _, err := store.Claim(t.Context(), n.ID, testLease, testMaxAttempts); err != nil {
			t.Fatal(err)
		}
		expireLease(t, pool, n.ID)
	}
	if _, err := store.ReapExpiredClaims(t.Context(), testMaxAttempts, 10); err != nil {
		t.Fatal(err)
	}

	got, err := store.Retry(t.Context(), n.ID)
	if err != nil || got.Attempts != 0 || got.FailureReason != "" {
		t.Errorf("Retry = attempts %d reason %q, %v; want a fresh budget", got.Attempts, got.FailureReason, err)
	}
}
