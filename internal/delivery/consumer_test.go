package delivery

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"go.opentelemetry.io/otel/trace"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
)

// acks records how each delivery was settled, standing in for the AMQP channel.
type acks struct {
	mu       sync.Mutex
	acked    []uint64
	rejected []uint64
	requeued []uint64
}

func (a *acks) Ack(tag uint64, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acked = append(a.acked, tag)
	return nil
}

func (a *acks) Nack(tag uint64, _, requeue bool) error {
	if !requeue {
		return errors.New("the consumer rejects rather than nacks without requeueing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requeued = append(a.requeued, tag)
	return nil
}

func (a *acks) Reject(tag uint64, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejected = append(a.rejected, tag)
	return nil
}

// counts returns how many deliveries were acknowledged, and how many rejected or requeued.
func (a *acks) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.acked), len(a.rejected) + len(a.requeued)
}

// fakeStore answers claims and records outcomes.
type fakeStore struct {
	mu           sync.Mutex
	claimErr     error
	finishErr    error
	takenOver    bool
	finished     []notification.Attempt
	unprocessed  []uuid.UUID
	released     []uuid.UUID
	claimedTimes int
}

func (f *fakeStore) ReleaseClaim(_ context.Context, id uuid.UUID, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	return nil
}

func (f *fakeStore) Claim(_ context.Context, id uuid.UUID, _ time.Duration, _ int) (notification.Claim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimedTimes++
	if f.claimErr != nil {
		return notification.Claim{}, f.claimErr
	}
	n := testNotification
	n.ID, n.Attempts, n.Status = id, 1, notification.StatusSending
	return notification.Claim{Notification: n, TakenOver: f.takenOver}, nil
}

func (f *fakeStore) FinishAttempt(_ context.Context, a notification.Attempt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, a)
	return f.finishErr
}

func (f *fakeStore) MarkUnprocessable(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unprocessed = append(f.unprocessed, id)
	return nil
}

// fakeSender answers every send with out, after running during (when set).
type fakeSender struct {
	out    Outcome
	during func()
}

func (s fakeSender) Send(context.Context, notification.Notification) Outcome {
	if s.during != nil {
		s.during()
	}
	return s.out
}

func testConfig() ConsumerConfig {
	return ConsumerConfig{
		Channel: notification.ChannelSMS, Concurrency: 4, RatePerSecond: 1000, Lease: time.Minute,
		Policy: testPolicy, ShutdownTimeout: 5 * time.Second, BreakerMinRequests: 20, BreakerFailurePercent: 50,
		BreakerWindow: 10 * time.Second, BreakerOpen: time.Minute,
	}
}

// deliveriesOf returns a channel holding the deliveries, tagged from 1 and settled through ack. It stays open, as
// RabbitMQ's does while the connection lives.
func deliveriesOf(ack amqp.Acknowledger, bodies ...amqp.Delivery) <-chan amqp.Delivery {
	ch := make(chan amqp.Delivery, len(bodies))
	for i, d := range bodies {
		d.Acknowledger, d.DeliveryTag = ack, uint64(i+1)
		ch <- d
	}
	return ch
}

// message is a well-formed delivery for a new notification ID.
func message(t *testing.T) amqp.Delivery {
	t.Helper()
	id := uuid.NewV7()
	body, err := notification.QueueMessage{NotificationID: id}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return amqp.Delivery{Body: body, MessageId: id.String()}
}

// runUntilSettled runs the consumer until want deliveries are settled, then stops it and returns Run's result.
func runUntilSettled(t *testing.T, c *Consumer, deliveries <-chan amqp.Delivery, a *acks, want int) error {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, deliveries) }()
	deadline := time.After(10 * time.Second)
	for {
		acked, rejected := a.counts()
		if acked+rejected >= want {
			break
		}
		select {
		case err := <-done:
			stop()
			return err
		case <-deadline:
			stop()
			t.Fatalf("settled %d of %d deliveries in 10 s", acked+rejected, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
	stop()
	return <-done
}

var discard = slog.New(slog.DiscardHandler)

func TestConsumerSendsRecordsAndAcknowledges(t *testing.T) {
	var a acks
	store := &fakeStore{}
	c := NewConsumer(store, fakeSender{out: Outcome{Class: Success, StatusCode: 202, MessageID: "m-1"}}, testConfig(), testMetrics(), discard)

	if err := runUntilSettled(t, c, deliveriesOf(&a, message(t), message(t)), &a, 2); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if acked, rejected := a.counts(); acked != 2 || rejected != 0 {
		t.Errorf("acked %d, rejected %d; want 2 and 0", acked, rejected)
	}
	if len(store.finished) != 2 || store.finished[0].Next != notification.StatusSent || store.finished[0].ProviderMessageID != "m-1" {
		t.Errorf("finished = %+v, want two sent", store.finished)
	}
}

func TestConsumerRetriesATransientFailure(t *testing.T) {
	var a acks
	store := &fakeStore{}
	c := NewConsumer(store, fakeSender{out: Outcome{Class: Transient, StatusCode: 503, Err: errors.New("busy")}}, testConfig(), testMetrics(), discard)
	if err := runUntilSettled(t, c, deliveriesOf(&a, message(t)), &a, 1); err != nil {
		t.Fatal(err)
	}
	// The retry waits in PostgreSQL (ADR 0006): the message itself is acknowledged.
	if acked, _ := a.counts(); acked != 1 || store.finished[0].Next != notification.StatusRetrying {
		t.Errorf("acked %d, finished %+v; want acknowledged and retrying", acked, store.finished)
	}
}

func TestConsumerSettlesMessagesItCannotSend(t *testing.T) {
	readableID := uuid.NewV7()
	tests := []struct {
		name         string
		delivery     amqp.Delivery
		claimErr     error
		wantAcked    int
		wantRejected int
		wantMarked   bool
	}{
		{"unreadable body, readable ID", amqp.Delivery{Body: []byte("not json"), MessageId: readableID.String()}, nil, 0, 1, true},
		{"unreadable body and ID", amqp.Delivery{Body: []byte("not json"), MessageId: "?"}, nil, 0, 1, false},
		{"not there to be sent", message(t), notification.ErrNotClaimable, 1, 0, false},
		{"a notification that does not exist", message(t), notification.ErrNotFound, 0, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a acks
			store := &fakeStore{claimErr: tt.claimErr}
			sends := 0
			c := NewConsumer(store, fakeSender{out: Outcome{Class: Success}, during: func() { sends++ }}, testConfig(), testMetrics(), discard)
			if err := runUntilSettled(t, c, deliveriesOf(&a, tt.delivery), &a, 1); err != nil {
				t.Fatal(err)
			}
			acked, rejected := a.counts()
			if acked != tt.wantAcked || rejected != tt.wantRejected || sends != 0 {
				t.Errorf("acked %d, rejected %d, sent %d; want %d, %d, 0", acked, rejected, sends, tt.wantAcked, tt.wantRejected)
			}
			if tt.wantMarked != (len(store.unprocessed) == 1 && store.unprocessed[0] == readableID) {
				t.Errorf("marked unprocessable %v, want %v", store.unprocessed, tt.wantMarked)
			}
		})
	}
}

func TestConsumerAcknowledgesAnOutcomeWhoseClaimWasLost(t *testing.T) {
	var a acks
	c := NewConsumer(&fakeStore{finishErr: notification.ErrClaimLost}, fakeSender{out: Outcome{Class: Success}}, testConfig(), testMetrics(), discard)
	if err := runUntilSettled(t, c, deliveriesOf(&a, message(t)), &a, 1); err != nil {
		t.Fatal(err)
	}
	if acked, _ := a.counts(); acked != 1 {
		t.Errorf("acked %d, want 1: the newer claim owns the notification", acked)
	}
}

// TestConsumerRecordsAPanickingSendAsAFailedAttempt: a bug in sending costs the notification an attempt, not the
// process (ADR 0012).
func TestConsumerRecordsAPanickingSendAsAFailedAttempt(t *testing.T) {
	var a acks
	store := &fakeStore{}
	c := NewConsumer(store, fakeSender{during: func() { panic("nil map write") }}, testConfig(), testMetrics(), discard)
	if err := runUntilSettled(t, c, deliveriesOf(&a, message(t)), &a, 1); err != nil {
		t.Fatal(err)
	}
	if len(store.finished) != 1 || store.finished[0].Outcome != notification.OutcomeTransient || store.finished[0].Next != notification.StatusRetrying {
		t.Errorf("finished = %+v, want a transient attempt, retrying", store.finished)
	}
}

// TestConsumerStopsWhenTheDatabaseFails: an infrastructure failure stops the consumer (ADR 0009); the delivery is
// left unsettled, so RabbitMQ delivers it again.
func TestConsumerStopsWhenTheDatabaseFails(t *testing.T) {
	var a acks
	c := NewConsumer(&fakeStore{claimErr: errors.New("connection lost")}, fakeSender{}, testConfig(), testMetrics(), discard)
	if err := c.Run(t.Context(), deliveriesOf(&a, message(t))); err == nil {
		t.Fatal("Run = nil, want the database error")
	}
	if acked, rejected := a.counts(); acked+rejected != 0 {
		t.Errorf("settled %d deliveries, want none", acked+rejected)
	}
}

func TestConsumerSendsAtMostConcurrencyAtOnce(t *testing.T) {
	var (
		a        acks
		now, top atomic.Int32
	)
	cfg := testConfig()
	cfg.Concurrency = 3
	sender := fakeSender{out: Outcome{Class: Success}, during: func() {
		n := now.Add(1)
		for {
			if m := top.Load(); n <= m || top.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		now.Add(-1)
	}}
	var msgs []amqp.Delivery
	for range 30 {
		msgs = append(msgs, message(t))
	}
	if err := runUntilSettled(t, NewConsumer(&fakeStore{}, sender, cfg, testMetrics(), discard), deliveriesOf(&a, msgs...), &a, 30); err != nil {
		t.Fatal(err)
	}
	if top.Load() != 3 {
		t.Errorf("at most %d sends ran at once, want exactly the concurrency of 3", top.Load())
	}
}

// TestConsumerDoesNotDeadlockAtFullConcurrency runs twenty messages through one slot. A semaphore released by the
// send, as ADR 0009 first had it, deadlocked here: the loop filled the freed slot instead of reading results, the
// next send could not hand over its result to the full buffer, and both waited for ever.
func TestConsumerDoesNotDeadlockAtFullConcurrency(t *testing.T) {
	var a acks
	cfg := testConfig()
	cfg.Concurrency = 1
	var msgs []amqp.Delivery
	for range 20 {
		msgs = append(msgs, message(t))
	}
	if err := runUntilSettled(t, NewConsumer(&fakeStore{}, fakeSender{out: Outcome{Class: Success}}, cfg, testMetrics(), discard), deliveriesOf(&a, msgs...), &a, 20); err != nil {
		t.Fatal(err)
	}
}

// TestConsumerKeepsToTheRate sends 60 messages at 50 per second: no second ever holds more than 51 sends (a
// token bucket of burst 1 can start one at each end of the window).
func TestConsumerKeepsToTheRate(t *testing.T) {
	var (
		a     acks
		mu    sync.Mutex
		times []time.Time
	)
	cfg := testConfig()
	cfg.RatePerSecond, cfg.Concurrency = 50, 10
	sender := fakeSender{out: Outcome{Class: Success}, during: func() {
		mu.Lock()
		defer mu.Unlock()
		times = append(times, time.Now())
	}}
	var msgs []amqp.Delivery
	for range 60 {
		msgs = append(msgs, message(t))
	}
	start := time.Now()
	if err := runUntilSettled(t, NewConsumer(&fakeStore{}, sender, cfg, testMetrics(), discard), deliveriesOf(&a, msgs...), &a, 60); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < time.Second {
		t.Errorf("60 sends at 50 per second took %v, want over a second", took)
	}
	for i := range times {
		inWindow := 0
		for _, t2 := range times[i:] {
			if t2.Sub(times[i]) < time.Second {
				inWindow++
			}
		}
		if inWindow > 51 {
			t.Fatalf("%d sends within one second from send %d, want at most 51", inWindow, i)
		}
	}
}

// TestConsumerFinishesSendsInFlightWhenStopped: stopping lets a send already under way finish and be recorded and
// acknowledged; deliveries not yet taken are left for RabbitMQ to deliver again.
func TestConsumerFinishesSendsInFlightWhenStopped(t *testing.T) {
	var a acks
	store := &fakeStore{}
	started, release := make(chan struct{}), make(chan struct{})
	cfg := testConfig()
	cfg.Concurrency = 1
	sender := fakeSender{out: Outcome{Class: Success}, during: func() {
		close(started)
		<-release
	}}
	deliveries := make(chan amqp.Delivery, 2)
	first, second := message(t), message(t)
	first.Acknowledger, first.DeliveryTag = &a, 1
	second.Acknowledger, second.DeliveryTag = &a, 2
	deliveries <- first

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- NewConsumer(store, sender, cfg, testMetrics(), discard).Run(ctx, deliveries) }()
	<-started
	stop()
	deliveries <- second
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if acked, _ := a.counts(); acked != 1 || len(store.finished) != 1 || store.finished[0].Next != notification.StatusSent {
		t.Errorf("acked %d, finished %+v; want the send in flight recorded and acknowledged, and nothing else", acked, store.finished)
	}
}

// TestConsumerLogsUnderTheMessagesCorrelationID: lines written before the notification is read, such as a
// dead-lettered message's, carry the correlation ID of the message's header. A value that is not a valid ID, which
// could forge log lines, is ignored.
func TestConsumerLogsUnderTheMessagesCorrelationID(t *testing.T) {
	for _, tt := range []struct {
		name, header string
		want         bool
	}{
		{"a valid ID", "trace-me", true},
		{"a forged line break", "trace-me\n{\"level\":\"ERROR\"}", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			var a acks
			c := NewConsumer(&fakeStore{}, fakeSender{}, testConfig(), testMetrics(), observability.NewLogger(&logs, slog.LevelInfo))
			d := amqp.Delivery{Body: []byte("not json"), MessageId: "?", Headers: amqp.Table{rabbitmq.CorrelationHeader: tt.header}}
			if err := runUntilSettled(t, c, deliveriesOf(&a, d), &a, 1); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), "unprocessable message dead-lettered") {
				t.Fatalf("no dead-letter line:\n%s", logs.String())
			}
			if tt.want != strings.Contains(logs.String(), `"correlation_id":"trace-me"`) || !tt.want && strings.Contains(logs.String(), "correlation_id") {
				t.Errorf("correlation in the log = %v, want %v:\n%s", !tt.want, tt.want, logs.String())
			}
		})
	}
}

// traceSender records the trace each send ran in.
type traceSender struct {
	traces chan trace.TraceID
}

func (s traceSender) Send(ctx context.Context, _ notification.Notification) Outcome {
	s.traces <- trace.SpanContextFromContext(ctx).TraceID()
	return Outcome{Class: Success, StatusCode: 202}
}

// TestConsumerSendsInTheMessagesTrace: the trace the relay put in the message's headers is the trace of the send.
func TestConsumerSendsInTheMessagesTrace(t *testing.T) {
	var a acks
	sender := traceSender{traces: make(chan trace.TraceID, 1)}
	c := NewConsumer(&fakeStore{}, sender, testConfig(), testMetrics(), discard)
	d := message(t)
	d.Headers = amqp.Table{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
	if err := runUntilSettled(t, c, deliveriesOf(&a, d), &a, 1); err != nil {
		t.Fatal(err)
	}
	if got := <-sender.traces; got.String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("sent in trace %s, want the message's", got)
	}
}
