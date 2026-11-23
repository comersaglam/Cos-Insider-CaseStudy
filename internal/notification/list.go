package notification

import (
	"errors"
	"time"
	"uuid"
)

// MaxRequeue is the most notifications one bulk retry puts back in the queue. At 100 messages per second per
// channel it is under two minutes of sending, and one transaction writes it in well under a second; a larger
// recovery is a few requests (ADR 0014).
const MaxRequeue = 10_000

// ErrNotCancellable reports a notification that is no longer waiting: it is being sent, or is finished.
var ErrNotCancellable = errors.New("notification can no longer be cancelled")

// ErrNotRetryable reports a notification that is not failed.
var ErrNotRetryable = errors.New("only a failed notification can be retried")

// ListFilter selects the notifications a list shows. A zero field selects everything.
type ListFilter struct {
	Statuses    []Status
	Channels    []Channel
	BatchID     *uuid.UUID
	CreatedFrom time.Time // inclusive
	CreatedTo   time.Time // exclusive
}

// Cursor marks the last notification of a page; the next page starts after it. Lists are ordered newest first by
// creation time, then by ID, so notifications created in one transaction, which share a time, still have one order
// and none is lost or repeated between pages.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Page is one page of a list, and the cursor of the next page, or nil on the last one.
type Page struct {
	Items []Notification
	Next  *Cursor
}

// RetryFilter selects failed notifications to send again, by when they failed.
type RetryFilter struct {
	Channel    Channel // "" for every channel
	BatchID    *uuid.UUID
	FailedFrom time.Time // inclusive, required
	FailedTo   time.Time // exclusive, required
	DryRun     bool
}

// RetryResult tells how many failed notifications matched a bulk retry and how many it put back in the queue: none
// on a dry run, at most MaxRequeue otherwise. Repeating the request requeues the rest.
type RetryResult struct {
	Matched  int
	Requeued int
	DryRun   bool
}
