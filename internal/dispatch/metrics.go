package dispatch

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// Metrics counts what the relay and the scheduler move.
type Metrics struct {
	published prometheus.Counter
	queued    prometheus.Counter
	reaped    prometheus.Counter
}

// NewMetrics registers the dispatch metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	ns := observability.Namespace
	m := &Metrics{
		published: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns, Name: "outbox_published_total", Help: "Outbox rows the relay published to RabbitMQ.",
		}),
		queued: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns, Name: "due_queued_total", Help: "Scheduled and retrying notifications the scheduler queued when due.",
		}),
		reaped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns, Name: "claims_reaped_total", Help: "Notifications taken back from workers whose lease ran out (ADR 0012).",
		}),
	}
	reg.MustRegister(m.published, m.queued, m.reaped)
	return m
}
