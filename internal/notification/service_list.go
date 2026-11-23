package notification

import (
	"context"
	"uuid"
)

// List returns a page of up to limit notifications that match f, newest first, after the cursor when it is not
// nil, and the cursor of the next page when another follows.
func (s *Service) List(ctx context.Context, f ListFilter, after *Cursor, limit int) (Page, error) {
	// One row more than the page tells whether another page follows, without counting.
	rows, err := s.store.List(ctx, f, after, limit+1)
	if err != nil {
		return Page{}, err
	}
	if len(rows) <= limit {
		return Page{Items: rows}, nil
	}
	last := rows[limit-1]
	return Page{Items: rows[:limit], Next: &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}}, nil
}

// Cancel cancels a notification that is still waiting (ADR 0011). One already cancelled is returned as it is, so
// a repeated request is safe; one being sent or finished gives ErrNotCancellable.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (Notification, error) {
	n, err := s.store.Cancel(ctx, id)
	if err != nil {
		return Notification{}, err
	}
	s.log.InfoContext(ctx, "notification cancelled", "notification_id", n.ID)
	return n, nil
}

// Retry puts a failed notification back in the queue, to be sent again; any other gives ErrNotRetryable.
func (s *Service) Retry(ctx context.Context, id uuid.UUID) (Notification, error) {
	n, err := s.store.Retry(ctx, id)
	if err != nil {
		return Notification{}, err
	}
	s.log.InfoContext(ctx, "notification retried", "notification_id", n.ID)
	return n, nil
}

// RetryFailed puts the failed notifications f selects back in the queue, at most MaxRequeue of them, oldest
// failure first (ADR 0014). The failure window is required; a dry run only counts. An invalid filter is refused
// with a *ValidationError.
func (s *Service) RetryFailed(ctx context.Context, f RetryFilter) (RetryResult, error) {
	var fields []FieldError
	if f.FailedFrom.IsZero() {
		fields = append(fields, FieldError{"failedFrom", CodeRequired, "is required"})
	}
	switch {
	case f.FailedTo.IsZero():
		fields = append(fields, FieldError{"failedTo", CodeRequired, "is required"})
	case !f.FailedFrom.IsZero() && !f.FailedTo.After(f.FailedFrom):
		fields = append(fields, FieldError{"failedTo", CodeInvalid, "must be after failedFrom"})
	}
	if f.Channel != "" && !f.Channel.Valid() {
		fields = append(fields, FieldError{"channel", CodeInvalid, "must be one of sms, email, push"})
	}
	if len(fields) > 0 {
		return RetryResult{}, &ValidationError{Fields: fields}
	}

	matched, err := s.store.CountFailed(ctx, f)
	if err != nil {
		return RetryResult{}, err
	}
	if f.DryRun {
		return RetryResult{Matched: matched, DryRun: true}, nil
	}
	requeued, err := s.store.RequeueFailed(ctx, f, MaxRequeue)
	if err != nil {
		return RetryResult{}, err
	}
	s.log.InfoContext(ctx, "failed notifications retried", "matched", matched, "requeued", requeued,
		"channel", f.Channel, "failed_from", f.FailedFrom, "failed_to", f.FailedTo)
	return RetryResult{Matched: matched, Requeued: requeued}, nil
}
