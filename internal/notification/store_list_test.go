package notification

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

// mustCreate stores n without a key.
func mustCreate(t *testing.T, store *Store, n Notification) Notification {
	t.Helper()
	stored, _, err := store.Create(t.Context(), n, IdempotencyKey{})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

// fail marks a notification failed at the given time, as the worker will.
func fail(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		"UPDATE notifications SET status = 'failed', failed_at = $2, failure_reason = 'attempts_exhausted' WHERE id = $1", id, at); err != nil {
		t.Fatal(err)
	}
}

// setStatus moves a notification to sent, or to sending under a lease of a minute, as a worker will.
func setStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, s Status) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		"UPDATE notifications SET status = $2, claimed_until = CASE WHEN $2 = 'sending' THEN now() + interval '1 minute' END WHERE id = $1",
		id, string(s)); err != nil {
		t.Fatal(err)
	}
}

func ids(ns []Notification) []uuid.UUID {
	out := make([]uuid.UUID, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

// TestStoreListPagesWithoutLosingTies pages through notifications three at a time. Five of them come in one batch,
// one transaction, and share their creation time, so the pages must break ties by ID.
func TestStoreListPagesWithoutLosingTies(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	mustCreate(t, store, newQueued(testDraft))
	nb := NewBatch{ID: uuid.NewV7(), Total: 5, CorrelationID: "c"}
	for i := range 5 {
		nb.Items = append(nb.Items, NewBatchItem{Index: i, Notification: newQueued(testDraft)})
	}
	if _, _, err := store.CreateBatch(t.Context(), nb, IdempotencyKey{}); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, store, newQueued(testDraft))

	all, err := store.List(t.Context(), ListFilter{}, nil, 100)
	if err != nil || len(all) != 7 {
		t.Fatalf("List = %d notifications, %v; want 7", len(all), err)
	}
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if cur.CreatedAt.After(prev.CreatedAt) || (cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID.Compare(prev.ID) > 0) {
			t.Fatalf("item %d is newer than item %d: the list is not newest first", i, i-1)
		}
	}

	var paged []Notification
	var after *Cursor
	for range 10 {
		page, err := store.List(t.Context(), ListFilter{}, after, 3)
		if err != nil {
			t.Fatal(err)
		}
		paged = append(paged, page...)
		if len(page) < 3 {
			break
		}
		last := page[len(page)-1]
		after = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if !slices.Equal(ids(paged), ids(all)) {
		t.Errorf("pages of three gave %d notifications in another order than the full list", len(paged))
	}
}

func TestStoreListFilters(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	sms := mustCreate(t, store, newQueued(testDraft))
	email := mustCreate(t, store, newQueued(Draft{To: "a@example.com", Channel: ChannelEmail, Content: "x", Priority: PriorityLow}))
	sent := mustCreate(t, store, newQueued(testDraft))
	setStatus(t, pool, sent.ID, StatusSent)
	nb := NewBatch{ID: uuid.NewV7(), Total: 1, CorrelationID: "c",
		Items: []NewBatchItem{{Index: 0, Notification: newQueued(testDraft)}}}
	if _, _, err := store.CreateBatch(t.Context(), nb, IdempotencyKey{}); err != nil {
		t.Fatal(err)
	}
	batchID := nb.ID
	inBatch := nb.Items[0].Notification.ID

	tests := []struct {
		name   string
		filter ListFilter
		want   []uuid.UUID // newest first
	}{
		{"everything", ListFilter{}, []uuid.UUID{inBatch, sent.ID, email.ID, sms.ID}},
		{"one status", ListFilter{Statuses: []Status{StatusSent}}, []uuid.UUID{sent.ID}},
		{"two statuses", ListFilter{Statuses: []Status{StatusSent, StatusQueued}}, []uuid.UUID{inBatch, sent.ID, email.ID, sms.ID}},
		{"a channel", ListFilter{Channels: []Channel{ChannelEmail}}, []uuid.UUID{email.ID}},
		{"a batch", ListFilter{BatchID: &batchID}, []uuid.UUID{inBatch}},
		{"status and channel", ListFilter{Statuses: []Status{StatusQueued}, Channels: []Channel{ChannelSMS}}, []uuid.UUID{inBatch, sms.ID}},
		{"created from the email on", ListFilter{CreatedFrom: email.CreatedAt}, []uuid.UUID{inBatch, sent.ID, email.ID}},
		{"created before the email", ListFilter{CreatedTo: email.CreatedAt}, []uuid.UUID{sms.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.List(t.Context(), tt.filter, nil, 100)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tt.want) {
				t.Errorf("List = %v, want %v", ids(got), tt.want)
			}
		})
	}
}

func TestStoreCancel(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	queued := mustCreate(t, store, newQueued(testDraft))

	got, err := store.Cancel(t.Context(), queued.ID)
	if err != nil || got.Status != StatusCancelled {
		t.Fatalf("Cancel = %s, %v; want cancelled", got.Status, err)
	}
	if again, err := store.Cancel(t.Context(), queued.ID); err != nil || again.Status != StatusCancelled {
		t.Errorf("cancelling again = %s, %v; want the cancelled notification, no error", again.Status, err)
	}

	sending := mustCreate(t, store, newQueued(testDraft))
	setStatus(t, pool, sending.ID, StatusSending)
	if _, err := store.Cancel(t.Context(), sending.ID); !errors.Is(err, ErrNotCancellable) {
		t.Errorf("cancelling a notification being sent = %v, want ErrNotCancellable", err)
	}
	if _, err := store.Cancel(t.Context(), uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("cancelling a missing notification = %v, want ErrNotFound", err)
	}
}

// TestStoreCancelRacingAClaim cancels notifications while a worker claims them at the same moment: one of the two
// wins each time, and what the cancel reported matches the outcome. The claim is the worker's conditional UPDATE.
func TestStoreCancelRacingAClaim(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	for range 20 {
		n := mustCreate(t, store, newQueued(testDraft))
		var (
			wg        sync.WaitGroup
			cancelErr error
			claimed   int64
		)
		wg.Go(func() { _, cancelErr = store.Cancel(t.Context(), n.ID) })
		wg.Go(func() {
			tag, err := pool.Exec(t.Context(),
				"UPDATE notifications SET status = 'sending', claimed_until = now() + interval '1 minute' WHERE id = $1 AND status = 'queued'", n.ID)
			if err != nil {
				t.Error(err)
			}
			claimed = tag.RowsAffected()
		})
		wg.Wait()

		final, err := store.Get(t.Context(), n.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case cancelErr == nil && (claimed != 0 || final.Status != StatusCancelled):
			t.Fatalf("cancel succeeded, but the claim took %d rows and the status is %s", claimed, final.Status)
		case errors.Is(cancelErr, ErrNotCancellable) && (claimed != 1 || final.Status != StatusSending):
			t.Fatalf("cancel refused, but the claim took %d rows and the status is %s", claimed, final.Status)
		case cancelErr != nil && !errors.Is(cancelErr, ErrNotCancellable):
			t.Fatalf("Cancel: %v", cancelErr)
		}
	}
}

func TestStoreRetry(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	failed := mustCreate(t, store, newQueued(testDraft))
	fail(t, pool, failed.ID, time.Now())

	got, err := store.Retry(t.Context(), failed.ID)
	if err != nil || got.Status != StatusQueued || got.FailedAt != nil {
		t.Fatalf("Retry = %s failed at %v, %v; want queued with no failure time", got.Status, got.FailedAt, err)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox WHERE notification_id = $1", failed.ID); c != 2 {
		t.Errorf("%d outbox rows for the notification, want 2: one from its creation and one from the retry", c)
	}
	if _, err := store.Retry(t.Context(), failed.ID); !errors.Is(err, ErrNotRetryable) {
		t.Errorf("retrying a queued notification = %v, want ErrNotRetryable", err)
	}
	if _, err := store.Retry(t.Context(), uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("retrying a missing notification = %v, want ErrNotFound", err)
	}
}

func TestStoreRequeueFailed(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	window := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

	var inWindow []uuid.UUID
	for i := range 5 {
		n := mustCreate(t, store, newQueued(testDraft))
		fail(t, pool, n.ID, window.Add(time.Duration(i)*time.Minute))
		inWindow = append(inWindow, n.ID)
	}
	late := mustCreate(t, store, newQueued(testDraft))
	fail(t, pool, late.ID, window.Add(2*time.Hour))
	email := mustCreate(t, store, newQueued(Draft{To: "a@example.com", Channel: ChannelEmail, Content: "x", Priority: PriorityLow}))
	fail(t, pool, email.ID, window.Add(time.Minute))
	mustCreate(t, store, newQueued(testDraft)) // queued, never failed

	f := RetryFilter{Channel: ChannelSMS, FailedFrom: window, FailedTo: window.Add(time.Hour)}
	if matched, err := store.CountFailed(t.Context(), f); err != nil || matched != 5 {
		t.Fatalf("CountFailed = %d, %v; want the 5 SMS that failed in the hour", matched, err)
	}

	if n, err := store.RequeueFailed(t.Context(), f, 3); err != nil || n != 3 {
		t.Fatalf("RequeueFailed(max 3) = %d, %v; want 3", n, err)
	}
	if n, err := store.RequeueFailed(t.Context(), f, 3); err != nil || n != 2 {
		t.Fatalf("RequeueFailed again = %d, %v; want the 2 left", n, err)
	}
	for _, id := range inWindow {
		if n, _ := store.Get(t.Context(), id); n.Status != StatusQueued || n.FailedAt != nil {
			t.Errorf("%s is %s, want queued", id, n.Status)
		}
	}
	for _, id := range []uuid.UUID{late.ID, email.ID} {
		if n, _ := store.Get(t.Context(), id); n.Status != StatusFailed {
			t.Errorf("%s outside the filter is %s, want still failed", id, n.Status)
		}
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 8+5 {
		t.Errorf("%d outbox rows, want 8 from creation and 5 from the retries", c)
	}
}

func TestStoreRequeueFailedByBatch(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	nb := NewBatch{ID: uuid.NewV7(), Total: 2, CorrelationID: "c", Items: []NewBatchItem{
		{Index: 0, Notification: newQueued(testDraft)}, {Index: 1, Notification: newQueued(testDraft)}}}
	if _, _, err := store.CreateBatch(t.Context(), nb, IdempotencyKey{}); err != nil {
		t.Fatal(err)
	}
	other := mustCreate(t, store, newQueued(testDraft))
	now := time.Now()
	for _, id := range []uuid.UUID{nb.Items[0].Notification.ID, nb.Items[1].Notification.ID, other.ID} {
		fail(t, pool, id, now)
	}

	batchID := nb.ID
	n, err := store.RequeueFailed(t.Context(),
		RetryFilter{BatchID: &batchID, FailedFrom: now.Add(-time.Minute), FailedTo: now.Add(time.Minute)}, MaxRequeue)
	if err != nil || n != 2 {
		t.Errorf("RequeueFailed(batch) = %d, %v; want the batch's 2", n, err)
	}
	if got, _ := store.Get(t.Context(), other.ID); got.Status != StatusFailed {
		t.Errorf("a notification outside the batch is %s, want still failed", got.Status)
	}
}
