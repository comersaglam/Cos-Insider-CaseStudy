package rabbitmq

import (
	"slices"

	amqp "github.com/rabbitmq/amqp091-go"
)

// HeaderCarrier lets a propagator read and write a trace in a message's AMQP headers, as it does in HTTP headers
// (ADR 0034).
type HeaderCarrier amqp.Table

// Get returns the header key as a string, or "" when it is missing or not a string.
func (c HeaderCarrier) Get(key string) string {
	v, _ := c[key].(string)
	return v
}

// Set writes the header key.
func (c HeaderCarrier) Set(key, value string) {
	c[key] = value
}

// Keys returns the headers' names, sorted.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
