package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// CorrelationHeader carries a message's correlation ID, under the name HTTP uses for it.
const CorrelationHeader = observability.CorrelationHeader

// Connection settings. The heartbeat lets both sides notice a dead peer within two intervals.
const (
	heartbeat   = 10 * time.Second
	dialTimeout = 5 * time.Second
)

// maxInFlight is the most messages a publisher sends before it waits for their confirms. Messages RabbitMQ could
// not route come back on a channel this size, so it can never fill while a batch is in flight.
const maxInFlight = 256

// ErrUnroutable reports a message no queue took. RabbitMQ confirms such a message all the same; only the
// mandatory flag brings it back, and Publish turns that into this error.
var ErrUnroutable = errors.New("no queue took the message")

// ErrNotConfirmed reports a message RabbitMQ refused (a negative confirm), for example to a queue that is full.
var ErrNotConfirmed = errors.New("RabbitMQ did not confirm the message")

// Connect opens a connection to url under name, the name RabbitMQ's management UI shows for it. A lost connection
// is not re-established: the process stops and is restarted (ADR 0009).
func Connect(url, name string) (*amqp.Connection, error) {
	conn, err := amqp.DialConfig(url, amqp.Config{
		Heartbeat:  heartbeat,
		Properties: amqp.Table{"connection_name": name},
		Dial:       amqp.DefaultDial(dialTimeout),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	return conn, nil
}

// Topology is the exchanges and queues work flows through (ADR 0012): a direct exchange that routes each message to
// the queue bound with its routing key, and a dead-letter exchange and queue, sharing one name, for messages a
// consumer rejects.
type Topology struct {
	Exchange    string
	DeadLetter  string
	MaxPriority uint8
	Queues      []Queue
}

// Queue is a work queue and the routing key that leads to it.
type Queue struct {
	Name       string
	RoutingKey string
}

// Declare creates the topology, or confirms it exists with the same settings; declaring it again changes nothing,
// so every process declares it as it starts. Work queues are durable classic queues with priorities, a single
// active consumer (ADR 0005) and the dead-letter exchange.
func Declare(ch *amqp.Channel, t Topology) error {
	if err := ch.ExchangeDeclare(t.Exchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", t.Exchange, err)
	}
	if err := ch.ExchangeDeclare(t.DeadLetter, amqp.ExchangeFanout, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", t.DeadLetter, err)
	}
	if _, err := ch.QueueDeclare(t.DeadLetter, true, false, false, false, amqp.Table{"x-queue-type": "classic"}); err != nil {
		return fmt.Errorf("declare queue %s: %w", t.DeadLetter, err)
	}
	if err := ch.QueueBind(t.DeadLetter, "", t.DeadLetter, false, nil); err != nil {
		return fmt.Errorf("bind queue %s: %w", t.DeadLetter, err)
	}

	for _, q := range t.Queues {
		args := amqp.Table{
			"x-queue-type":             "classic",
			"x-max-priority":           int32(t.MaxPriority),
			"x-single-active-consumer": true,
			"x-dead-letter-exchange":   t.DeadLetter,
		}
		if _, err := ch.QueueDeclare(q.Name, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare queue %s: %w", q.Name, err)
		}
		if err := ch.QueueBind(q.Name, q.RoutingKey, t.Exchange, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", q.Name, err)
		}
	}
	return nil
}

// Message is one message to publish. ID becomes the AMQP message ID; Body is JSON.
type Message struct {
	Exchange      string
	RoutingKey    string
	ID            string
	Body          []byte
	Priority      uint8
	CorrelationID string
	// Headers are more AMQP headers, such as the trace's traceparent (ADR 0034).
	Headers map[string]string
}

// Publisher publishes persistent messages on a channel of its own, in confirm mode, and reports a message only
// once RabbitMQ has taken responsibility for it. It is safe for concurrent use; publishes take turns.
type Publisher struct {
	mu      sync.Mutex
	ch      *amqp.Channel
	returns chan amqp.Return
}

// NewPublisher opens a channel on conn and puts it in confirm mode.
func NewPublisher(conn *amqp.Connection) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open publishing channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	return &Publisher{ch: ch, returns: ch.NotifyReturn(make(chan amqp.Return, maxInFlight))}, nil
}

// Publish sends the messages and returns nil once RabbitMQ has confirmed every one of them and routed each to a
// queue. Messages go out in batches of maxInFlight, each batch confirmed before the next. On an error some
// messages may have been delivered: the caller publishes them all again, and consumers drop the duplicates
// (ADR 0012).
func (p *Publisher) Publish(ctx context.Context, msgs ...Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for batch := range slices.Chunk(msgs, maxInFlight) {
		if err := p.publishBatch(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func (p *Publisher) publishBatch(ctx context.Context, msgs []Message) error {
	p.drainReturns() // left over from a batch that failed before reading them

	confirms := make([]*amqp.DeferredConfirmation, len(msgs))
	for i, m := range msgs {
		pub := amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    m.ID,
			Priority:     m.Priority,
			Timestamp:    time.Now().UTC(),
			Body:         m.Body,
		}
		if m.CorrelationID != "" || len(m.Headers) > 0 {
			pub.Headers = amqp.Table{}
			for k, v := range m.Headers {
				pub.Headers[k] = v
			}
			if m.CorrelationID != "" {
				pub.Headers[CorrelationHeader] = m.CorrelationID
			}
		}
		dc, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, m.Exchange, m.RoutingKey, true, false, pub)
		if err != nil {
			return fmt.Errorf("publish message %s: %w", m.ID, err)
		}
		confirms[i] = dc
	}

	for i, dc := range confirms {
		acked, err := dc.WaitContext(ctx)
		if err != nil {
			return fmt.Errorf("wait for the confirm of message %s: %w", msgs[i].ID, err)
		}
		if !acked {
			return fmt.Errorf("%w: %s", ErrNotConfirmed, msgs[i].ID)
		}
	}

	// RabbitMQ sends a message back before it confirms it, so every return of this batch is already here.
	if returned := p.drainReturns(); len(returned) > 0 {
		return fmt.Errorf("%w: %s", ErrUnroutable, strings.Join(returned, ", "))
	}
	return nil
}

// drainReturns takes the IDs of every message that came back, without waiting.
func (p *Publisher) drainReturns() []string {
	var ids []string
	for {
		select {
		case r, ok := <-p.returns:
			if !ok {
				return ids
			}
			ids = append(ids, r.MessageId)
		default:
			return ids
		}
	}
}

// Close closes the publisher's channel.
func (p *Publisher) Close() error {
	return p.ch.Close()
}

// Consume starts delivering queue's messages on ch, to be acknowledged one by one, with at most prefetch of them
// unacknowledged at a time: the backpressure of ADR 0009, since RabbitMQ stops delivering until some are
// acknowledged.
func Consume(ch *amqp.Channel, queue string, prefetch int) (<-chan amqp.Delivery, error) {
	if err := ch.Qos(prefetch, 0, false); err != nil {
		return nil, fmt.Errorf("set prefetch on %s: %w", queue, err)
	}
	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", queue, err)
	}
	return deliveries, nil
}
