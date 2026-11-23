package notification

import (
	"context"
	"testing"
	"time"

	"uuid"
)

// TestStatusListenerHearsEveryStatusChange: a committed status change reaches the listener with the notification,
// its batch and the moment, in UTC; an update that leaves the status alone does not.
func TestStatusListenerHearsEveryStatusChange(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	listener, err := store.ListenStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })

	batchID := uuid.NewV7()
	b, _, err := store.CreateBatch(t.Context(), NewBatch{ID: batchID, Total: 1, CorrelationID: "c", Items: []NewBatchItem{
		{Index: 0, Notification: newQueued(testDraft)},
	}}, IdempotencyKey{})
	if err != nil {
		t.Fatal(err)
	}
	id := b.Items[0].ID

	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET attempts = attempts WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancel(t.Context(), id); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ev, err := listener.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ev.NotificationID != id || ev.Status != StatusCancelled || ev.BatchID == nil || *ev.BatchID != batchID ||
		ev.At.Location() != time.UTC || time.Since(ev.At) > time.Minute {
		t.Errorf("event = %+v; want %s cancelled in batch %s, now, in UTC", ev, id, batchID)
	}

	// Nothing else was heard: the update that kept the status sent nothing.
	quiet, stop := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer stop()
	if ev, err := listener.Next(quiet); err == nil {
		t.Errorf("an unchanged status was heard: %+v", ev)
	}
}
