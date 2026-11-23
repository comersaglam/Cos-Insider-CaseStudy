package delivery

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sony/gobreaker/v2"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
)

// scriptedSender answers each send with the next outcome of its script, repeating the last.
type scriptedSender struct {
	mu     sync.Mutex
	script []Outcome
	sends  atomic.Int32
}

func (s *scriptedSender) Send(context.Context, notification.Notification) Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := int(s.sends.Add(1)) - 1
	return s.script[min(i, len(s.script)-1)]
}

// breakerConfig opens the breaker once three attempts in a minute have been made and half of them failed.
func breakerConfig() ConsumerConfig {
	cfg := testConfig()
	cfg.Concurrency, cfg.BreakerMinRequests, cfg.BreakerFailurePercent = 1, 3, 50
	cfg.BreakerWindow, cfg.BreakerOpen = time.Minute, time.Hour
	return cfg
}

func messages(t *testing.T, a *acks, n int) <-chan amqp.Delivery {
	t.Helper()
	var msgs []amqp.Delivery
	for range n {
		msgs = append(msgs, message(t))
	}
	return deliveriesOf(a, msgs...)
}

var unavailable = Outcome{Class: Transient, StatusCode: http.StatusServiceUnavailable, Err: errors.New("provider answered 503")}

// TestBreakerOpensAndStopsTakingMessages: after three transient failures, all of the attempts so far, the breaker opens and the
// consumer takes no further message, so none is claimed and no attempt is spent while the provider is down.
func TestBreakerOpensAndStopsTakingMessages(t *testing.T) {
	var a acks
	store := &fakeStore{}
	sender := &scriptedSender{script: []Outcome{unavailable}}
	c := NewConsumer(store, sender, breakerConfig(), testMetrics(), discard)

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, messages(t, &a, 10)) }()
	waitUntil(t, func() bool { acked, _ := a.counts(); return acked == 3 })
	time.Sleep(200 * time.Millisecond) // time for a fourth message, were the breaker to let one through
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if sender.sends.Load() != 3 || store.claimedTimes != 3 {
		t.Errorf("sent %d, claimed %d; want 3 and 3 with the breaker open", sender.sends.Load(), store.claimedTimes)
	}
	if c.breaker.State() != gobreaker.StateOpen {
		t.Errorf("breaker is %s, want open", c.breaker.State())
	}
}

// TestBreakerProbesAndCloses: once the open interval passes, one probe goes through; the provider has recovered,
// so the breaker closes and the remaining messages are sent.
func TestBreakerProbesAndCloses(t *testing.T) {
	var a acks
	cfg := breakerConfig()
	cfg.BreakerOpen = 200 * time.Millisecond
	sender := &scriptedSender{script: []Outcome{unavailable, unavailable, unavailable, {Class: Success, StatusCode: 202}}}
	if err := runUntilSettled(t, NewConsumer(&fakeStore{}, sender, cfg, testMetrics(), discard), messages(t, &a, 6), &a, 6); err != nil {
		t.Fatal(err)
	}
	if sender.sends.Load() != 6 {
		t.Errorf("sent %d, want 6: three failures, the probe, and two more", sender.sends.Load())
	}
}

// TestBreakerIgnoresAnswersThatSayNothingOfTheProvidersHealth: a permanent rejection or a rate-limit answer is no
// sign of an outage, so it neither opens the breaker nor counts towards opening it (ADR 0007).
func TestBreakerIgnoresAnswersThatSayNothingOfTheProvidersHealth(t *testing.T) {
	var a acks
	sender := &scriptedSender{script: []Outcome{
		{Class: Permanent, StatusCode: 422, Err: errors.New("invalid")},
		{Class: Transient, StatusCode: 429, Err: errors.New("slow down")},
		{Class: PermanentAlert, StatusCode: 401, Err: errors.New("unauthorized")},
		{Class: Transient, StatusCode: 429, Err: errors.New("slow down")},
	}}
	c := NewConsumer(&fakeStore{}, sender, breakerConfig(), testMetrics(), discard)
	if err := runUntilSettled(t, c, messages(t, &a, 8), &a, 8); err != nil {
		t.Fatal(err)
	}
	if c.breaker.State() != gobreaker.StateClosed {
		t.Errorf("breaker is %s after rejections and rate limits, want closed", c.breaker.State())
	}
}

// closedBreaker reports itself closed but refuses every request, as a breaker does that opened between the loop's
// look at it and the send.
type closedBreaker struct{}

func (closedBreaker) State() gobreaker.State { return gobreaker.StateClosed }

func (closedBreaker) Allow() (func(error), error) { return nil, gobreaker.ErrOpenState }

// TestARefusedSendReleasesItsClaim: the breaker opened after the notification was claimed; the claim is undone,
// attempt included, and the message goes back to the queue.
func TestARefusedSendReleasesItsClaim(t *testing.T) {
	var a acks
	store := &fakeStore{}
	sender := &scriptedSender{script: []Outcome{{Class: Success}}}
	c := NewConsumer(store, sender, testConfig(), testMetrics(), discard)
	c.breaker = closedBreaker{}

	if err := runUntilSettled(t, c, messages(t, &a, 1), &a, 1); err != nil {
		t.Fatal(err)
	}
	if sender.sends.Load() != 0 || len(store.released) != 1 || len(a.requeued) != 1 {
		t.Errorf("sent %d, released %d, requeued %d; want 0, 1, 1", sender.sends.Load(), len(store.released), len(a.requeued))
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 10 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var accepted = Outcome{Class: Success, StatusCode: http.StatusAccepted, MessageID: "m"}

// TestBreakerOpensOnAFailureRatioNotARun (ADR 0036): failures in a row among many successes, as retries queued
// together arrive, do not open the breaker; a failure ratio at the threshold over enough attempts does.
func TestBreakerOpensOnAFailureRatioNotARun(t *testing.T) {
	run := func(script []Outcome) gobreaker.State {
		var a acks
		cfg := breakerConfig()
		cfg.BreakerMinRequests = 10
		c := NewConsumer(&fakeStore{}, &scriptedSender{script: append(script, accepted)}, cfg, testMetrics(), discard)
		if err := runUntilSettled(t, c, messages(t, &a, len(script)), &a, len(script)); err != nil {
			t.Fatal(err)
		}
		return c.breaker.State()
	}
	var burst []Outcome // 20 successes, then 6 failures in a row: 23% of 26
	for range 20 {
		burst = append(burst, accepted)
	}
	for range 6 {
		burst = append(burst, unavailable)
	}
	if got := run(burst); got != gobreaker.StateClosed {
		t.Errorf("six failures in a row among 20 successes: breaker %s, want closed", got)
	}
	var outage []Outcome // alternating, then failing: half of 10 by the tenth
	for range 5 {
		outage = append(outage, accepted, unavailable)
	}
	if got := run(outage); got != gobreaker.StateOpen {
		t.Errorf("half of ten attempts failed: breaker %s, want open", got)
	}
}
