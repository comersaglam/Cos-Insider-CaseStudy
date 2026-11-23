package rabbitmq_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/trace"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
)

// setup connects to the test broker and returns a topology whose names belong to this test alone.
func setup(t *testing.T) (*amqp.Connection, rabbitmq.Topology) {
	t.Helper()
	conn, err := rabbitmq.Connect(testenv.RabbitMQURL(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	prefix := "t-" + strings.ToLower(rand.Text())[:10]
	return conn, rabbitmq.Topology{
		Exchange:    prefix + ".work",
		DeadLetter:  prefix + ".dlq",
		MaxPriority: 3,
		Queues:      []rabbitmq.Queue{{Name: prefix + ".sms", RoutingKey: "sms"}, {Name: prefix + ".email", RoutingKey: "email"}},
	}
}

func declare(t *testing.T, conn *amqp.Connection, top rabbitmq.Topology) {
	t.Helper()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	if err := rabbitmq.Declare(ch, top); err != nil {
		t.Fatalf("Declare: %v", err)
	}
}

// depth returns how many messages wait in a queue.
func depth(t *testing.T, conn *amqp.Connection, queue string) int {
	t.Helper()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	q, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("inspect %s: %v", queue, err)
	}
	return q.Messages
}

// receive takes up to n deliveries, waiting at most wait for each.
func receive(deliveries <-chan amqp.Delivery, n int, wait time.Duration) []amqp.Delivery {
	var got []amqp.Delivery
	for range n {
		select {
		case d := <-deliveries:
			got = append(got, d)
		case <-time.After(wait):
			return got
		}
	}
	return got
}

func TestDeclareTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	declare(t, conn, top) // as every worker does when it starts
	for _, q := range top.Queues {
		if d := depth(t, conn, q.Name); d != 0 {
			t.Errorf("%s holds %d messages, want 0", q.Name, d)
		}
	}
	if d := depth(t, conn, top.DeadLetter); d != 0 {
		t.Errorf("the dead-letter queue holds %d, want 0", d)
	}
}

func TestPublishReturnsOnceRabbitMQConfirms(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()

	err = pub.Publish(t.Context(),
		rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "sms", ID: "n1", Body: []byte("1"), CorrelationID: "c1"},
		rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "sms", ID: "n2", Body: []byte("2")},
		rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "email", ID: "n3", Body: []byte("3")},
	)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if d := depth(t, conn, top.Queues[0].Name); d != 2 {
		t.Errorf("sms queue holds %d, want 2", d)
	}
	if d := depth(t, conn, top.Queues[1].Name); d != 1 {
		t.Errorf("email queue holds %d, want 1", d)
	}
}

// TestPublishReportsAMessageNoQueueTakes guards against the silent loss a confirm alone allows: RabbitMQ confirms a
// message it routed nowhere. The publish is mandatory, so the message comes back and the publish fails.
func TestPublishReportsAMessageNoQueueTakes(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()

	err = pub.Publish(t.Context(), rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "fax", ID: "n1", Body: []byte("x")})
	if !errors.Is(err, rabbitmq.ErrUnroutable) {
		t.Errorf("Publish = %v, want ErrUnroutable", err)
	}
}

// TestPublishReportsAMessageRabbitMQRefuses uses a queue that refuses messages once full, so RabbitMQ answers the
// publish with a negative confirm.
func TestPublishReportsAMessageRabbitMQRefuses(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	full := top.Queues[0].Name
	// Durable: RabbitMQ 4 refuses transient non-exclusive queues. The name is the test's own.
	if _, err := ch.QueueDeclare(full, true, false, false, false, amqp.Table{"x-max-length": 0, "x-overflow": "reject-publish"}); err != nil {
		t.Fatal(err)
	}

	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	// The default exchange routes by queue name.
	err = pub.Publish(t.Context(), rabbitmq.Message{Exchange: "", RoutingKey: full, ID: "n1", Body: []byte("x")})
	if !errors.Is(err, rabbitmq.ErrNotConfirmed) {
		t.Errorf("Publish = %v, want ErrNotConfirmed", err)
	}
}

func TestPublishedMessagesCarryTheirProperties(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	if err := pub.Publish(t.Context(), rabbitmq.Message{
		Exchange: top.Exchange, RoutingKey: "sms", ID: "n1", Body: []byte(`{"id":"n1"}`), Priority: 2, CorrelationID: "c-9",
		Headers: map[string]string{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	}); err != nil {
		t.Fatal(err)
	}

	deliveries, ch := consume(t, conn, top.Queues[0].Name, 1)
	defer func() { _ = ch.Close() }()
	got := receive(deliveries, 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatal("no message delivered")
	}
	d := got[0]
	if d.MessageId != "n1" || d.Priority != 2 || d.DeliveryMode != amqp.Persistent ||
		d.ContentType != "application/json" || d.Headers[rabbitmq.CorrelationHeader] != "c-9" || d.Timestamp.IsZero() ||
		d.Headers["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Errorf("delivery = id %q priority %d mode %d type %q headers %v time %v",
			d.MessageId, d.Priority, d.DeliveryMode, d.ContentType, d.Headers, d.Timestamp)
	}
}

func consume(t *testing.T, conn *amqp.Connection, queue string, prefetch int) (<-chan amqp.Delivery, *amqp.Channel) {
	t.Helper()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := rabbitmq.Consume(ch, queue, prefetch)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	return deliveries, ch
}

func TestHigherPriorityIsDeliveredFirst(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	queue := top.Queues[0].Name
	for i, p := range []uint8{1, 2, 3, 1, 3} {
		if err := pub.Publish(t.Context(), rabbitmq.Message{
			Exchange: top.Exchange, RoutingKey: "sms", ID: string(rune('a' + i)), Body: []byte{p}, Priority: p,
		}); err != nil {
			t.Fatal(err)
		}
	}

	deliveries, ch := consume(t, conn, queue, 10)
	defer func() { _ = ch.Close() }()
	var order []uint8
	for _, d := range receive(deliveries, 5, 5*time.Second) {
		order = append(order, d.Priority)
	}
	if string(order) != string([]uint8{3, 3, 2, 1, 1}) {
		t.Errorf("priorities delivered in order %v, want 3 3 2 1 1", order)
	}
}

// TestOnlyOneConsumerIsActive checks single active consumer (ADR 0005): of two consumers, one receives everything;
// when it goes away, the other takes over, and the messages it had not acknowledged are delivered again.
func TestOnlyOneConsumerIsActive(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	queue := top.Queues[0].Name

	first, firstCh := consume(t, conn, queue, 10)
	second, secondCh := consume(t, conn, queue, 10)
	defer func() { _ = secondCh.Close() }()
	var msgs []rabbitmq.Message
	for i := range 6 {
		msgs = append(msgs, rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "sms", ID: string(rune('a' + i)), Body: []byte("x")})
	}
	if err := pub.Publish(t.Context(), msgs...); err != nil {
		t.Fatal(err)
	}

	if got := receive(first, 6, 5*time.Second); len(got) != 6 {
		t.Fatalf("the first consumer received %d, want all 6", len(got))
	}
	if got := receive(second, 1, 500*time.Millisecond); len(got) != 0 {
		t.Fatalf("the second consumer received %d while the first was active, want 0", len(got))
	}

	_ = firstCh.Close() // the active consumer goes away without acknowledging
	if got := receive(second, 6, 5*time.Second); len(got) != 6 || !got[0].Redelivered {
		t.Errorf("after failover the second consumer received %d, want the 6 unacknowledged, redelivered", len(got))
	}
}

func TestARejectedMessageIsDeadLettered(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	if err := pub.Publish(t.Context(), rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "email", ID: "bad", Body: []byte("not json")}); err != nil {
		t.Fatal(err)
	}

	deliveries, ch := consume(t, conn, top.Queues[1].Name, 1)
	defer func() { _ = ch.Close() }()
	got := receive(deliveries, 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatal("no message delivered")
	}
	if err := got[0].Reject(false); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for depth(t, conn, top.DeadLetter) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the rejected message did not reach the dead-letter queue")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestConnectFailsWhenNothingAnswers(t *testing.T) {
	if _, err := rabbitmq.Connect("amqp://guest:guest@127.0.0.1:1/", "test"); err == nil {
		t.Fatal("Connect succeeded against a closed port")
	}
}

// TestDeclareRefusesAQueueWithOtherSettings: a queue that exists with other settings (here without priorities) is
// an error, which stops the process at start instead of letting it work against a topology it does not expect.
func TestDeclareRefusesAQueueWithOtherSettings(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ch.QueueDeclare(top.Queues[0].Name, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := rabbitmq.Declare(ch, top); err == nil || !strings.Contains(err.Error(), top.Queues[0].Name) {
		t.Errorf("Declare = %v, want an error naming the queue", err)
	}
}

func TestPublishStopsWhenTheContextHasEnded(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := pub.Publish(ctx, rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "sms", ID: "n1"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Publish = %v, want context.Canceled", err)
	}
	if d := depth(t, conn, top.Queues[0].Name); d != 0 {
		t.Errorf("the queue holds %d, want nothing published", d)
	}
}

func TestQueueDepthCollectorReadsTheQueuesOnScrape(t *testing.T) {
	t.Parallel()
	conn, top := setup(t)
	declare(t, conn, top)
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pub.Close() }()
	for _, id := range []string{"n1", "n2"} {
		if err := pub.Publish(t.Context(), rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "sms", ID: id, Body: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}

	c := rabbitmq.NewQueueDepthCollector(conn, []string{top.Queues[0].Name, top.DeadLetter})
	want := `
# HELP notifications_rabbitmq_queue_consumers Consumers of each queue, counted when scraped; with single active consumer one of them receives.
# TYPE notifications_rabbitmq_queue_consumers gauge
notifications_rabbitmq_queue_consumers{queue="` + top.DeadLetter + `"} 0
notifications_rabbitmq_queue_consumers{queue="` + top.Queues[0].Name + `"} 0
# HELP notifications_rabbitmq_queue_messages Messages ready in each queue, counted when scraped.
# TYPE notifications_rabbitmq_queue_messages gauge
notifications_rabbitmq_queue_messages{queue="` + top.DeadLetter + `"} 0
notifications_rabbitmq_queue_messages{queue="` + top.Queues[0].Name + `"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

// TestHeaderCarrierCarriesATrace: a trace injected into a message's headers is extracted from them, as the relay
// and the consumer do (ADR 0034).
func TestHeaderCarrierCarriesATrace(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})

	headers := amqp.Table{"X-Correlation-ID": "c-9"}
	observability.Propagator().Inject(trace.ContextWithSpanContext(context.Background(), sent), rabbitmq.HeaderCarrier(headers))
	if headers["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("headers = %v", headers)
	}
	got := trace.SpanContextFromContext(observability.Propagator().Extract(context.Background(), rabbitmq.HeaderCarrier(headers)))
	if got.TraceID() != tid || got.SpanID() != sid || !got.IsRemote() {
		t.Errorf("extracted %+v", got)
	}
	if keys := rabbitmq.HeaderCarrier(headers).Keys(); len(keys) != 2 {
		t.Errorf("keys = %v", keys)
	}
}
