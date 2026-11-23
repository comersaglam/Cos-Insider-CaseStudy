package notification

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// StatusEvent is a committed change of a notification's status (ADR 0033).
type StatusEvent struct {
	NotificationID uuid.UUID  `json:"notificationId"`
	BatchID        *uuid.UUID `json:"batchId,omitzero"`
	Status         Status     `json:"status"`
	At             time.Time  `json:"at"`
}

// StatusListener receives the status changes PostgreSQL announces, on a connection of its own, as OutboxListener
// does for the outbox.
type StatusListener struct {
	conn *pgx.Conn
}

// ListenStatus opens the listener.
func (s *Store) ListenStatus(ctx context.Context) (*StatusListener, error) {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("open the status listener: %w", err)
	}
	if _, err := conn.Exec(ctx, "LISTEN notification_status"); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("listen for status changes: %w", err)
	}
	return &StatusListener{conn: conn}, nil
}

// Next waits for the next status change and returns it, its time in UTC; it returns ctx's error when ctx ends, and
// an error when the connection is lost.
func (l *StatusListener) Next(ctx context.Context) (StatusEvent, error) {
	n, err := l.conn.WaitForNotification(ctx)
	if err != nil {
		return StatusEvent{}, fmt.Errorf("wait for a status change: %w", err)
	}
	var ev StatusEvent
	if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
		return StatusEvent{}, fmt.Errorf("read status change %q: %w", n.Payload, err)
	}
	ev.At = ev.At.UTC()
	return ev, nil
}

// Close closes the listener's connection.
func (l *StatusListener) Close(ctx context.Context) error {
	return l.conn.Close(ctx)
}
