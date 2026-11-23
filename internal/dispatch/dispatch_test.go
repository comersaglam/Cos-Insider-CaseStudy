package dispatch

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
)

var discard = slog.New(slog.DiscardHandler)

func testMetrics() *Metrics { return NewMetrics(prometheus.NewRegistry()) }

// newStore returns a notification store over a fresh database of the test's own, and its pool.
func newStore(t *testing.T) (*notification.Store, *pgxpool.Pool) {
	t.Helper()
	pool, err := postgres.Connect(t.Context(), testenv.PostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return notification.NewStore(pool), pool
}

// newBroker connects to the test broker and declares a topology whose names belong to this test.
func newBroker(t *testing.T) (*amqp.Connection, rabbitmq.Topology) {
	t.Helper()
	conn, err := rabbitmq.Connect(testenv.RabbitMQURL(t), "dispatch-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	prefix := "t-" + strings.ToLower(rand.Text())[:10]
	top := rabbitmq.Topology{
		Exchange: prefix + ".notifications", DeadLetter: prefix + ".dlq", MaxPriority: notification.MaxPriorityRank,
		Queues: []rabbitmq.Queue{{Name: prefix + ".sms", RoutingKey: "sms"}, {Name: prefix + ".email", RoutingKey: "email"}},
	}
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	if err := rabbitmq.Declare(ch, top); err != nil {
		t.Fatal(err)
	}
	return conn, top
}

// testTraceParent is the trace the test notifications were created in.
const testTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// create stores a queued notification, as the API does.
func create(t *testing.T, store *notification.Store, channel notification.Channel, to string, p notification.Priority) notification.Notification {
	t.Helper()
	n, _, err := store.Create(t.Context(), notification.Notification{
		ID: uuid.NewV7(), To: to, Channel: channel, Content: "hello", Priority: p,
		Status: notification.StatusQueued, CorrelationID: "corr-" + to, TraceParent: testTraceParent,
	}, notification.IdempotencyKey{})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// startRelay runs a relay until the test ends; it must then stop with nil.
func startRelay(t *testing.T, store *notification.Store, conn *amqp.Connection, top rabbitmq.Topology, poll time.Duration) {
	t.Helper()
	listener, err := store.ListenOutbox(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	relay := NewRelay(store, listener, pub, RelayConfig{Exchange: top.Exchange, BatchSize: 10, PollInterval: poll}, testMetrics(), discard)

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("Run after stop = %v, want nil", err)
		}
		_ = listener.Close(context.Background())
		_ = pub.Close()
	})
}

// waitFor polls cond until it holds or ten seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func depth(t *testing.T, conn *amqp.Connection, queue string) int {
	t.Helper()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	q, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return q.Messages
}

func outboxRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM outbox").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRelayPublishesTheOutbox runs the relay with a poll interval of a minute: what it publishes after starting, it
// publishes because the outbox's NOTIFY woke it.
func TestRelayPublishesTheOutbox(t *testing.T) {
	t.Parallel()
	store, pool := newStore(t)
	conn, top := newBroker(t)
	before := create(t, store, notification.ChannelSMS, "+905551234567", notification.PriorityLow)

	startRelay(t, store, conn, top, time.Minute)
	waitFor(t, "the row stored before the relay started", func() bool { return depth(t, conn, top.Queues[0].Name) == 1 })

	after := create(t, store, notification.ChannelSMS, "+905551234568", notification.PriorityHigh)
	create(t, store, notification.ChannelEmail, "ayse@example.com", notification.PriorityNormal)
	waitFor(t, "the rows stored after it started", func() bool {
		return depth(t, conn, top.Queues[0].Name) == 2 && depth(t, conn, top.Queues[1].Name) == 1
	})
	if n := outboxRows(t, pool); n != 0 {
		t.Errorf("%d outbox rows left, want 0", n)
	}

	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	deliveries, err := rabbitmq.Consume(ch, top.Queues[0].Name, 10)
	if err != nil {
		t.Fatal(err)
	}
	// The higher priority is delivered first.
	for _, want := range []notification.Notification{after, before} {
		d := <-deliveries
		m, err := notification.ParseQueueMessage(d.Body)
		if err != nil || m.NotificationID != want.ID || d.MessageId != want.ID.String() ||
			d.Priority != want.Priority.Rank() || d.Headers[rabbitmq.CorrelationHeader] != want.CorrelationID ||
			!strings.HasPrefix(fmt.Sprint(d.Headers["traceparent"]), "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
			t.Errorf("delivery = %s id %s priority %d headers %v; want %s at %d", d.Body, d.MessageId, d.Priority, d.Headers, want.ID, want.Priority.Rank())
		}
	}
}

func TestTwoRelaysPublishEachRowOnce(t *testing.T) {
	t.Parallel()
	store, pool := newStore(t)
	conn, top := newBroker(t)
	for i := range 100 {
		create(t, store, notification.ChannelSMS, "+9055512345"+string(rune('0'+i%10))+string(rune('0'+i/10)), notification.PriorityNormal)
	}

	startRelay(t, store, conn, top, time.Second)
	startRelay(t, store, conn, top, time.Second)
	waitFor(t, "the outbox to empty", func() bool { return outboxRows(t, pool) == 0 })
	// Give a duplicate, had there been one, time to arrive.
	time.Sleep(200 * time.Millisecond)
	if d := depth(t, conn, top.Queues[0].Name); d != 100 {
		t.Errorf("the queue holds %d messages for 100 notifications", d)
	}
}

// failingPublisher stands for RabbitMQ being down.
type failingPublisher struct{}

func (failingPublisher) Publish(context.Context, ...rabbitmq.Message) error {
	return errors.New("connection refused")
}

// recordingPublisher stands for RabbitMQ being up.
type recordingPublisher struct {
	mu   sync.Mutex
	msgs []rabbitmq.Message
}

func (p *recordingPublisher) Publish(_ context.Context, msgs ...rabbitmq.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, msgs...)
	return nil
}

// noWait wakes the relay at once, every time.
type noWait struct{}

func (noWait) Wait(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// TestRelayStopsWhenRabbitMQIsDownAndTheRowsWait: the relay stops with the error (the process restarts, ADR 0009)
// and the rows stay; a relay started once RabbitMQ is back publishes them.
func TestRelayStopsWhenRabbitMQIsDownAndTheRowsWait(t *testing.T) {
	t.Parallel()
	store, pool := newStore(t)
	create(t, store, notification.ChannelSMS, "+905551234567", notification.PriorityNormal)
	cfg := RelayConfig{Exchange: "notifications", BatchSize: 10, PollInterval: time.Second}

	if err := NewRelay(store, noWait{}, failingPublisher{}, cfg, testMetrics(), discard).Run(t.Context()); err == nil {
		t.Fatal("Run = nil while publishing fails, want the error")
	}
	if n := outboxRows(t, pool); n != 1 {
		t.Fatalf("%d outbox rows after the failure, want the row kept", n)
	}

	var pub recordingPublisher
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- NewRelay(store, noWait{}, &pub, cfg, testMetrics(), discard).Run(ctx) }()
	waitFor(t, "the row to be published", func() bool { return outboxRows(t, pool) == 0 })
	stop()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if len(pub.msgs) != 1 || pub.msgs[0].Exchange != "notifications" || pub.msgs[0].RoutingKey != "sms" {
		t.Errorf("published %+v, want one message to notifications by sms", pub.msgs)
	}
}

func TestSchedulerQueuesWhatIsDue(t *testing.T) {
	t.Parallel()
	store, pool := newStore(t)
	due := create(t, store, notification.ChannelSMS, "+905551234567", notification.PriorityNormal)
	later := create(t, store, notification.ChannelSMS, "+905551234568", notification.PriorityNormal)
	for id, at := range map[uuid.UUID]time.Time{due.ID: time.Now().Add(-time.Second), later.ID: time.Now().Add(time.Hour)} {
		if _, err := pool.Exec(t.Context(), "UPDATE notifications SET status = 'retrying', next_attempt_at = $2 WHERE id = $1", id, at); err != nil {
			t.Fatal(err)
		}
	}

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- NewScheduler(store, SchedulerConfig{Interval: 50 * time.Millisecond, BatchSize: 10, MaxAttempts: 8}, testMetrics(), discard).Run(ctx)
	}()
	waitFor(t, "the due notification to be queued", func() bool {
		n, err := store.Get(t.Context(), due.ID)
		return err == nil && n.Status == notification.StatusQueued
	})
	stop()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if n, _ := store.Get(t.Context(), later.ID); n.Status != notification.StatusRetrying {
		t.Errorf("the notification not yet due is %s, want retrying", n.Status)
	}
}

// brokenStore fails every call, as a lost database does.
type brokenStore struct{}

func (brokenStore) QueueDue(context.Context, int) (int, error) {
	return 0, errors.New("connection lost")
}

func (brokenStore) ReapExpiredClaims(context.Context, int, int) (int, error) {
	return 0, errors.New("connection lost")
}

func (brokenStore) DrainOutbox(context.Context, int, func(context.Context, []notification.OutboxEntry) error) (int, error) {
	return 0, errors.New("connection lost")
}

func TestLoopsStopWithTheDatabaseError(t *testing.T) {
	if err := NewScheduler(brokenStore{}, SchedulerConfig{Interval: time.Second, BatchSize: 10, MaxAttempts: 8}, testMetrics(), discard).Run(t.Context()); err == nil {
		t.Error("scheduler: Run = nil, want the database error")
	}
	if err := NewRelay(brokenStore{}, noWait{}, &recordingPublisher{}, RelayConfig{BatchSize: 10, PollInterval: time.Second}, testMetrics(), discard).Run(t.Context()); err == nil {
		t.Error("relay: Run = nil, want the database error")
	}
}

// TestSchedulerReapsALostWorkersNotification: a worker claimed a notification and was lost; once its lease runs out
// the scheduler records the attempt as unknown and queues the notification again (ADR 0012).
func TestSchedulerReapsALostWorkersNotification(t *testing.T) {
	t.Parallel()
	store, pool := newStore(t)
	n := create(t, store, notification.ChannelSMS, "+905551234567", notification.PriorityNormal)
	if _, err := store.Claim(t.Context(), n.ID, time.Minute, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET claimed_until = now() - interval '1 second' WHERE id = $1", n.ID); err != nil {
		t.Fatal(err)
	}

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- NewScheduler(store, SchedulerConfig{Interval: 50 * time.Millisecond, BatchSize: 10, MaxAttempts: 8}, testMetrics(), discard).Run(ctx)
	}()
	waitFor(t, "the lost notification to be queued again", func() bool {
		got, err := store.Get(t.Context(), n.ID)
		return err == nil && got.Status == notification.StatusQueued
	})
	stop()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if records, _ := store.Attempts(t.Context(), n.ID); len(records) != 1 || records[0].Outcome != notification.OutcomeUnknown {
		t.Errorf("records = %+v, want the lost attempt as unknown", records)
	}
}

func TestLoopsCountWhatTheyMove(t *testing.T) {
	t.Parallel()
	store, pool := newStore(t)
	create(t, store, notification.ChannelSMS, "+905551234567", notification.PriorityNormal)
	due := create(t, store, notification.ChannelSMS, "+905551234568", notification.PriorityNormal)
	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET status = 'retrying', next_attempt_at = now() WHERE id = $1", due.ID); err != nil {
		t.Fatal(err)
	}
	m := testMetrics()

	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	var pub recordingPublisher
	go func() {
		_ = NewRelay(store, noWait{}, &pub, RelayConfig{BatchSize: 10, PollInterval: time.Second}, m, discard).Run(ctx)
	}()
	go func() {
		_ = NewScheduler(store, SchedulerConfig{Interval: 20 * time.Millisecond, BatchSize: 10, MaxAttempts: 8}, m, discard).Run(ctx)
	}()
	waitFor(t, "the queued and the due notification to be published", func() bool { return testutil.ToFloat64(m.published) >= 3 })
	if got := testutil.ToFloat64(m.queued); got != 1 {
		t.Errorf("queued = %v, want the one due", got)
	}
}
