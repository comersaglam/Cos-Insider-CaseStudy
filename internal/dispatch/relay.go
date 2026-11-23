package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
)

// outbox is what the relay needs from storage; *notification.Store satisfies it.
type outbox interface {
	DrainOutbox(ctx context.Context, limit int, publish func(context.Context, []notification.OutboxEntry) error) (int, error)
}

// waker tells the relay when to look again; *notification.OutboxListener satisfies it.
type waker interface {
	Wait(ctx context.Context, timeout time.Duration) error
}

// publisher sends messages to RabbitMQ; *rabbitmq.Publisher satisfies it.
type publisher interface {
	Publish(ctx context.Context, msgs ...rabbitmq.Message) error
}

// RelayConfig sets the relay's exchange and pace.
type RelayConfig struct {
	// Exchange receives the notifications, routed by their channel (ADR 0012).
	Exchange string
	// BatchSize is the most outbox rows one transaction takes.
	BatchSize int
	// PollInterval is the longest the relay waits without being woken, the fallback against a lost notification.
	PollInterval time.Duration
}

// Relay hands committed outbox rows to RabbitMQ (ADR 0003): it publishes them with confirms and deletes them in the
// transaction that locked them. It wakes when the outbox's trigger notifies, and at least every PollInterval.
type Relay struct {
	outbox outbox
	wake   waker
	pub    publisher
	cfg    RelayConfig
	log    *slog.Logger
}

// NewRelay returns a relay over the given outbox, wake-up source and publisher.
func NewRelay(o outbox, w waker, p publisher, cfg RelayConfig, log *slog.Logger) *Relay {
	return &Relay{outbox: o, wake: w, pub: p, cfg: cfg, log: log}
}

// Run relays until ctx ends, then returns nil. It returns an error when publishing or the database fails: the
// rows stay in the outbox, and the process stops to be restarted (ADR 0009).
func (r *Relay) Run(ctx context.Context) error {
	for {
		if err := r.drain(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := r.wake.Wait(ctx, r.cfg.PollInterval); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// drain publishes batches until the outbox has fewer rows than a batch.
func (r *Relay) drain(ctx context.Context) error {
	for {
		n, err := r.outbox.DrainOutbox(ctx, r.cfg.BatchSize, r.publish)
		if err != nil {
			return fmt.Errorf("relay the outbox: %w", err)
		}
		if n > 0 {
			r.log.DebugContext(ctx, "outbox relayed", "published", n)
		}
		if n < r.cfg.BatchSize {
			return nil
		}
	}
}

// publish turns outbox entries into messages: routed by channel, with the priority's rank as AMQP priority, and
// the notification's ID as message ID and body.
func (r *Relay) publish(ctx context.Context, entries []notification.OutboxEntry) error {
	msgs := make([]rabbitmq.Message, len(entries))
	for i, e := range entries {
		body, err := notification.QueueMessage{NotificationID: e.NotificationID}.Marshal()
		if err != nil {
			return fmt.Errorf("encode message for %s: %w", e.NotificationID, err)
		}
		msgs[i] = rabbitmq.Message{
			Exchange: r.cfg.Exchange, RoutingKey: string(e.Channel), ID: e.NotificationID.String(), Body: body,
			Priority: e.Priority.Rank(), CorrelationID: e.CorrelationID,
		}
	}
	return r.pub.Publish(ctx, msgs...)
}
