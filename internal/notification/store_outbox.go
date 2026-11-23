package notification

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
)

// OutboxEntry is an outbox row with what publishing its notification needs.
type OutboxEntry struct {
	NotificationID uuid.UUID
	Channel        Channel
	Priority       Priority
	CorrelationID  string
	TraceParent    string // the trace the notification was created in, or ""
}

// DrainOutbox takes up to limit outbox rows, oldest first, hands them to publish, and deletes them once publish
// returns nil, all in one transaction (ADR 0003). Rows another relay holds are skipped; rows whose publish fails
// stay for the next attempt. If the commit fails after a successful publish, the rows are published again later,
// and consumers drop the duplicates (ADR 0012). It returns how many rows it published.
func (s *Store) DrainOutbox(ctx context.Context, limit int, publish func(context.Context, []OutboxEntry) error) (int, error) {
	var published int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		rows, err := q.ClaimOutbox(ctx, limit)
		if err != nil {
			return fmt.Errorf("claim outbox rows: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		entries := make([]OutboxEntry, len(rows))
		ids := make([]int64, len(rows))
		for i, r := range rows {
			entries[i] = OutboxEntry{
				NotificationID: r.NotificationID, Channel: Channel(r.Channel), Priority: Priority(r.Priority),
				CorrelationID: r.CorrelationID, TraceParent: deref(r.TraceParent),
			}
			ids[i] = r.ID
		}
		if err := publish(ctx, entries); err != nil {
			return err
		}
		if err := q.DeleteOutbox(ctx, ids); err != nil {
			return fmt.Errorf("delete published outbox rows: %w", err)
		}
		published = len(rows)
		return nil
	})
	return published, err
}

// QueueDue queues up to limit waiting notifications (scheduled or retrying) whose time has come, soonest first,
// with their outbox rows, and returns how many it queued.
func (s *Store) QueueDue(ctx context.Context, limit int) (int, error) {
	n, err := db.New(s.pool).QueueDueNotifications(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("queue due notifications: %w", err)
	}
	return int(n), nil
}

// OutboxListener waits for rows to be committed to the outbox, on a connection of its own that listens for the
// notification the outbox's trigger sends.
type OutboxListener struct {
	conn *pgx.Conn
}

// ListenOutbox opens the listener. A listening connection is not returned to the pool, since it would keep
// listening for whoever borrowed it next.
func (s *Store) ListenOutbox(ctx context.Context) (*OutboxListener, error) {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("open the outbox listener: %w", err)
	}
	if _, err := conn.Exec(ctx, "LISTEN outbox"); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("listen for the outbox: %w", err)
	}
	return &OutboxListener{conn: conn}, nil
}

// Wait returns nil when rows have been committed to the outbox since the last call, or when timeout passes, the
// fallback against a lost notification; and ctx's error when ctx ends.
func (l *OutboxListener) Wait(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := l.conn.WaitForNotification(waitCtx)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(waitCtx.Err(), context.DeadlineExceeded):
		return nil
	}
	return fmt.Errorf("wait for the outbox: %w", err)
}

// Close closes the listener's connection.
func (l *OutboxListener) Close(ctx context.Context) error {
	return l.conn.Close(ctx)
}
