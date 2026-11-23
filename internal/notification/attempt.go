package notification

import (
	"errors"
	"time"
	"uuid"
)

// AttemptOutcome is how an attempt to send ended, as the attempt's record keeps it (ADR 0007).
type AttemptOutcome string

// The outcomes of an attempt.
const (
	OutcomeSuccess        AttemptOutcome = "success"
	OutcomePermanent      AttemptOutcome = "permanent"
	OutcomePermanentAlert AttemptOutcome = "permanent_alert"
	OutcomeTransient      AttemptOutcome = "transient"
	// OutcomeUnknown: the worker that made the attempt stopped before recording how it ended (ADR 0012).
	OutcomeUnknown AttemptOutcome = "unknown"
)

// FailureReason says why a notification failed.
type FailureReason string

// The reasons a notification fails.
const (
	FailureProviderRejected  FailureReason = "provider_rejected"
	FailureAttemptsExhausted FailureReason = "attempts_exhausted"
	FailureUnprocessable     FailureReason = "unprocessable_message"
)

// ErrNotClaimable reports a notification that is not there to be sent: it is finished, cancelled, waiting for a
// later time, out of attempts, or held by another worker whose lease has not run out.
var ErrNotClaimable = errors.New("notification is not there to be sent")

// ErrClaimLost reports an outcome recorded after the claim's lease ran out and another took the notification over:
// the attempt is kept in the record, but the notification is left as the newer claim has it.
var ErrClaimLost = errors.New("the claim was taken over before the outcome was recorded")

// Claim is a notification claimed for sending, with one more attempt spent.
type Claim struct {
	Notification Notification
	// TakenOver: the notification had been claimed before and that lease ran out. The earlier worker may have
	// sent it, so this attempt may send it twice; it is counted (ADR 0012).
	TakenOver bool
}

// Attempt is one attempt's record and what becomes of the notification after it.
type Attempt struct {
	NotificationID uuid.UUID
	Number         int // the attempt's number, as the claim counted it
	Outcome        AttemptOutcome
	StatusCode     int // the provider's HTTP status, 0 without an answer
	Error          string
	Duration       time.Duration

	// Next is the notification's status after the attempt: sent, retrying or failed.
	Next              Status
	NextAttemptAt     time.Time     // when Next is retrying
	FailureReason     FailureReason // when Next is failed
	ProviderMessageID string        // when Next is sent
}

// AttemptRecord is a stored attempt.
type AttemptRecord struct {
	Number     int
	Outcome    AttemptOutcome
	StatusCode int
	Error      string
	Duration   time.Duration
	CreatedAt  time.Time
}
