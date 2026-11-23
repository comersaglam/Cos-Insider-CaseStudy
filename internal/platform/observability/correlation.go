package observability

import (
	"context"
	"uuid"
)

// CorrelationHeader carries a correlation ID over HTTP and AMQP.
const CorrelationHeader = "X-Correlation-ID"

// maxCorrelationID bounds an ID taken from a client, which ends up in every log line and database row of a request.
const maxCorrelationID = 128

// correlationKey is the context key of the correlation ID. An unexported type makes the key impossible to collide
// with, or to read from outside this package.
type correlationKey struct{}

// WithCorrelationID returns a copy of ctx carrying id.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID returns the correlation ID ctx carries, or "" when it carries none.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

// NewCorrelationID returns a new random correlation ID. A version 7 UUID begins with its creation time, so IDs
// sort roughly by age.
func NewCorrelationID() string {
	return uuid.NewV7().String()
}

// ValidCorrelationID reports whether id may be taken from outside: 1 to 128 visible ASCII characters. Anything
// else, a line break above all, could forge or garble log lines, and is replaced with a new ID.
func ValidCorrelationID(id string) bool {
	if id == "" || len(id) > maxCorrelationID {
		return false
	}
	for i := range len(id) {
		if id[i] < '!' || id[i] > '~' {
			return false
		}
	}
	return true
}
