package notification

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// scrapeTimeout bounds the queries one scrape runs.
const scrapeTimeout = 3 * time.Second

// waitingStatuses are the statuses of notifications not yet finished.
var waitingStatuses = []Status{StatusScheduled, StatusQueued, StatusRetrying, StatusSending}

// WaitingCollector reports, each time Prometheus scrapes, how many notifications wait by channel and status, and
// how many outbox rows wait for the relay (ADR 0009, 0026). Reading at scrape time costs nothing between scrapes
// and is never stale; the counts come from the database, so they agree across replicas.
type WaitingCollector struct {
	store   *Store
	waiting *prometheus.Desc
	outbox  *prometheus.Desc
}

// NewWaitingCollector returns a collector over store, to be registered on a registry.
func NewWaitingCollector(store *Store) *WaitingCollector {
	ns := observability.Namespace
	return &WaitingCollector{
		store: store,
		waiting: prometheus.NewDesc(prometheus.BuildFQName(ns, "", "waiting"),
			"Notifications not yet finished, by channel and status, counted when scraped.", []string{"channel", "status"}, nil),
		outbox: prometheus.NewDesc(prometheus.BuildFQName(ns, "", "outbox_pending"),
			"Outbox rows not yet published to RabbitMQ: how far the relay is behind.", nil, nil),
	}
}

// Describe sends the collector's two metrics.
func (c *WaitingCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.waiting
	ch <- c.outbox
}

// Collect counts and sends the metrics; when the database does not answer, it sends errors instead, and the scrape
// shows them.
func (c *WaitingCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	q := db.New(c.store.pool)

	rows, err := q.CountWaiting(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.waiting, err)
	} else {
		counts := map[[2]string]int{}
		for _, r := range rows {
			counts[[2]string{r.Channel, r.Status}] = r.Count
		}
		for _, channel := range Channels() {
			for _, status := range waitingStatuses {
				n := counts[[2]string{string(channel), string(status)}]
				ch <- prometheus.MustNewConstMetric(c.waiting, prometheus.GaugeValue, float64(n), string(channel), string(status))
			}
		}
	}

	pending, err := q.CountOutbox(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.outbox, err)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.outbox, prometheus.GaugeValue, float64(pending))
}
