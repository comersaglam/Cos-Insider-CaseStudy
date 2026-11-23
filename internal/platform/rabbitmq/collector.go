package rabbitmq

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// QueueDepthCollector reports, each time Prometheus scrapes, how many messages are ready in each queue and how many
// consumers it has (ADR 0009, 0026). A dead-letter queue above zero is a message nobody could attribute to a
// notification, worth an alert (ADR 0014).
type QueueDepthCollector struct {
	conn      *amqp.Connection
	queues    []string
	messages  *prometheus.Desc
	consumers *prometheus.Desc
}

// NewQueueDepthCollector returns a collector of the given queues over conn, to be registered on a registry.
func NewQueueDepthCollector(conn *amqp.Connection, queues []string) *QueueDepthCollector {
	ns := observability.Namespace
	return &QueueDepthCollector{
		conn: conn, queues: queues,
		messages: prometheus.NewDesc(prometheus.BuildFQName(ns, "rabbitmq", "queue_messages"),
			"Messages ready in each queue, counted when scraped.", []string{"queue"}, nil),
		consumers: prometheus.NewDesc(prometheus.BuildFQName(ns, "rabbitmq", "queue_consumers"),
			"Consumers of each queue, counted when scraped; with single active consumer one of them receives.", []string{"queue"}, nil),
	}
}

// Describe sends the collector's two metrics.
func (c *QueueDepthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.messages
	ch <- c.consumers
}

// Collect inspects each queue on a channel of its own, closed afterwards; a queue that cannot be inspected is
// reported as an error, which the scrape shows.
func (c *QueueDepthCollector) Collect(ch chan<- prometheus.Metric) {
	amqpCh, err := c.conn.Channel()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.messages, fmt.Errorf("open a channel: %w", err))
		return
	}
	defer func() { _ = amqpCh.Close() }()
	for _, name := range c.queues {
		q, err := amqpCh.QueueDeclarePassive(name, true, false, false, false, nil)
		if err != nil {
			// A failed passive declaration closes the channel; the remaining queues cannot be read on it.
			ch <- prometheus.NewInvalidMetric(c.messages, fmt.Errorf("inspect %s: %w", name, err))
			return
		}
		ch <- prometheus.MustNewConstMetric(c.messages, prometheus.GaugeValue, float64(q.Messages), name)
		ch <- prometheus.MustNewConstMetric(c.consumers, prometheus.GaugeValue, float64(q.Consumers), name)
	}
}
