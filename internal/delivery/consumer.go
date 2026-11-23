package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
	"uuid"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sony/gobreaker/v2"
	"golang.org/x/time/rate"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
)

// breakerBuckets is how many buckets the breaker's rolling window has.
const breakerBuckets = 10

// tracer names the delivery module's spans.
var tracer = otel.Tracer("github.com/comersaglam/Cos-Insider-CaseStudy/internal/delivery")

// ErrDeliveriesClosed reports that RabbitMQ stopped delivering: the channel or the connection was lost. The
// consumer stops and the process is restarted (ADR 0009).
var ErrDeliveriesClosed = errors.New("RabbitMQ stopped delivering messages")

// store is what the consumer needs from storage; *notification.Store satisfies it.
type store interface {
	Claim(ctx context.Context, id uuid.UUID, lease time.Duration, maxAttempts int) (notification.Claim, error)
	FinishAttempt(ctx context.Context, a notification.Attempt) error
	MarkUnprocessable(ctx context.Context, id uuid.UUID) error
	ReleaseClaim(ctx context.Context, id uuid.UUID, attempt int) error
}

// breaker guards the provider (ADR 0007, 0024); gobreaker's two-step breaker satisfies it.
type breaker interface {
	State() gobreaker.State
	Allow() (done func(error), err error)
}

// sender makes one attempt to send a notification; *Client satisfies it.
type sender interface {
	Send(ctx context.Context, n notification.Notification) Outcome
}

// ConsumerConfig sets one channel's consumer.
type ConsumerConfig struct {
	Channel notification.Channel
	// Concurrency is how many sends run at once; the channel's prefetch count should equal it (ADR 0009).
	Concurrency int
	// RatePerSecond is the most sends the channel starts in a second (ADR 0005).
	RatePerSecond float64
	// Lease is how long a claim holds a notification: longer than a send can take (ADR 0012).
	Lease  time.Duration
	Policy RetryPolicy
	// ShutdownTimeout bounds how long stopping waits for the sends in flight.
	ShutdownTimeout time.Duration
	// The circuit breaker opens for BreakerOpen once, over the last BreakerWindow, at least BreakerMinRequests sends
	// were made and BreakerFailurePercent of them failed for now (ADR 0007, 0036).
	BreakerMinRequests    int
	BreakerFailurePercent int
	BreakerWindow         time.Duration
	BreakerOpen           time.Duration
}

// breakerRecheck is how often the loop looks at a breaker that is not closed, to resume once it lets a probe through.
const breakerRecheck = 100 * time.Millisecond

// errProviderFailing and errNotCounted are what a send reports to the breaker: a transient failure counts towards
// opening it; a rejection or a rate limit says nothing of the provider's health and is not counted.
var (
	errProviderFailing = errors.New("the provider failed")
	errNotCounted      = errors.New("an answer that says nothing of the provider's health")
)

// Consumer sends the notifications of one channel's queue, as its single active consumer (ADR 0005, 0009, 0012).
// For each message it waits for the rate limiter and a free slot, then a goroutine of its own claims the
// notification, sends it, and records the outcome. Only the consumer's loop acknowledges, since an AMQP channel
// must not be shared.
type Consumer struct {
	store   store
	send    sender
	limiter *rate.Limiter
	breaker breaker
	cfg     ConsumerConfig
	metrics *Metrics
	channel string // the metrics' channel label
	log     *slog.Logger
}

// NewConsumer returns a consumer over the given storage and sender. Its limiter has a burst of one, so sends are
// spaced evenly and no second holds more than RatePerSecond of them, plus the one at its edge.
//
// Its circuit breaker opens on a failure ratio over a rolling window, not on failures in a row, which retries queued
// together would produce while the provider is healthy (ADR 0036). It stays open for BreakerOpen, then lets one probe
// through: a success closes it, a failure opens it again.
func NewConsumer(s store, snd sender, cfg ConsumerConfig, metrics *Metrics, log *slog.Logger) *Consumer {
	log = log.With("channel", cfg.Channel)
	channel := string(cfg.Channel)
	metrics.breakerState.WithLabelValues(channel).Set(0)
	metrics.start(channel)
	return &Consumer{
		store: s, send: snd, cfg: cfg, metrics: metrics, channel: channel, log: log,
		limiter: rate.NewLimiter(rate.Limit(cfg.RatePerSecond), 1),
		breaker: gobreaker.NewTwoStepCircuitBreaker[struct{}](gobreaker.Settings{
			Name:        string(cfg.Channel),
			MaxRequests: 1,
			Timeout:     cfg.BreakerOpen,
			// A rolling window of ten buckets, so old attempts leave it gradually.
			Interval:     cfg.BreakerWindow,
			BucketPeriod: cfg.BreakerWindow / breakerBuckets,
			ReadyToTrip: func(c gobreaker.Counts) bool {
				return int(c.Requests) >= cfg.BreakerMinRequests &&
					int(c.TotalFailures)*100 >= cfg.BreakerFailurePercent*int(c.Requests)
			},
			IsExcluded: func(err error) bool { return errors.Is(err, errNotCounted) },
			OnStateChange: func(_ string, from, to gobreaker.State) {
				metrics.breakerState.WithLabelValues(channel).Set(breakerValue(to))
				log.Warn("circuit breaker changed state", "from", from.String(), "to", to.String())
			},
		}),
	}
}

// action is how a delivery is settled.
type action int

const (
	ack     action = iota // done with: sent, failed, waiting for a retry in PostgreSQL, or not there to be sent
	reject                // dead-lettered: the message cannot be processed (ADR 0014)
	requeue               // put back unsent: the circuit breaker refused the send (ADR 0024)
)

// result is what a send hands back to the loop: the delivery, how to settle it, or the failure that stops the
// consumer, which leaves the delivery for RabbitMQ to deliver again.
type result struct {
	d      amqp.Delivery
	action action
	err    error
}

// Run consumes deliveries until ctx ends, then lets the sends in flight finish within ShutdownTimeout, settles
// them and returns nil. Deliveries not yet taken stay unacknowledged; RabbitMQ delivers them again. Run returns an
// error when the database fails or the deliveries stop.
//
// inFlight counts the sends started whose result the loop has not yet settled, and the loop takes no delivery
// while it is at Concurrency. Counting until the result is settled, not until the send ends, is what keeps the
// loop from deadlocking: results never outnumber the buffer, so a send never waits to hand one over, and the loop
// never waits for a slot, only for a result (ADR 0009).
func (c *Consumer) Run(ctx context.Context, deliveries <-chan amqp.Delivery) error {
	results := make(chan result, c.cfg.Concurrency)
	inFlight := 0
	// Sends in flight run to their end when Run is stopped; the provider timeout bounds each.
	work := context.WithoutCancel(ctx)

	for {
		// The breaker sets how many sends may be in flight: all while closed, the one probe while half-open, none
		// while open, when the queue keeps the messages and no attempt is spent (ADR 0007). A nil channel is never
		// ready, so at the limit the select waits only for results, and for the breaker to be looked at again.
		limit, recheck := c.cfg.Concurrency, (<-chan time.Time)(nil)
		switch c.breaker.State() {
		case gobreaker.StateOpen:
			limit, recheck = 0, time.After(breakerRecheck)
		case gobreaker.StateHalfOpen:
			limit, recheck = 1, time.After(breakerRecheck)
		}
		var incoming <-chan amqp.Delivery
		if inFlight < limit {
			incoming = deliveries
		}

		select {
		case <-recheck:
		case r := <-results:
			inFlight--
			c.metrics.inFlight.WithLabelValues(c.channel).Dec()
			if err := c.settle(r); err != nil {
				return err
			}
		case d, ok := <-incoming:
			if !ok {
				return c.drain(results, inFlight, ErrDeliveriesClosed)
			}
			// A retry held since the breaker opened goes back once, so RabbitMQ hands out a fresher notification
			// as the probe if one waits (ADR 0036); it comes back redelivered, and probes if nothing fresher does.
			if c.breaker.State() == gobreaker.StateHalfOpen && d.Priority == 0 && !d.Redelivered {
				if err := d.Nack(false, true); err != nil {
					return fmt.Errorf("requeue a held retry: %w", err)
				}
				continue
			}
			waitStart := time.Now()
			if err := c.limiter.Wait(ctx); err != nil {
				// Stopped while waiting for the rate: d is left for RabbitMQ to deliver again.
				return c.drain(results, inFlight, nil)
			}
			c.metrics.limiterWait.WithLabelValues(c.channel).Observe(time.Since(waitStart).Seconds())
			inFlight++
			c.metrics.inFlight.WithLabelValues(c.channel).Inc()
			go func() { results <- c.handle(work, d) }()
		case <-ctx.Done():
			return c.drain(results, inFlight, nil)
		}
	}
}

// drain settles the results of the sends in flight, waiting at most ShutdownTimeout, and returns cause.
func (c *Consumer) drain(results <-chan result, inFlight int, cause error) error {
	deadline := time.After(c.cfg.ShutdownTimeout)
	for ; inFlight > 0; inFlight-- {
		select {
		case r := <-results:
			c.metrics.inFlight.WithLabelValues(c.channel).Dec()
			if err := c.settle(r); err != nil {
				return err
			}
		case <-deadline:
			c.log.Warn("stopped with sends still in flight", "in_flight", inFlight)
			return cause
		}
	}
	return cause
}

func (c *Consumer) settle(r result) error {
	if r.err != nil {
		return r.err
	}
	var err error
	switch r.action {
	case ack:
		err = r.d.Ack(false)
	case reject:
		err = r.d.Reject(false)
	case requeue:
		err = r.d.Nack(false, true)
	}
	if err != nil {
		return fmt.Errorf("settle delivery %s: %w", r.d.MessageId, err)
	}
	return nil
}

// handle claims, sends and records one message's notification.
func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) result {
	// The relay puts the notification's correlation ID in a header, so even the lines written before the
	// notification is read are linked to the request that created it; anyone who can publish can set a header, so
	// it is checked as the API checks a client's.
	if id, _ := d.Headers[observability.CorrelationHeader].(string); observability.ValidCorrelationID(id) {
		ctx = observability.WithCorrelationID(ctx, id)
	}
	// The message is processed in a span that continues the trace the relay put in its headers (ADR 0034).
	ctx = observability.Propagator().Extract(ctx, rabbitmq.HeaderCarrier(d.Headers))
	ctx, span := tracer.Start(ctx, "process "+c.channel, trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.MessagingSystemRabbitMQ, semconv.MessagingOperationTypeProcess,
			semconv.MessagingMessageID(d.MessageId)))
	defer span.End()
	msg, err := notification.ParseQueueMessage(d.Body)
	if err != nil {
		return c.unprocessable(ctx, d, err)
	}

	claim, err := c.store.Claim(ctx, msg.NotificationID, c.cfg.Lease, c.cfg.Policy.MaxAttempts)
	switch {
	case errors.Is(err, notification.ErrNotClaimable):
		// Cancelled, finished, waiting, or in flight under another copy of the message (ADR 0012).
		c.log.DebugContext(ctx, "nothing to send", "notification_id", msg.NotificationID, "reason", err)
		return result{d: d, action: ack}
	case errors.Is(err, notification.ErrNotFound):
		c.log.WarnContext(ctx, "message for a notification that does not exist, dead-lettered", "notification_id", msg.NotificationID)
		c.metrics.deadLettered.WithLabelValues(c.channel).Inc()
		return result{d: d, action: reject}
	case err != nil:
		return result{d: d, err: fmt.Errorf("claim notification %s: %w", msg.NotificationID, err)}
	}

	n := claim.Notification
	ctx = observability.WithCorrelationID(ctx, n.CorrelationID)
	if claim.TakenOver {
		c.metrics.takeovers.WithLabelValues(c.channel).Inc()
		c.log.WarnContext(ctx, "notification taken over from a lost worker; it may be sent twice",
			"notification_id", n.ID, "attempt", n.Attempts)
	}

	// The breaker is asked after the claim, so a message with nothing to send never counts as a probe. If it
	// opened meanwhile, the claim is undone, attempt included, and the message goes back to the queue.
	done, err := c.breaker.Allow()
	if err != nil {
		if err := c.store.ReleaseClaim(ctx, n.ID, n.Attempts); err != nil {
			return result{d: d, err: fmt.Errorf("release the claim on %s: %w", n.ID, err)}
		}
		return result{d: d, action: requeue}
	}
	out := c.sendSafely(ctx, n)
	done(breakerReport(out))
	c.metrics.attempts.WithLabelValues(c.channel, string(out.Class)).Inc()
	c.metrics.latency.WithLabelValues(c.channel).Observe(out.Latency.Seconds())
	attempt := c.cfg.Policy.After(n.ID, n.Attempts, out, time.Now())
	switch err := c.store.FinishAttempt(ctx, attempt); {
	case errors.Is(err, notification.ErrClaimLost):
		c.log.WarnContext(ctx, "outcome recorded after the claim was taken over", "notification_id", n.ID, "outcome", attempt.Outcome)
	case err != nil:
		return result{d: d, err: fmt.Errorf("record the attempt on %s: %w", n.ID, err)}
	default:
		c.logAttempt(ctx, n, attempt, out)
	}
	return result{d: d, action: ack}
}

// sendSafely sends n, turning a panic into a transient outcome: a bug costs the notification an attempt, not the
// process (ADR 0012).
func (c *Consumer) sendSafely(ctx context.Context, n notification.Notification) (out Outcome) {
	start := time.Now()
	defer func() {
		if p := recover(); p != nil {
			c.log.ErrorContext(ctx, "panic while sending", "notification_id", n.ID, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			out = Outcome{Class: Transient, Latency: time.Since(start), Err: fmt.Errorf("panic while sending: %v", p)}
		}
	}()
	return c.send.Send(ctx, n)
}

// unprocessable dead-letters a message that cannot be read, failing its notification when the message ID names
// one (ADR 0014).
func (c *Consumer) unprocessable(ctx context.Context, d amqp.Delivery, cause error) result {
	if id, err := uuid.Parse(d.MessageId); err == nil {
		if err := c.store.MarkUnprocessable(ctx, id); err != nil {
			return result{d: d, err: fmt.Errorf("mark notification %s unprocessable: %w", id, err)}
		}
	}
	c.log.WarnContext(ctx, "unprocessable message dead-lettered", "message_id", d.MessageId, "error", cause)
	c.metrics.deadLettered.WithLabelValues(c.channel).Inc()
	return result{d: d, action: reject}
}

// logAttempt logs an attempt at the level its outcome deserves: an error for our own configuration, a warning for
// any other failure.
func (c *Consumer) logAttempt(ctx context.Context, n notification.Notification, a notification.Attempt, out Outcome) {
	level := slog.LevelInfo
	switch out.Class {
	case Transient, Permanent:
		level = slog.LevelWarn
	case PermanentAlert:
		level = slog.LevelError
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("notification.outcome", string(a.Outcome)), attribute.Int("notification.attempt", a.Number))
	if out.Class != Success {
		span.SetStatus(codes.Error, string(a.Outcome))
	}
	c.log.Log(ctx, level, "attempt finished",
		"notification_id", n.ID, "attempt", a.Number, "outcome", a.Outcome, "status_code", a.StatusCode,
		"next", a.Next, "latency_ms", out.Latency.Milliseconds(), "to", observability.MaskRecipient(n.To), "error", a.Error)
}

// breakerReport is what an outcome tells the breaker: a transient failure counts towards opening it, a success
// towards closing it, and a rejection or a rate limit, which are no sign of an outage, nothing (ADR 0007).
func breakerReport(out Outcome) error {
	switch {
	case out.Class == Success:
		return nil
	case out.Class == Transient && out.StatusCode != http.StatusTooManyRequests:
		return errProviderFailing
	}
	return errNotCounted
}
