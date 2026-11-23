package notification

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"
)

// newTestBatch returns a batch of n accepted notifications, at even positions, and one rejection for each odd
// position in between.
func newTestBatch(n int) NewBatch {
	b := NewBatch{ID: uuid.NewV7(), CorrelationID: "corr-b"}
	for i := range n {
		b.Items = append(b.Items, NewBatchItem{Index: 2 * i, Notification: newQueued(testDraft)})
		if i < n-1 {
			b.Rejections = append(b.Rejections, Rejection{Index: 2*i + 1, Fields: []FieldError{
				{Field: "to", Code: CodeInvalid, Message: "must be an E.164 phone number such as +905551234567"},
			}})
		}
	}
	b.Total = len(b.Items) + len(b.Rejections)
	return b
}

// itemsOf returns the positions and IDs the store should report for b.
func itemsOf(b NewBatch) []BatchItem {
	items := make([]BatchItem, 0, len(b.Items))
	for _, item := range b.Items {
		items = append(items, BatchItem{Index: item.Index, ID: item.Notification.ID})
	}
	return items
}

func TestStoreCreatesABatch(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	nb := newTestBatch(3)

	b, created, err := store.CreateBatch(t.Context(), nb, IdempotencyKey{})
	if err != nil || !created {
		t.Fatalf("CreateBatch = created %v, err %v", created, err)
	}
	if b.ID != nb.ID || b.Total != 5 || b.Accepted != 3 || b.CorrelationID != "corr-b" {
		t.Errorf("batch = %+v", b)
	}
	if b.CreatedAt.IsZero() || b.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt = %v, want a time in UTC", b.CreatedAt)
	}
	if !slices.Equal(b.Items, itemsOf(nb)) {
		t.Errorf("items = %v, want %v", b.Items, itemsOf(nb))
	}
	if fmt.Sprint(b.Rejections) != fmt.Sprint(nb.Rejections) {
		t.Errorf("rejections = %+v, want %+v", b.Rejections, nb.Rejections)
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications WHERE batch_id = $1", nb.ID); c != 3 {
		t.Errorf("%d notifications in the batch, want 3", c)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 3 {
		t.Errorf("%d outbox rows, want 3", c)
	}
}

func TestStoreCreatesAFullBatch(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	nb := NewBatch{ID: uuid.NewV7(), Total: MaxBatchSize, CorrelationID: "corr-full"}
	for i := range MaxBatchSize {
		nb.Items = append(nb.Items, NewBatchItem{Index: i, Notification: newQueued(testDraft)})
	}

	b, _, err := store.CreateBatch(t.Context(), nb, IdempotencyKey{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Accepted != MaxBatchSize || len(b.Items) != MaxBatchSize {
		t.Errorf("accepted %d, items %d; want %d", b.Accepted, len(b.Items), MaxBatchSize)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != MaxBatchSize {
		t.Errorf("%d outbox rows, want %d", c, MaxBatchSize)
	}
}

func TestStoreRepeatedBatchReturnsTheFirst(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	key := IdempotencyKey{Key: "campaign-7", Fingerprint: [32]byte{1}}
	first := newTestBatch(3)

	if _, _, err := store.CreateBatch(t.Context(), first, key); err != nil {
		t.Fatal(err)
	}
	again, created, err := store.CreateBatch(t.Context(), newTestBatch(3), key)
	if err != nil || created {
		t.Fatalf("repeat = created %v, err %v; want the first batch", created, err)
	}
	if again.ID != first.ID || !slices.Equal(again.Items, itemsOf(first)) ||
		fmt.Sprint(again.Rejections) != fmt.Sprint(first.Rejections) {
		t.Errorf("repeat = %+v, want the first batch's IDs and rejections", again)
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications"); c != 3 {
		t.Errorf("%d notifications, want 3", c)
	}
}

func TestStoreRefusesABatchKeyReusedForAnotherBatch(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, _, err := store.CreateBatch(t.Context(), newTestBatch(2), IdempotencyKey{Key: "campaign-8", Fingerprint: [32]byte{1}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.CreateBatch(t.Context(), newTestBatch(2), IdempotencyKey{Key: "campaign-8", Fingerprint: [32]byte{2}})
	if !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Errorf("CreateBatch = %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestStoreGetBatchCountsItsNotificationsByStatus(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	nb := newTestBatch(3)
	if _, _, err := store.CreateBatch(t.Context(), nb, IdempotencyKey{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET status = 'sent' WHERE id = $1", nb.Items[0].Notification.ID); err != nil {
		t.Fatal(err)
	}

	b, err := store.GetBatch(t.Context(), nb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 5 || b.Accepted != 3 || len(b.Rejections) != 2 || b.Items != nil {
		t.Errorf("batch = %+v; want the counts and rejections, without items", b)
	}
	if b.Statuses[StatusQueued] != 2 || b.Statuses[StatusSent] != 1 || len(b.Statuses) != 2 {
		t.Errorf("statuses = %v, want 2 queued and 1 sent", b.Statuses)
	}
	if b.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt = %v, want UTC", b.CreatedAt)
	}
}

func TestStoreGetBatchReportsAMissingBatch(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, err := store.GetBatch(t.Context(), uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBatch = %v, want ErrNotFound", err)
	}
}

func TestStoreConcurrentBatchesWithOneKeyCreateOne(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	key := IdempotencyKey{Key: "campaign-9", Fingerprint: [32]byte{9}}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     = map[uuid.UUID]bool{}
		created int
	)
	for range 5 {
		wg.Go(func() {
			b, c, err := store.CreateBatch(t.Context(), newTestBatch(10), key)
			if err != nil {
				t.Errorf("CreateBatch: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids[b.ID] = true
			if c {
				created++
			}
		})
	}
	wg.Wait()

	if created != 1 || len(ids) != 1 {
		t.Errorf("%d batches created, %d IDs returned; want 1 and 1", created, len(ids))
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications"); c != 10 {
		t.Errorf("%d notifications, want 10", c)
	}
}
