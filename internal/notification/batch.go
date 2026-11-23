package notification

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"
)

// MaxBatchSize is the most notifications one batch may hold, as the brief sets.
const MaxBatchSize = 1000

// NewBatch is a batch ready to store: the notifications it accepted, each with its position in the request, and
// the items it rejected. Rejected items are recorded with their reasons only; their content is personal data and
// is not kept.
type NewBatch struct {
	ID            uuid.UUID
	Total         int
	Items         []NewBatchItem
	Rejections    []Rejection
	CorrelationID string
}

// NewBatchItem is an accepted notification and its position in the request, counted from zero.
type NewBatchItem struct {
	Index        int
	Notification Notification
}

// Batch is a stored batch.
type Batch struct {
	ID       uuid.UUID
	Total    int
	Accepted int
	// Items are the accepted notifications in request order; they are filled when a batch is created, not read.
	Items      []BatchItem
	Rejections []Rejection
	// Statuses counts the accepted notifications by their status now; it is filled when a batch is read.
	Statuses      map[Status]int
	CorrelationID string
	CreatedAt     time.Time
}

// BatchItem is the ID an accepted item was given, and its position in the request.
type BatchItem struct {
	Index int
	ID    uuid.UUID
}

// Rejection is an item of a batch that broke a rule, by its position in the request.
type Rejection struct {
	Index  int
	Fields []FieldError
}

// BatchError refuses a batch in which no item is valid; nothing of it is stored.
type BatchError struct {
	Rejections []Rejection
}

// Error lists each rejected item with its problems.
func (e *BatchError) Error() string {
	parts := make([]string, 0, len(e.Rejections))
	for _, r := range e.Rejections {
		parts = append(parts, fmt.Sprintf("item %d: %s", r.Index, (&ValidationError{Fields: r.Fields}).Error()))
	}
	return "no notification of the batch is valid: " + strings.Join(parts, "; ")
}

// BatchFingerprint returns the SHA-256 of every draft's fingerprint, in order and rejected drafts included, so two
// requests with one idempotency key are the same batch exactly when every item is the same and in the same place.
func BatchFingerprint(drafts []Draft) [32]byte {
	h := sha256.New()
	h.Write([]byte(strconv.Itoa(len(drafts)) + ":"))
	for _, d := range drafts {
		f := d.Fingerprint()
		h.Write(f[:])
	}
	return [32]byte(h.Sum(nil))
}
