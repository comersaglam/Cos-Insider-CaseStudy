package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// store is what the service needs from storage; *Store satisfies it.
type store interface {
	Create(ctx context.Context, n Notification, idem IdempotencyKey) (Notification, bool, error)
	Get(ctx context.Context, id uuid.UUID) (Notification, error)
	CreateBatch(ctx context.Context, b NewBatch, idem IdempotencyKey) (Batch, bool, error)
	GetBatch(ctx context.Context, id uuid.UUID) (Batch, error)
	List(ctx context.Context, f ListFilter, after *Cursor, limit int) ([]Notification, error)
	Cancel(ctx context.Context, id uuid.UUID) (Notification, error)
	Retry(ctx context.Context, id uuid.UUID) (Notification, error)
	CountFailed(ctx context.Context, f RetryFilter) (int, error)
	RequeueFailed(ctx context.Context, f RetryFilter, maxRows int) (int, error)
}

// Service holds the notification use cases. It knows the rules; it neither reads requests nor writes SQL.
type Service struct {
	store store
	log   *slog.Logger
}

// NewService returns a service over the given storage.
func NewService(s store, log *slog.Logger) *Service {
	return &Service{store: s, log: log}
}

// Create validates d and stores it as a queued notification, with its outbox row, so that it is sent. An
// idempotency key, when not empty, makes a repeated request return the first notification with created = false;
// the same key with a different request is refused with ErrIdempotencyKeyReused. An invalid draft is refused with
// a *ValidationError and nothing is stored.
func (s *Service) Create(ctx context.Context, d Draft, idempotencyKey string) (n Notification, created bool, err error) {
	if err := d.Validate(); err != nil {
		return Notification{}, false, err
	}

	correlationID := correlationIDOf(ctx)
	var idem IdempotencyKey
	if idempotencyKey != "" {
		idem = IdempotencyKey{Key: idempotencyKey, Fingerprint: d.Fingerprint()}
	}

	n, created, err = s.store.Create(ctx, Notification{
		ID:            uuid.NewV7(),
		To:            d.To,
		Channel:       d.Channel,
		Content:       d.Content,
		Priority:      d.Priority,
		Status:        StatusQueued,
		CorrelationID: correlationID,
	}, idem)
	if err != nil {
		return Notification{}, false, err
	}
	s.log.InfoContext(ctx, "notification accepted",
		"notification_id", n.ID, "channel", n.Channel, "priority", n.Priority,
		"to", observability.MaskRecipient(n.To), "replayed", !created)
	return n, created, nil
}

// Get returns the notification with the given ID, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Notification, error) {
	return s.store.Get(ctx, id)
}

// CreateBatch accepts the valid drafts of a batch as queued notifications and records the invalid ones with their
// reasons; one invalid item does not refuse the others. A batch with no item, or more than MaxBatchSize, is refused
// with a *ValidationError, and one in which no item is valid with a *BatchError; neither is stored. An idempotency
// key, when not empty, covers the whole batch as Create's covers one notification.
func (s *Service) CreateBatch(ctx context.Context, drafts []Draft, idempotencyKey string) (b Batch, created bool, err error) {
	switch {
	case len(drafts) == 0:
		return Batch{}, false, &ValidationError{Fields: []FieldError{
			{Field: "notifications", Code: CodeRequired, Message: "must hold at least one notification"}}}
	case len(drafts) > MaxBatchSize:
		return Batch{}, false, &ValidationError{Fields: []FieldError{
			{Field: "notifications", Code: CodeTooLong, Message: fmt.Sprintf("must hold at most %d notifications", MaxBatchSize)}}}
	}

	correlationID := correlationIDOf(ctx)
	nb := NewBatch{ID: uuid.NewV7(), Total: len(drafts), CorrelationID: correlationID}
	for i, d := range drafts {
		if err := d.Validate(); err != nil {
			verr, ok := errors.AsType[*ValidationError](err)
			if !ok {
				return Batch{}, false, err
			}
			nb.Rejections = append(nb.Rejections, Rejection{Index: i, Fields: verr.Fields})
			continue
		}
		nb.Items = append(nb.Items, NewBatchItem{Index: i, Notification: Notification{
			ID: uuid.NewV7(), To: d.To, Channel: d.Channel, Content: d.Content, Priority: d.Priority,
			Status: StatusQueued, CorrelationID: correlationID,
		}})
	}
	if len(nb.Items) == 0 {
		return Batch{}, false, &BatchError{Rejections: nb.Rejections}
	}

	var idem IdempotencyKey
	if idempotencyKey != "" {
		idem = IdempotencyKey{Key: idempotencyKey, Fingerprint: BatchFingerprint(drafts)}
	}
	b, created, err = s.store.CreateBatch(ctx, nb, idem)
	if err != nil {
		return Batch{}, false, err
	}
	s.log.InfoContext(ctx, "batch accepted",
		"batch_id", b.ID, "total", b.Total, "accepted", b.Accepted, "rejected", len(b.Rejections), "replayed", !created)
	return b, created, nil
}

// GetBatch returns the batch with the given ID, with its accepted notifications counted by status, or ErrNotFound.
func (s *Service) GetBatch(ctx context.Context, id uuid.UUID) (Batch, error) {
	return s.store.GetBatch(ctx, id)
}

// correlationIDOf returns ctx's correlation ID, or a new one for callers outside HTTP, whose context carries none;
// the column is NOT NULL, and the ID is what joins the logs.
func correlationIDOf(ctx context.Context) string {
	if id := observability.CorrelationID(ctx); id != "" {
		return id
	}
	return observability.NewCorrelationID()
}
