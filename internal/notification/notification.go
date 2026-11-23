package notification

import (
	"errors"
	"time"
	"uuid"
)

// Notification is a notification as it is stored.
type Notification struct {
	ID            uuid.UUID
	To            string
	Channel       Channel
	Content       string
	Priority      Priority
	Status        Status
	CorrelationID string
	BatchID       *uuid.UUID // the batch it came in, or nil
	ScheduledAt   *time.Time // the time it was scheduled for, or nil
	TemplateID    *uuid.UUID // the template it was rendered from, or nil
	// TraceParent is the W3C traceparent of the request that created it, or "" (ADR 0034).
	TraceParent   string
	FailedAt      *time.Time // when it failed, while it is failed
	Attempts      int        // attempts spent since it was created or last retried by hand
	FailureReason FailureReason
	// ProviderMessageID is the provider's ID for the message, once sent.
	ProviderMessageID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ErrNotFound reports that no notification has the requested ID.
var ErrNotFound = errors.New("notification not found")

// ErrIdempotencyKeyReused reports a request that carries the idempotency key of an earlier, different request.
var ErrIdempotencyKeyReused = errors.New("idempotency key already used for a different request")

// IdempotencyKey identifies a client's request, so that repeating the request creates nothing new. The zero value
// means the request has no key.
type IdempotencyKey struct {
	Key string
	// Fingerprint is the request's Draft.Fingerprint; it tells a repeat from a different request reusing the key.
	Fingerprint [32]byte
}
