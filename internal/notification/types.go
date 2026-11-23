package notification

// Channel is the medium a notification is delivered through. Its values are the ones the provider contract uses.
type Channel string

// The channels a notification can be sent over.
const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelPush  Channel = "push"
)

// Valid reports whether c is one of the known channels. Any string converts to a Channel, so a value that comes
// from outside is checked with Valid before it is used.
func (c Channel) Valid() bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelPush:
		return true
	}
	return false
}

// Priority orders waiting work within a channel: high before normal before low. It is not sent to the provider.
type Priority string

// The priorities a notification can have.
const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// MaxPriorityRank is the rank of the highest priority, and so the number of priority levels a queue needs.
const MaxPriorityRank = 3

// Rank orders priorities as numbers, higher first: the AMQP priority a message is published with.
func (p Priority) Rank() uint8 {
	switch p {
	case PriorityHigh:
		return MaxPriorityRank
	case PriorityNormal:
		return 2
	}
	return 1
}

// Valid reports whether p is one of the known priorities.
func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	}
	return false
}
