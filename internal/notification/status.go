package notification

import "slices"

// Status is where a notification is in its lifecycle. A notification starts as StatusQueued, or as
// StatusScheduled when it is to be sent later.
type Status string

// The statuses of a notification.
const (
	// StatusScheduled waits for its scheduled time.
	StatusScheduled Status = "scheduled"
	// StatusQueued is due and waits for a worker.
	StatusQueued Status = "queued"
	// StatusSending is claimed by a worker that is calling the provider.
	StatusSending Status = "sending"
	// StatusRetrying failed with a transient error and waits for its next attempt.
	StatusRetrying Status = "retrying"
	// StatusSent was accepted by the provider. It is final.
	StatusSent Status = "sent"
	// StatusFailed failed permanently, ran out of attempts, or had a message that could not be processed; only
	// the retry endpoints move it on.
	StatusFailed Status = "failed"
	// StatusCancelled was cancelled while it waited. It is final.
	StatusCancelled Status = "cancelled"
)

// lifecycle holds every status in the order a notification meets them; it gives lists of statuses a stable order.
var lifecycle = []Status{
	StatusScheduled, StatusQueued, StatusSending, StatusRetrying, StatusSent, StatusFailed, StatusCancelled,
}

// transitions lists, for every status, the statuses it may move to.
var transitions = map[Status][]Status{
	StatusScheduled: {StatusQueued, StatusCancelled},
	StatusQueued:    {StatusSending, StatusCancelled, StatusFailed},
	StatusSending:   {StatusSent, StatusRetrying, StatusFailed},
	StatusRetrying:  {StatusQueued, StatusCancelled},
	StatusFailed:    {StatusQueued},
	StatusSent:      nil,
	StatusCancelled: nil,
}

// Valid reports whether s is one of the known statuses.
func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// CanTransitionTo reports whether a notification in status s may move to status next.
func (s Status) CanTransitionTo(next Status) bool {
	return slices.Contains(transitions[s], next)
}

// StatusesThatCanBecome returns, in lifecycle order, the statuses from which a notification may move to target. A
// conditional UPDATE takes its WHERE clause from it, so the transition table stays the one source of the rules.
func StatusesThatCanBecome(target Status) []Status {
	var from []Status
	for _, s := range lifecycle {
		if s.CanTransitionTo(target) {
			from = append(from, s)
		}
	}
	return from
}
