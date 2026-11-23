package notification

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
)

// List returns up to limit notifications that match f, newest first, starting after the cursor when it is not nil.
func (s *Store) List(ctx context.Context, f ListFilter, after *Cursor, limit int) ([]Notification, error) {
	params := db.ListNotificationsParams{BatchID: f.BatchID, PageSize: limit}
	for _, st := range f.Statuses {
		params.Statuses = append(params.Statuses, string(st))
	}
	for _, ch := range f.Channels {
		params.Channels = append(params.Channels, string(ch))
	}
	if !f.CreatedFrom.IsZero() {
		params.CreatedFrom = &f.CreatedFrom
	}
	if !f.CreatedTo.IsZero() {
		params.CreatedTo = &f.CreatedTo
	}
	if after != nil {
		params.AfterCreatedAt, params.AfterID = &after.CreatedAt, &after.ID
	}

	rows, err := db.New(s.pool).ListNotifications(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	out := make([]Notification, len(rows))
	for i, r := range rows {
		out[i] = fromRow(r)
	}
	return out, nil
}

// Cancel cancels a notification that is still waiting, in one conditional UPDATE, so a worker claiming it at the
// same moment and the cancel cannot both win. A notification already cancelled is returned as it is; one that is
// being sent or is finished gives ErrNotCancellable, naming its status.
func (s *Store) Cancel(ctx context.Context, id uuid.UUID) (Notification, error) {
	var cancellable []string
	for _, st := range StatusesThatCanBecome(StatusCancelled) {
		cancellable = append(cancellable, string(st))
	}
	q := db.New(s.pool)
	row, err := q.CancelNotification(ctx, db.CancelNotificationParams{ID: id, Cancellable: cancellable})
	if err == nil {
		return fromRow(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, fmt.Errorf("cancel notification: %w", err)
	}

	current, err := s.Get(ctx, id)
	switch {
	case err != nil:
		return Notification{}, err
	case current.Status == StatusCancelled:
		return current, nil
	}
	return Notification{}, fmt.Errorf("%w: it is %s", ErrNotCancellable, current.Status)
}

// Retry puts a failed notification back in the queue with a new outbox row, in one transaction. A notification
// that is not failed gives ErrNotRetryable, naming its status.
func (s *Store) Retry(ctx context.Context, id uuid.UUID) (Notification, error) {
	var out Notification
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		row, err := q.RequeueFailedNotification(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			current, err := q.GetNotification(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("read notification: %w", err)
			}
			return fmt.Errorf("%w: it is %s", ErrNotRetryable, current.Status)
		}
		if err != nil {
			return fmt.Errorf("requeue notification: %w", err)
		}
		if err := q.InsertOutbox(ctx, row.ID); err != nil {
			return fmt.Errorf("insert outbox row: %w", err)
		}
		out = fromRow(row)
		return nil
	})
	return out, err
}

// CountFailed counts the failed notifications that f selects.
func (s *Store) CountFailed(ctx context.Context, f RetryFilter) (int, error) {
	n, err := db.New(s.pool).CountFailedNotifications(ctx, db.CountFailedNotificationsParams{
		FailedFrom: f.FailedFrom, FailedTo: f.FailedTo, Channel: channelParam(f.Channel), BatchID: f.BatchID,
	})
	if err != nil {
		return 0, fmt.Errorf("count failed notifications: %w", err)
	}
	return n, nil
}

// RequeueFailed puts up to maxRows of the failed notifications f selects back in the queue, oldest failure first,
// with their outbox rows, in one statement, and returns how many it requeued.
func (s *Store) RequeueFailed(ctx context.Context, f RetryFilter, maxRows int) (int, error) {
	n, err := db.New(s.pool).RequeueFailedNotifications(ctx, db.RequeueFailedNotificationsParams{
		FailedFrom: f.FailedFrom, FailedTo: f.FailedTo, Channel: channelParam(f.Channel), BatchID: f.BatchID,
		MaxRows: maxRows,
	})
	if err != nil {
		return 0, fmt.Errorf("requeue failed notifications: %w", err)
	}
	return int(n), nil
}

// channelParam is nil, selecting every channel, for the empty channel.
func channelParam(c Channel) *string {
	if c == "" {
		return nil
	}
	s := string(c)
	return &s
}
