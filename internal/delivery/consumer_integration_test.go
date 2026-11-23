package delivery

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"encoding/json/v2"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
)

// TestConsumerAgainstRabbitMQAndPostgreSQL publishes messages as the relay does and lets the consumer send them to
// a fake provider that answers by recipient: each notification ends as its answer says, a cancelled one is never
// sent, and a message published twice is sent once.
func TestConsumerAgainstRabbitMQAndPostgreSQL(t *testing.T) {
	pool, err := postgres.Connect(t.Context(), testenv.PostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := notification.NewStore(pool)

	var (
		mu    sync.Mutex
		calls = map[string]int{}
	)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			To string `json:"to"`
		}
		_ = json.UnmarshalRead(r.Body, &req)
		mu.Lock()
		calls[req.To]++
		mu.Unlock()
		switch req.To {
		case "+905550000001":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"messageId":"m-1","status":"accepted"}`)
		case "+905550000002":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
	}))
	t.Cleanup(provider.Close)

	conn, err := rabbitmq.Connect(testenv.RabbitMQURL(t), "delivery-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	prefix := "t-" + strings.ToLower(rand.Text())[:10]
	top := rabbitmq.Topology{Exchange: prefix + ".notifications", DeadLetter: prefix + ".dlq", MaxPriority: notification.MaxPriorityRank,
		Queues: []rabbitmq.Queue{{Name: prefix + ".sms", RoutingKey: "sms"}}}
	setup, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	if err := rabbitmq.Declare(setup, top); err != nil {
		t.Fatal(err)
	}
	_ = setup.Close()

	create := func(to string) notification.Notification {
		n, _, err := store.Create(t.Context(), notification.Notification{
			ID: uuid.NewV7(), To: to, Channel: notification.ChannelSMS, Content: "hi", Priority: notification.PriorityNormal,
			Status: notification.StatusQueued, CorrelationID: "c",
		}, notification.IdempotencyKey{})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	sent, retried, rejected, cancelled := create("+905550000001"), create("+905550000002"), create("+905550000003"), create("+905550000004")
	if _, err := store.Cancel(t.Context(), cancelled.ID); err != nil {
		t.Fatal(err)
	}

	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pub.Close() })
	var msgs []rabbitmq.Message
	for _, n := range []notification.Notification{sent, retried, rejected, cancelled, sent} { // sent twice
		body, _ := notification.QueueMessage{NotificationID: n.ID}.Marshal()
		msgs = append(msgs, rabbitmq.Message{Exchange: top.Exchange, RoutingKey: "sms", ID: n.ID.String(), Body: body})
	}
	if err := pub.Publish(t.Context(), msgs...); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	consumeCh, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumeCh.Close() })
	deliveries, err := rabbitmq.Consume(consumeCh, top.Queues[0].Name, cfg.Concurrency)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- NewConsumer(store, NewClient(provider.URL, time.Second), cfg, testMetrics(), discard).Run(ctx, deliveries)
	}()

	want := map[uuid.UUID]notification.Status{
		sent.ID: notification.StatusSent, retried.ID: notification.StatusRetrying,
		rejected.ID: notification.StatusFailed, cancelled.ID: notification.StatusCancelled,
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		settled := true
		for id, status := range want {
			if n, _ := store.Get(t.Context(), id); n.Status != status {
				settled = false
			}
		}
		if settled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the notifications did not reach their final statuses in 10 s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Let the duplicate of the sent notification be consumed too.
	time.Sleep(300 * time.Millisecond)
	stop()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}

	if n, _ := store.Get(t.Context(), sent.ID); n.ProviderMessageID != "m-1" || n.Attempts != 1 {
		t.Errorf("sent = %+v, want the provider's ID after one attempt", n)
	}
	if n, _ := store.Get(t.Context(), rejected.ID); n.FailureReason != notification.FailureProviderRejected {
		t.Errorf("rejected = %s, want provider_rejected", n.FailureReason)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["+905550000001"] != 1 || calls["+905550000004"] != 0 {
		t.Errorf("provider calls = %v; want the duplicate sent once and the cancelled never", calls)
	}
}
