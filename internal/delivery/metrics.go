package delivery

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sony/gobreaker/v2"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// Metrics measures sending, by channel. A notification's ID is never a label: each label value is a time series
// Prometheus keeps.
type Metrics struct {
	attempts     *prometheus.CounterVec
	latency      *prometheus.HistogramVec
	breakerState *prometheus.GaugeVec
	limiterWait  *prometheus.HistogramVec
	inFlight     *prometheus.GaugeVec
	takeovers    *prometheus.CounterVec
	deadLettered *prometheus.CounterVec
}

// NewMetrics registers the sending metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	ns := observability.Namespace
	m := &Metrics{
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns, Name: "delivery_attempts_total",
			Help: "Attempts to send, by channel and outcome (success, permanent, permanent_alert, transient).",
		}, []string{"channel", "outcome"}),
		latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns, Name: "provider_request_duration_seconds",
			Help: "How long the provider takes to answer a send, by channel.", Buckets: prometheus.DefBuckets,
		}, []string{"channel"}),
		breakerState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns, Name: "circuit_breaker_state",
			Help: "Each channel's circuit breaker: 0 closed, 1 half-open, 2 open.",
		}, []string{"channel"}),
		limiterWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns, Name: "rate_limiter_wait_seconds",
			Help:    "How long a message waits for the channel's rate limit before it is sent.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		}, []string{"channel"}),
		inFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns, Name: "sends_in_flight", Help: "Sends started whose result is not yet settled, by channel.",
		}, []string{"channel"}),
		takeovers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns, Name: "claim_takeovers_total",
			Help: "Notifications claimed from a worker whose lease ran out: each may have been sent twice (ADR 0012).",
		}, []string{"channel"}),
		deadLettered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns, Name: "dead_lettered_total", Help: "Messages rejected into the dead-letter queue, by channel.",
		}, []string{"channel"}),
	}
	reg.MustRegister(m.attempts, m.latency, m.breakerState, m.limiterWait, m.inFlight, m.takeovers, m.deadLettered)
	return m
}

// breakerValue maps a breaker state to the gauge's value.
func breakerValue(s gobreaker.State) float64 {
	switch s {
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	}
	return 0
}
