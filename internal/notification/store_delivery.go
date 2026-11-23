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

// Claim claims a notification for sending (ADR 0012): it must be queued, or sending under a lease that ran out,
// with attempts left. The claim spends an attempt and holds the notification for lease. A notification that is not
// there to be sent gives ErrNotClaimable, naming its status; a missing one, ErrNotFound.
func (s *Store) Claim(ctx context.Context, id uuid.UUID, lease time.Duration, maxAttempts int) (Claim, error) {
	row, err := db.New(s.pool).ClaimNotification(ctx, db.ClaimNotificationParams{
		ID: id, LeaseMs: int(lease.Milliseconds()), MaxAttempts: maxAttempts,
	})
	if err == nil {
		return Claim{Notification: fromRow(row.Notification), TakenOver: row.PreviousStatus == string(StatusSending)}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, fmt.Errorf("claim notification: %w", err)
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return Claim{}, err
	}
	return Claim{}, fmt.Errorf("%w: it is %s after %d attempts", ErrNotClaimable, current.Status, current.Attempts)
}

// FinishAttempt records an attempt and moves the notification to what came of it, in one transaction. If the
// claim was taken over meanwhile, the attempt is still recorded, the notification is left as the newer claim has
// it, and FinishAttempt returns ErrClaimLost.
func (s *Store) FinishAttempt(ctx context.Context, a Attempt) error {
	record := db.InsertDeliveryAttemptParams{
		NotificationID: a.NotificationID, Attempt: a.Number, Outcome: string(a.Outcome),
		DurationMs: int(a.Duration.Milliseconds()),
	}
	if a.StatusCode != 0 {
		record.StatusCode = &a.StatusCode
	}
	if a.Error != "" {
		record.Error = &a.Error
	}
	finish := db.FinishSendingParams{ID: a.NotificationID, Attempt: a.Number, Status: string(a.Next)}
	switch a.Next {
	case StatusRetrying:
		finish.NextAttemptAt = &a.NextAttemptAt
	case StatusFailed:
		reason := string(a.FailureReason)
		finish.FailureReason = &reason
	case StatusSent:
		if a.ProviderMessageID != "" {
			finish.ProviderMessageID = &a.ProviderMessageID
		}
	}

	var lost bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := q.InsertDeliveryAttempt(ctx, record); err != nil {
			return fmt.Errorf("record the attempt: %w", err)
		}
		n, err := q.FinishSending(ctx, finish)
		if err != nil {
			return fmt.Errorf("record the outcome: %w", err)
		}
		lost = n == 0
		return nil
	})
	if err != nil {
		return err
	}
	if lost {
		return ErrClaimLost
	}
	return nil
}

// ReleaseClaim undoes a claim whose send never started, when the circuit breaker refused it: the notification is
// queued again and the attempt given back (ADR 0024). A claim that has changed hands is left alone.
func (s *Store) ReleaseClaim(ctx context.Context, id uuid.UUID, attempt int) error {
	if err := db.New(s.pool).ReleaseClaim(ctx, db.ReleaseClaimParams{ID: id, Attempt: attempt}); err != nil {
		return fmt.Errorf("release claim: %w", err)
	}
	return nil
}

// MarkUnprocessable fails a queued notification whose message cannot be processed (ADR 0014). A notification in
// any other status is left as it is.
func (s *Store) MarkUnprocessable(ctx context.Context, id uuid.UUID) error {
	if err := db.New(s.pool).MarkUnprocessable(ctx, id); err != nil {
		return fmt.Errorf("mark notification unprocessable: %w", err)
	}
	return nil
}

// ReapExpiredClaims returns up to limit notifications whose worker's lease ran out to retrying, due at once, or to
// failed when they have no attempt left, recording the lost attempt as unknown; it returns how many it reaped.
func (s *Store) ReapExpiredClaims(ctx context.Context, maxAttempts, limit int) (int, error) {
	n, err := db.New(s.pool).ReapExpiredClaims(ctx, db.ReapExpiredClaimsParams{MaxAttempts: maxAttempts, BatchSize: limit})
	if err != nil {
		return 0, fmt.Errorf("reap expired claims: %w", err)
	}
	return int(n), nil
}

// Attempts returns the recorded attempts of a notification, in order.
func (s *Store) Attempts(ctx context.Context, id uuid.UUID) ([]AttemptRecord, error) {
	rows, err := db.New(s.pool).ListDeliveryAttempts(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	out := make([]AttemptRecord, len(rows))
	for i, r := range rows {
		out[i] = AttemptRecord{
			Number: r.Attempt, Outcome: AttemptOutcome(r.Outcome), Error: deref(r.Error),
			Duration: time.Duration(r.DurationMs) * time.Millisecond, CreatedAt: r.CreatedAt.UTC(),
		}
		if r.StatusCode != nil {
			out[i].StatusCode = *r.StatusCode
		}
	}
	return out, nil
}
