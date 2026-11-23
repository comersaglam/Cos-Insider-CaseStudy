package notification

import (
	"testing"
	"time"

	"uuid"
)

// TestStoreKeepsAScheduledNotificationWaiting: a scheduled notification is stored with its time and no outbox row,
// due when its time comes; it is queued by the scheduler (QueueDue), not at once.
func TestStoreKeepsAScheduledNotificationWaiting(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	n := newQueued(testDraft)
	n.Status, n.ScheduledAt = StatusScheduled, &at

	stored, created, err := store.Create(t.Context(), n, IdempotencyKey{})
	if err != nil || !created {
		t.Fatalf("Create = %v, created %v", err, created)
	}
	if stored.Status != StatusScheduled || stored.ScheduledAt == nil || !stored.ScheduledAt.Equal(at) {
		t.Errorf("stored = %s at %v, want scheduled at %v", stored.Status, stored.ScheduledAt, at)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox WHERE notification_id = $1", n.ID); c != 0 {
		t.Errorf("%d outbox rows, want none before its time", c)
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications WHERE id = $1 AND next_attempt_at = $2", n.ID, at); c != 1 {
		t.Error("next_attempt_at is not the scheduled time")
	}

	// Its time comes: the scheduler queues it with its outbox row, and the time it was scheduled for stays.
	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET next_attempt_at = now() - interval '1 second' WHERE id = $1", n.ID); err != nil {
		t.Fatal(err)
	}
	if queued, err := store.QueueDue(t.Context(), 10); err != nil || queued != 1 {
		t.Fatalf("QueueDue = %d, %v", queued, err)
	}
	got, err := store.Get(t.Context(), n.ID)
	if err != nil || got.Status != StatusQueued || got.ScheduledAt == nil || !got.ScheduledAt.Equal(at) {
		t.Errorf("after its time = %+v, %v; want queued, still showing when it was scheduled for", got, err)
	}
}

// TestStoreNeverQueuesACancelledScheduledNotification: cancelling clears the time it waited for.
func TestStoreNeverQueuesACancelledScheduledNotification(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	at := time.Now().Add(time.Hour)
	n := newQueued(testDraft)
	n.Status, n.ScheduledAt = StatusScheduled, &at
	mustCreate(t, store, n)

	cancelled, err := store.Cancel(t.Context(), n.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("Cancel = %s, %v; want cancelled", cancelled.Status, err)
	}
	if queued, _ := store.QueueDue(t.Context(), 10); queued != 0 {
		t.Errorf("a cancelled notification was queued")
	}
}

// TestStoreCreateBatchSchedulesItemByItem: each item of a batch is queued or scheduled on its own; only the queued
// ones get an outbox row.
func TestStoreCreateBatchSchedulesItemByItem(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	at := time.Now().Add(time.Hour)
	now, later := newQueued(testDraft), newQueued(testDraft)
	later.Status, later.ScheduledAt = StatusScheduled, &at

	b, _, err := store.CreateBatch(t.Context(), NewBatch{ID: uuid.NewV7(), Total: 2, CorrelationID: "corr-1", Items: []NewBatchItem{
		{Index: 0, Notification: now}, {Index: 1, Notification: later},
	}}, IdempotencyKey{})
	if err != nil || b.Accepted != 2 {
		t.Fatalf("CreateBatch = %+v, %v", b, err)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 1 {
		t.Errorf("%d outbox rows, want only the queued item's", c)
	}
	if got, _ := store.Get(t.Context(), later.ID); got.Status != StatusScheduled || got.ScheduledAt == nil {
		t.Errorf("the scheduled item = %s at %v", got.Status, got.ScheduledAt)
	}
}
