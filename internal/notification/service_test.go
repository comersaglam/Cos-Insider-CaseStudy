package notification

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// fakeStore records what the service hands it and answers as configured.
type fakeStore struct {
	created     []Notification
	keys        []IdempotencyKey
	replay      bool  // answer Create and CreateBatch as a repeated request
	err         error // answer every call with this error
	notFoundGet bool

	batches       []NewBatch
	batchKeys     []IdempotencyKey
	storedBatch   Batch
	batchNotFound bool

	listed       []Notification // what List answers, before the limit
	listLimits   []int
	failedCount  int
	requeueLimit int
}

func (f *fakeStore) List(_ context.Context, _ ListFilter, _ *Cursor, limit int) ([]Notification, error) {
	f.listLimits = append(f.listLimits, limit)
	return f.listed[:min(limit, len(f.listed))], f.err
}

func (f *fakeStore) Cancel(_ context.Context, id uuid.UUID) (Notification, error) {
	return Notification{ID: id, Status: StatusCancelled}, f.err
}

func (f *fakeStore) Retry(_ context.Context, id uuid.UUID) (Notification, error) {
	return Notification{ID: id, Status: StatusQueued}, f.err
}

func (f *fakeStore) CountFailed(context.Context, RetryFilter) (int, error) {
	return f.failedCount, f.err
}

func (f *fakeStore) RequeueFailed(_ context.Context, _ RetryFilter, maxRows int) (int, error) {
	f.requeueLimit = maxRows
	return min(maxRows, f.failedCount), f.err
}

func (f *fakeStore) Create(_ context.Context, n Notification, idem IdempotencyKey) (Notification, bool, error) {
	if f.err != nil {
		return Notification{}, false, f.err
	}
	f.created = append(f.created, n)
	f.keys = append(f.keys, idem)
	return n, !f.replay, nil
}

func (f *fakeStore) Get(_ context.Context, id uuid.UUID) (Notification, error) {
	if f.notFoundGet {
		return Notification{}, ErrNotFound
	}
	return Notification{ID: id}, f.err
}

func (f *fakeStore) CreateBatch(_ context.Context, b NewBatch, idem IdempotencyKey) (Batch, bool, error) {
	if f.err != nil {
		return Batch{}, false, f.err
	}
	f.batches = append(f.batches, b)
	f.batchKeys = append(f.batchKeys, idem)
	stored := Batch{ID: b.ID, Total: b.Total, Accepted: len(b.Items), Rejections: b.Rejections}
	for _, item := range b.Items {
		stored.Items = append(stored.Items, BatchItem{Index: item.Index, ID: item.Notification.ID})
	}
	return stored, !f.replay, nil
}

func (f *fakeStore) GetBatch(_ context.Context, _ uuid.UUID) (Batch, error) {
	if f.batchNotFound {
		return Batch{}, ErrNotFound
	}
	return f.storedBatch, nil
}

func newTestService(store *fakeStore) *Service {
	return NewService(store, slog.New(slog.DiscardHandler))
}

func TestServiceCreateRefusesAnInvalidDraftWithoutStoringIt(t *testing.T) {
	store := &fakeStore{}
	_, _, err := newTestService(store).Create(t.Context(), Draft{Channel: ChannelSMS}, "")

	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Errorf("Create = %v, want a *ValidationError", err)
	}
	if len(store.created) != 0 {
		t.Error("an invalid draft was stored")
	}
}

func TestServiceCreateStoresAQueuedNotification(t *testing.T) {
	store := &fakeStore{}
	ctx := observability.WithCorrelationID(t.Context(), "corr-9")

	n, created, err := newTestService(store).Create(ctx, testDraft, "order-42-sms")
	if err != nil || !created {
		t.Fatalf("Create = created %v, err %v", created, err)
	}
	want := Notification{
		ID: n.ID, To: testDraft.To, Channel: testDraft.Channel, Content: testDraft.Content,
		Priority: testDraft.Priority, Status: StatusQueued, CorrelationID: "corr-9",
	}
	if store.created[0] != want {
		t.Errorf("stored\n%+v\nwant\n%+v", store.created[0], want)
	}
	if n.ID == (uuid.UUID{}) {
		t.Error("the notification has no ID")
	}
	if got := store.keys[0]; got.Key != "order-42-sms" || got.Fingerprint != testDraft.Fingerprint() {
		t.Errorf("idempotency key = %+v, want the key with the draft's fingerprint", got)
	}
}

func TestServiceCreateWithoutAKeyPassesNoKey(t *testing.T) {
	store := &fakeStore{}
	if _, _, err := newTestService(store).Create(t.Context(), testDraft, ""); err != nil {
		t.Fatal(err)
	}
	if store.keys[0] != (IdempotencyKey{}) {
		t.Errorf("idempotency key = %+v, want none", store.keys[0])
	}
}

// TestServiceCreateGivesACorrelationIDWhenTheContextHasNone covers callers outside HTTP, whose context carries no
// ID; the column is NOT NULL and the ID is what joins the logs.
func TestServiceCreateGivesACorrelationIDWhenTheContextHasNone(t *testing.T) {
	store := &fakeStore{}
	if _, _, err := newTestService(store).Create(t.Context(), testDraft, ""); err != nil {
		t.Fatal(err)
	}
	if !observability.ValidCorrelationID(store.created[0].CorrelationID) {
		t.Errorf("correlation ID = %q, want a generated one", store.created[0].CorrelationID)
	}
}

func TestServiceGet(t *testing.T) {
	id := uuid.NewV7()
	if n, err := newTestService(&fakeStore{}).Get(t.Context(), id); err != nil || n.ID != id {
		t.Errorf("Get = %v, %v", n.ID, err)
	}
	if _, err := newTestService(&fakeStore{notFoundGet: true}).Get(t.Context(), id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
}
