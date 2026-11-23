package notification

import (
	"context"
	"log/slog"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// store is what the service needs from storage; *Store satisfies it.
type store interface {
	Create(ctx context.Context, n Notification, idem IdempotencyKey) (Notification, bool, error)
	Get(ctx context.Context, id uuid.UUID) (Notification, error)
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

	correlationID := observability.CorrelationID(ctx)
	if correlationID == "" {
		correlationID = observability.NewCorrelationID()
	}
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
