package delivery

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
)

func testMetrics() *Metrics { return NewMetrics(prometheus.NewRegistry()) }

func TestConsumerRecordsItsMetrics(t *testing.T) {
	var a acks
	m := testMetrics()
	sender := &scriptedSender{script: []Outcome{
		{Class: Success, StatusCode: 202, Latency: 120 * time.Millisecond},
		{Class: Transient, StatusCode: 503, Latency: 80 * time.Millisecond, Err: errors.New("busy")},
	}}
	store := &fakeStore{takenOver: true}
	unreadable := amqp.Delivery{Body: []byte("not json"), MessageId: "?"}
	c := NewConsumer(store, sender, testConfig(), m, discard)
	if err := runUntilSettled(t, c, deliveriesOf(&a, message(t), message(t), unreadable), &a, 3); err != nil {
		t.Fatal(err)
	}

	sms := string(notification.ChannelSMS)
	for name, got := range map[string]float64{
		"success attempts":   testutil.ToFloat64(m.attempts.WithLabelValues(sms, "success")),
		"transient attempts": testutil.ToFloat64(m.attempts.WithLabelValues(sms, "transient")),
		"takeovers":          testutil.ToFloat64(m.takeovers.WithLabelValues(sms)),
		"dead-lettered":      testutil.ToFloat64(m.deadLettered.WithLabelValues(sms)),
	} {
		want := 1.0
		if name == "takeovers" {
			want = 2
		}
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if n := testutil.CollectAndCount(m.latency); n != 1 {
		t.Errorf("%d latency series, want one for the channel", n)
	}
	if got := testutil.ToFloat64(m.inFlight.WithLabelValues(sms)); got != 0 {
		t.Errorf("sends in flight after every send ended = %v, want 0", got)
	}
}

func TestBreakerStateIsAGauge(t *testing.T) {
	var a acks
	m := testMetrics()
	c := NewConsumer(&fakeStore{}, &scriptedSender{script: []Outcome{unavailable}}, breakerConfig(), m, discard)
	sms := string(notification.ChannelSMS)
	if got := testutil.ToFloat64(m.breakerState.WithLabelValues(sms)); got != 0 {
		t.Errorf("a new consumer's breaker state = %v, want 0 (closed)", got)
	}
	if err := runUntilSettled(t, c, messages(t, &a, 3), &a, 3); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(m.breakerState.WithLabelValues(sms)); got != 2 {
		t.Errorf("breaker state after three failures = %v, want 2 (open)", got)
	}
}

// TestConsumerReportsEveryOutcomeFromTheStart: every channel and outcome series exists, at zero, as soon as the
// consumer is built. Prometheus takes a counter's first sample as its starting point, so a series born with its
// first failure would hide that failure from rate() and increase(): a burst's first failures went uncounted.
func TestConsumerReportsEveryOutcomeFromTheStart(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewConsumer(&fakeStore{}, fakeSender{}, testConfig(), NewMetrics(reg), discard)
	want := map[string]bool{}
	for _, class := range []Class{Success, Permanent, PermanentAlert, Transient} {
		want[`notifications_delivery_attempts_total{channel="sms",outcome="`+string(class)+`"}`] = true
	}
	want[`notifications_claim_takeovers_total{channel="sms"}`] = true
	want[`notifications_dead_lettered_total{channel="sms"}`] = true

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			series := f.GetName() + "{"
			for i, l := range m.GetLabel() {
				if i > 0 {
					series += ","
				}
				series += l.GetName() + `="` + l.GetValue() + `"`
			}
			delete(want, series+"}")
		}
	}
	for series := range want {
		t.Errorf("%s is missing until its first event", series)
	}
}
