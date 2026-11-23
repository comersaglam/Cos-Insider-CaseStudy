package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestRunReportsMissingConfiguration(t *testing.T) {
	err := run(t.Context(), nil, env(nil), io.Discard)
	for _, key := range []string{"DATABASE_URL", "AMQP_URL", "PROVIDER_URL"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("run = %v, want %s reported", err, key)
		}
	}
}

func TestRunTakesNoArguments(t *testing.T) {
	if err := run(t.Context(), []string{"serve"}, env(nil), io.Discard); err == nil {
		t.Error("run accepted an argument")
	}
}

// provider is a fake provider recording each request's recipient and idempotency key.
type provider struct {
	mu     sync.Mutex
	calls  map[string]int
	keys   map[string]string
	answer func(to string) int
	block  chan struct{} // when set, each request waits for it to close
	called chan string
}

func (p *provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	_ = json.UnmarshalRead(r.Body, &req)
	p.mu.Lock()
	p.calls[req.To]++
	p.keys[req.To] = r.Header.Get("Idempotency-Key")
	p.mu.Unlock()
	if p.called != nil {
		p.called <- req.To
	}
	if p.block != nil {
		<-p.block
	}
	status := p.answer(req.To)
	w.WriteHeader(status)
	if status == http.StatusAccepted {
		_, _ = io.WriteString(w, `{"messageId":"m-`+req.To+`","status":"accepted"}`)
	}
}

func (p *provider) count(to string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[to]
}

// startWorker runs the worker against a database and a virtual host of the test's own and the given provider,
// and returns a service to create notifications with, as the API does, and the store to inspect them. The worker
// stops when stop is called or the test ends, and must stop cleanly.
func startWorker(t *testing.T, p *provider) (svc *notification.Service, store *notification.Store, ops string, stop func()) {
	t.Helper()
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	dsn := testenv.PostgresDSN(t)
	pool, err := postgres.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store = notification.NewStore(pool)

	// Made here, before stop is registered: cleanups run last-registered first, so the worker stops before its
	// virtual host is deleted.
	amqpURL := testenv.RabbitMQVhostURL(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	logs, logsW := io.Pipe()
	go func() {
		done <- run(ctx, nil, env(map[string]string{
			"DATABASE_URL": dsn, "AMQP_URL": amqpURL, "PROVIDER_URL": srv.URL,
			"PROVIDER_TIMEOUT": "2s", "SHUTDOWN_TIMEOUT": "5s", "MAX_ATTEMPTS": "2", "RETRY_BASE": "50ms",
			"SCHEDULER_INTERVAL": "50ms", "RELAY_POLL_INTERVAL": "1s", "OPS_ADDR": "127.0.0.1:0",
		}), logsW)
		_ = logsW.Close()
	}()
	// The "worker started" line carries the operations server's address; reading goes on so logging never blocks.
	opsAddr := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(logs)
		for scanner.Scan() {
			var line map[string]any
			if json.Unmarshal(scanner.Bytes(), &line) == nil && line["msg"] == "worker started" {
				opsAddr <- line["ops_addr"].(string)
			}
		}
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			// The client may hold a connection it dialled and never used; a server waits five seconds before it counts
			// such a connection idle, longer than the test's shutdown timeout allows. Closing it lets the server stop
			// at once.
			http.DefaultClient.CloseIdleConnections()
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("run after stop = %v, want nil", err)
				}
			case <-time.After(15 * time.Second):
				t.Error("the worker did not stop within 15 s")
			}
		})
	}
	t.Cleanup(stop)
	select {
	case a := <-opsAddr:
		ops = "http://" + a
	case <-time.After(30 * time.Second):
		t.Fatal("the worker did not start within 30 s")
	}
	return notification.NewService(store, slog.New(slog.DiscardHandler)), store, ops, stop
}

func waitForStatus(t *testing.T, store *notification.Store, id uuid.UUID, want notification.Status) notification.Notification {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		n, err := store.Get(t.Context(), id)
		if err == nil && n.Status == want {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is %s after 20 s, want %s", id, n.Status, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWorkerDeliversEndToEnd creates notifications as the API does and lets the worker take them through the
// outbox, RabbitMQ and the consumers to a fake provider: two are sent, and one the provider keeps answering 503 is
// retried through the scheduler and fails once its two attempts are spent.
func TestWorkerDeliversEndToEnd(t *testing.T) {
	p := &provider{calls: map[string]int{}, keys: map[string]string{}, answer: func(to string) int {
		if to == "device-token-503" {
			return http.StatusServiceUnavailable
		}
		return http.StatusAccepted
	}}
	svc, store, ops, _ := startWorker(t, p)

	create := func(d notification.Draft) notification.Notification {
		n, _, err := svc.Create(t.Context(), d, "")
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	sms := create(notification.Draft{To: "+905551234567", Channel: notification.ChannelSMS, Content: "one", Priority: notification.PriorityHigh})
	email := create(notification.Draft{To: "ayse@example.com", Channel: notification.ChannelEmail, Content: "two", Priority: notification.PriorityNormal})
	push := create(notification.Draft{To: "device-token-503", Channel: notification.ChannelPush, Content: "three", Priority: notification.PriorityLow})

	if n := waitForStatus(t, store, sms.ID, notification.StatusSent); n.ProviderMessageID != "m-+905551234567" {
		t.Errorf("sms provider ID = %q", n.ProviderMessageID)
	}
	waitForStatus(t, store, email.ID, notification.StatusSent)
	if n := waitForStatus(t, store, push.ID, notification.StatusFailed); n.FailureReason != notification.FailureAttemptsExhausted || n.Attempts != 2 {
		t.Errorf("push = %s after %d attempts, want attempts_exhausted after 2", n.FailureReason, n.Attempts)
	}

	if p.count("+905551234567") != 1 || p.count("ayse@example.com") != 1 || p.count("device-token-503") != 2 {
		t.Errorf("provider calls = %v, want 1, 1 and 2", p.calls)
	}
	if p.keys["+905551234567"] != sms.ID.String() {
		t.Errorf("Idempotency-Key = %q, want the notification's ID", p.keys["+905551234567"])
	}
	if records, _ := store.Attempts(t.Context(), push.ID); len(records) != 2 || records[1].StatusCode != 503 {
		t.Errorf("push attempts = %+v, want two 503s", records)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ops+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	for _, series := range []string{
		`notifications_delivery_attempts_total{channel="sms",outcome="success"} 1`,
		`notifications_delivery_attempts_total{channel="push",outcome="transient"} 2`,
		`notifications_waiting{channel="sms",status="queued"} 0`,
		`notifications_rabbitmq_queue_messages{queue="notifications.dlq"} 0`,
		`notifications_circuit_breaker_state{channel="email"} 0`,
		`notifications_outbox_published_total`,
	} {
		if !strings.Contains(string(body), series) {
			t.Errorf("the worker's /metrics lacks %s", series)
		}
	}

	probe, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ops+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := http.DefaultClient.Do(probe)
	if err != nil {
		t.Fatal(err)
	}
	_ = ready.Body.Close()
	if ready.StatusCode != http.StatusOK {
		t.Errorf("GET /readyz on the operations server = %d, want 200", ready.StatusCode)
	}
	if err := run(t.Context(), []string{"healthcheck"}, env(map[string]string{"OPS_ADDR": strings.TrimPrefix(ops, "http://")}), io.Discard); err != nil {
		t.Errorf("worker healthcheck against the running worker = %v, want nil", err)
	}
}

// TestWorkerFinishesASendInFlightWhenStopped stops the worker while the provider is still answering: the send
// finishes, its outcome is recorded, and the worker stops cleanly.
func TestWorkerFinishesASendInFlightWhenStopped(t *testing.T) {
	p := &provider{calls: map[string]int{}, keys: map[string]string{}, answer: func(string) int { return http.StatusAccepted },
		block: make(chan struct{}), called: make(chan string, 1)}
	svc, store, _, stop := startWorker(t, p)

	n, _, err := svc.Create(t.Context(), notification.Draft{To: "+905551234567", Channel: notification.ChannelSMS, Content: "x", Priority: notification.PriorityNormal}, "")
	if err != nil {
		t.Fatal(err)
	}
	<-p.called
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	time.Sleep(200 * time.Millisecond) // the worker is stopping while the provider still holds the request
	close(p.block)
	<-stopped

	if got, _ := store.Get(t.Context(), n.ID); got.Status != notification.StatusSent {
		t.Errorf("status = %s, want sent: the send in flight finished and was recorded", got.Status)
	}
}
