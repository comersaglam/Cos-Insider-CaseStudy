package notification

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
)

// Store keeps notifications in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a store over pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create stores n together with its outbox row, in one transaction (ADR 0003), and reports created = true.
//
// When idem carries a key an earlier request used, nothing is written: Create returns that request's notification
// with created = false if the two requests have the same fingerprint, and ErrIdempotencyKeyReused if they differ.
// Concurrent requests with one key are safe: the unique index makes each wait until the first has committed.
func (s *Store) Create(ctx context.Context, n Notification, idem IdempotencyKey) (stored Notification, created bool, err error) {
	params := db.InsertNotificationParams{
		ID:            n.ID,
		Channel:       string(n.Channel),
		Recipient:     n.To,
		Content:       n.Content,
		Priority:      string(n.Priority),
		Status:        string(n.Status),
		CorrelationID: n.CorrelationID,
	}
	if idem.Key != "" {
		params.IdempotencyKey = &idem.Key
		params.RequestHash = idem.Fingerprint[:]
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		row, err := q.InsertNotification(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			// The key is taken by a committed notification.
			existing, err := q.GetNotificationByIdempotencyKey(ctx, &idem.Key)
			if err != nil {
				return fmt.Errorf("read the notification holding the idempotency key: %w", err)
			}
			if !bytes.Equal(existing.RequestHash, params.RequestHash) {
				return ErrIdempotencyKeyReused
			}
			stored = fromRow(existing)
			return nil
		}
		if err != nil {
			return fmt.Errorf("insert notification: %w", err)
		}
		if err := q.InsertOutbox(ctx, row.ID); err != nil {
			return fmt.Errorf("insert outbox row: %w", err)
		}
		stored, created = fromRow(row), true
		return nil
	})
	return stored, created, err
}

// Get returns the notification with the given ID, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Notification, error) {
	row, err := db.New(s.pool).GetNotification(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrNotFound
	}
	if err != nil {
		return Notification{}, fmt.Errorf("read notification: %w", err)
	}
	return fromRow(row), nil
}

// fromRow converts a database row to a Notification. pgx reads timestamptz in the host's time zone; the service
// works in UTC.
func fromRow(r db.Notification) Notification {
	return Notification{
		ID:            r.ID,
		To:            r.Recipient,
		Channel:       Channel(r.Channel),
		Content:       r.Content,
		Priority:      Priority(r.Priority),
		Status:        Status(r.Status),
		CorrelationID: r.CorrelationID,
		CreatedAt:     r.CreatedAt.UTC(),
		UpdatedAt:     r.UpdatedAt.UTC(),
	}
}
