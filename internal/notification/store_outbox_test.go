package notification

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

// collect is a publish function that records what it is handed.
type collect struct {
	mu      sync.Mutex
	entries []OutboxEntry
	err     error
}

func (c *collect) publish(_ context.Context, entries []OutboxEntry) error {
	if c.err != nil {
		return c.err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entries...)
	return nil
}

func TestDrainOutboxPublishesOldestFirstAndDeletes(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	first := mustCreate(t, store, newQueued(testDraft))
	second := newQueued(Draft{To: "a@example.com", Channel: ChannelEmail, Content: "x", Priority: PriorityLow})
	second.CorrelationID = "corr-2"
	mustCreate(t, store, second)

	var c collect
	n, err := store.DrainOutbox(t.Context(), 10, c.publish)
	if err != nil || n != 2 {
		t.Fatalf("DrainOutbox = %d, %v; want 2", n, err)
	}
	want := []OutboxEntry{
		{NotificationID: first.ID, Channel: ChannelSMS, Priority: PriorityHigh, CorrelationID: "corr-1"},
		{NotificationID: second.ID, Channel: ChannelEmail, Priority: PriorityLow, CorrelationID: "corr-2"},
	}
	if !slices.Equal(c.entries, want) {
		t.Errorf("published %+v, want %+v", c.entries, want)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 0 {
		t.Errorf("%d outbox rows left, want 0", c)
	}
}

func TestDrainOutboxKeepsTheRowsWhenPublishingFails(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	for range 3 {
		mustCreate(t, store, newQueued(testDraft))
	}

	failing := collect{err: errors.New("RabbitMQ is down")}
	if _, err := store.DrainOutbox(t.Context(), 10, failing.publish); err == nil {
		t.Fatal("DrainOutbox reported no error when publishing failed")
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 3 {
		t.Fatalf("%d outbox rows after a failed publish, want all 3 kept", c)
	}

	var ok collect
	if n, err := store.DrainOutbox(t.Context(), 10, ok.publish); err != nil || n != 3 {
		t.Errorf("once RabbitMQ is back: DrainOutbox = %d, %v; want 3", n, err)
	}
}

func TestDrainOutboxTakesAtMostTheLimit(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	for range 5 {
		mustCreate(t, store, newQueued(testDraft))
	}
	var c collect
	if n, err := store.DrainOutbox(t.Context(), 2, c.publish); err != nil || n != 2 {
		t.Fatalf("DrainOutbox = %d, %v; want 2", n, err)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 3 {
		t.Errorf("%d outbox rows left, want 3", c)
	}
}

// TestConcurrentDrainsPublishEachRowOnce runs four relays at once over fifty rows: SKIP LOCKED gives each row to
// exactly one of them.
func TestConcurrentDrainsPublishEachRowOnce(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	for range 50 {
		mustCreate(t, store, newQueued(testDraft))
	}

	var (
		c  collect
		wg sync.WaitGroup
	)
	for range 4 {
		wg.Go(func() {
			for {
				n, err := store.DrainOutbox(t.Context(), 5, c.publish)
				if err != nil {
					t.Error(err)
					return
				}
				if n == 0 {
					return
				}
			}
		})
	}
	wg.Wait()

	seen := map[uuid.UUID]bool{}
	for _, e := range c.entries {
		seen[e.NotificationID] = true
	}
	if len(c.entries) != 50 || len(seen) != 50 {
		t.Errorf("published %d entries for %d notifications, want 50 and 50", len(c.entries), len(seen))
	}
}

// makeWaiting puts a notification in a waiting status with its due time, as the worker or a scheduled create will.
func makeWaiting(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, s Status, due time.Time) {
	t.Helper()
	// A scheduled notification also keeps the time it was scheduled for.
	if _, err := pool.Exec(t.Context(), `UPDATE notifications SET status = $2, next_attempt_at = $3,
		scheduled_at = CASE WHEN $2 = 'scheduled' THEN $3 ELSE scheduled_at END WHERE id = $1`, id, string(s), due); err != nil {
		t.Fatal(err)
	}
}

func TestQueueDueQueuesOnlyWhatIsDue(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	retryDue := mustCreate(t, store, newQueued(testDraft))
	scheduledDue := mustCreate(t, store, newQueued(testDraft))
	notYet := mustCreate(t, store, newQueued(testDraft))
	failed := mustCreate(t, store, newQueued(testDraft))
	makeWaiting(t, pool, retryDue.ID, StatusRetrying, time.Now().Add(-time.Minute))
	makeWaiting(t, pool, scheduledDue.ID, StatusScheduled, time.Now().Add(-time.Second))
	makeWaiting(t, pool, notYet.ID, StatusRetrying, time.Now().Add(time.Hour))
	fail(t, pool, failed.ID, time.Now().Add(-time.Hour))
	if _, err := pool.Exec(t.Context(), "DELETE FROM outbox"); err != nil {
		t.Fatal(err)
	}

	if n, err := store.QueueDue(t.Context(), 100); err != nil || n != 2 {
		t.Fatalf("QueueDue = %d, %v; want the 2 due", n, err)
	}
	for id, want := range map[uuid.UUID]Status{retryDue.ID: StatusQueued, scheduledDue.ID: StatusQueued, notYet.ID: StatusRetrying, failed.ID: StatusFailed} {
		if n, _ := store.Get(t.Context(), id); n.Status != want {
			t.Errorf("%s is %s, want %s", id, n.Status, want)
		}
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 2 {
		t.Errorf("%d outbox rows, want one for each queued notification", c)
	}
}

func TestConcurrentSchedulersQueueEachNotificationOnce(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	for range 30 {
		n := mustCreate(t, store, newQueued(testDraft))
		makeWaiting(t, pool, n.ID, StatusRetrying, time.Now().Add(-time.Minute))
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM outbox"); err != nil {
		t.Fatal(err)
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
	)
	for range 3 {
		wg.Go(func() {
			for {
				n, err := store.QueueDue(t.Context(), 5)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				total += n
				mu.Unlock()
				if n == 0 {
					return
				}
			}
		})
	}
	wg.Wait()
	if c := count(t, pool, "SELECT count(*) FROM outbox"); total != 30 || c != 30 {
		t.Errorf("queued %d with %d outbox rows, want 30 and 30", total, c)
	}
}

func TestOutboxListenerWakesWhenRowsAreCommitted(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	l, err := store.ListenOutbox(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close(context.Background()) })

	// Nothing committed: Wait gives up after its timeout, twice, and the connection stays usable.
	for range 2 {
		start := time.Now()
		if err := l.Wait(t.Context(), 100*time.Millisecond); err != nil {
			t.Fatalf("Wait with nothing committed = %v, want nil after the timeout", err)
		}
		if waited := time.Since(start); waited < 90*time.Millisecond {
			t.Fatalf("Wait returned after %v, before its timeout", waited)
		}
	}

	go func() {
		// Not mustCreate: t.Fatal must not be called outside the test's own goroutine.
		if _, _, err := store.Create(t.Context(), newQueued(testDraft), IdempotencyKey{}); err != nil {
			t.Error(err)
		}
	}()
	start := time.Now()
	if err := l.Wait(t.Context(), 10*time.Second); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("woken %v after the commit; the notification did not arrive", waited)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := l.Wait(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait on an ended context = %v, want context.Canceled", err)
	}
}

func TestStoreCancelsAScheduledNotification(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	n := mustCreate(t, store, newQueued(testDraft))
	makeWaiting(t, pool, n.ID, StatusScheduled, time.Now().Add(time.Hour))
	if got, err := store.Cancel(t.Context(), n.ID); err != nil || got.Status != StatusCancelled {
		t.Errorf("Cancel = %s, %v; want cancelled", got.Status, err)
	}
}
