package notification

import (
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
)

// newTestStore returns a store over a fresh database of the test's own, and the pool to inspect it with.
func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	pool, err := postgres.Connect(t.Context(), testenv.PostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool), pool
}

var testDraft = Draft{To: "+905551234567", Channel: ChannelSMS, Content: "Your order has shipped.", Priority: PriorityHigh}

// newQueued returns a notification ready to store, built from d.
func newQueued(d Draft) Notification {
	return Notification{
		ID: uuid.NewV7(), To: d.To, Channel: d.Channel, Content: d.Content, Priority: d.Priority,
		Status: StatusQueued, CorrelationID: "corr-1",
	}
}

// count returns the number of rows a query counts.
func count(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStoreCreatesAndReadsANotification(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	n := newQueued(testDraft)

	stored, created, err := store.Create(t.Context(), n, IdempotencyKey{})
	if err != nil || !created {
		t.Fatalf("Create = created %v, err %v; want created", created, err)
	}
	if stored.CreatedAt.IsZero() || stored.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt = %v, want a time in UTC", stored.CreatedAt)
	}
	stored.CreatedAt, stored.UpdatedAt = time.Time{}, time.Time{}
	if stored != n {
		t.Errorf("Create stored\n%+v\nwant\n%+v", stored, n)
	}

	got, err := store.Get(t.Context(), n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != n.ID || got.To != n.To || got.Status != StatusQueued || got.CreatedAt.Location() != time.UTC {
		t.Errorf("Get = %+v", got)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox WHERE notification_id = $1", n.ID); c != 1 {
		t.Errorf("%d outbox rows for the notification, want 1", c)
	}
}

func TestStoreGetReportsAMissingNotification(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, err := store.Get(t.Context(), uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
}

func TestStoreRepeatedRequestReturnsTheFirstNotification(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	key := IdempotencyKey{Key: "order-42-sms", Fingerprint: testDraft.Fingerprint()}

	first, _, err := store.Create(t.Context(), newQueued(testDraft), key)
	if err != nil {
		t.Fatal(err)
	}
	again, created, err := store.Create(t.Context(), newQueued(testDraft), key)
	if err != nil || created || again.ID != first.ID {
		t.Errorf("repeat = %s created %v err %v; want %s, not created", again.ID, created, err, first.ID)
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications"); c != 1 {
		t.Errorf("%d notifications, want 1", c)
	}
	if c := count(t, pool, "SELECT count(*) FROM outbox"); c != 1 {
		t.Errorf("%d outbox rows, want 1", c)
	}
}

func TestStoreRefusesAKeyReusedForAnotherRequest(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, _, err := store.Create(t.Context(), newQueued(testDraft),
		IdempotencyKey{Key: "order-42-sms", Fingerprint: testDraft.Fingerprint()}); err != nil {
		t.Fatal(err)
	}

	other := testDraft
	other.Content = "Your order is late."
	_, _, err := store.Create(t.Context(), newQueued(other),
		IdempotencyKey{Key: "order-42-sms", Fingerprint: other.Fingerprint()})
	if !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Errorf("Create = %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestStoreNeverMergesRequestsWithoutAKey(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	for range 2 {
		if _, created, err := store.Create(t.Context(), newQueued(testDraft), IdempotencyKey{}); err != nil || !created {
			t.Fatalf("Create = created %v, err %v", created, err)
		}
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications"); c != 2 {
		t.Errorf("%d notifications, want 2", c)
	}
}

// TestStoreConcurrentRequestsWithOneKeyCreateOne sends one request twenty times at once, as a client retrying
// over a flaky network might: the unique index makes all but one wait and then find the first.
func TestStoreConcurrentRequestsWithOneKeyCreateOne(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	key := IdempotencyKey{Key: "order-43-sms", Fingerprint: testDraft.Fingerprint()}

	const requests = 20
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     = map[uuid.UUID]bool{}
		created int
	)
	for range requests {
		wg.Go(func() {
			n, c, err := store.Create(t.Context(), newQueued(testDraft), key)
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ids[n.ID] = true
			if c {
				created++
			}
		})
	}
	wg.Wait()

	if created != 1 || len(ids) != 1 {
		t.Errorf("%d requests created a notification and %d IDs were returned; want 1 and 1", created, len(ids))
	}
	if c := count(t, pool, "SELECT count(*) FROM notifications"); c != 1 {
		t.Errorf("%d notifications, want 1", c)
	}
}
