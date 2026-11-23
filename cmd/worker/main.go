// Command worker delivers notifications: it relays the outbox to RabbitMQ, queues due retries and scheduled
// notifications, reaps the work of lost workers, and consumes each channel's queue, sending to the provider.
//
// It is the composition root of the worker process (ADR 0004, 0009): it reads the configuration, connects to
// PostgreSQL and RabbitMQ, declares the topology, and runs every loop under one errgroup until SIGINT or SIGTERM.
// A loop that fails stops the others and the process; it is then restarted (crash-only).
//
// Usage:
//
//	worker              run the worker
//	worker healthcheck  ask the worker's operations server on OPS_ADDR whether it is ready; exit 0 if it is, 1
//	                    otherwise. The container's health check runs it, the image having no shell or curl (ADR 0027).
//
// Configuration, from the environment:
//
//	DATABASE_URL         PostgreSQL URL (required)
//	AMQP_URL             RabbitMQ URL (required)
//	PROVIDER_URL         the provider's endpoint, such as a webhook.site URL (required)
//	LOG_LEVEL            debug, info, warn or error (default info)
//	PROVIDER_TIMEOUT     how long one send may take (default 10s)
//	SEND_CONCURRENCY     sends in flight per channel, and the prefetch count (default 32)
//	RATE_PER_SECOND      sends started per second per channel (default 100)
//	RETRY_BASE           the first retry's ceiling, doubled each attempt (default 10s)
//	RETRY_CAP            the highest ceiling (default 15m)
//	MAX_ATTEMPTS         attempts before a notification fails (default 8)
//	BREAKER_FAILURES     transient failures in a row that open a channel's breaker (default 5)
//	BREAKER_OPEN         how long it stays open before a probe (default 30s)
//	RELAY_POLL_INTERVAL  the relay's fallback poll when no notification wakes it (default 5s)
//	SCHEDULER_INTERVAL   how often due work is queued and lost work reaped (default 1s)
//	SHUTDOWN_TIMEOUT     how long sends in flight may take to finish when stopping (default 15s)
//	OPS_ADDR             where the operations server serves /metrics, /healthz and /readyz (default :9090)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/sync/errgroup"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/delivery"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/dispatch"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/health"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/config"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/rabbitmq"
)

// Batch sizes: the relay's matches the publisher's batch; the scheduler's bounds one statement.
const (
	relayBatch     = 256
	schedulerBatch = 1000
)

// defaultOpsAddr is where the operations server listens unless OPS_ADDR says otherwise.
const defaultOpsAddr = ":9090"

// leaseMargin is added to the provider timeout for a claim's lease, so a send always ends before its lease does
// (ADR 0012).
const leaseMargin = 30 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

// errRabbitMQClosed fails the readiness probe once the RabbitMQ connection is gone.
var errRabbitMQClosed = errors.New("the RabbitMQ connection is closed")

// topology is the service's exchanges and queues (ADR 0012): the direct exchange "notifications", a queue per
// channel bound by the channel's name, and the dead-letter queue.
func topology() rabbitmq.Topology {
	t := rabbitmq.Topology{
		Exchange: "notifications", DeadLetter: "notifications.dlq", MaxPriority: notification.MaxPriorityRank,
	}
	for _, c := range notification.Channels() {
		t.Queues = append(t.Queues, rabbitmq.Queue{Name: "notifications." + string(c), RoutingKey: string(c)})
	}
	return t
}

// run is the whole program, with its inputs passed in so that tests can drive it. It returns nil once ctx ends and
// every loop has stopped, and the first error otherwise.
func run(ctx context.Context, args []string, lookup func(string) (string, bool), stdout io.Writer) error {
	cfg := config.NewReader(lookup)
	if len(args) == 1 && args[0] == "healthcheck" {
		opsAddr := cfg.String("OPS_ADDR", defaultOpsAddr)
		if err := cfg.Err(); err != nil {
			return fmt.Errorf("invalid configuration:\n%w", err)
		}
		return httpx.Probe(ctx, opsAddr, "/readyz")
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments %q; the worker takes none, or healthcheck", args)
	}

	databaseURL := cfg.Secret("DATABASE_URL")
	amqpURL := cfg.Secret("AMQP_URL")
	// A webhook.site URL is a credential: whoever has it can read every request sent to it.
	providerURL := cfg.Secret("PROVIDER_URL")
	level := cfg.LogLevel("LOG_LEVEL", slog.LevelInfo)
	providerTimeout := cfg.Duration("PROVIDER_TIMEOUT", 10*time.Second)
	concurrency := cfg.Int("SEND_CONCURRENCY", 32)
	ratePerSecond := cfg.Int("RATE_PER_SECOND", 100)
	policy := delivery.RetryPolicy{
		Base: cfg.Duration("RETRY_BASE", 10*time.Second), Cap: cfg.Duration("RETRY_CAP", 15*time.Minute),
		MaxAttempts: cfg.Int("MAX_ATTEMPTS", 8),
	}
	breakerFailures := cfg.Int("BREAKER_FAILURES", 5)
	breakerOpen := cfg.Duration("BREAKER_OPEN", 30*time.Second)
	pollInterval := cfg.Duration("RELAY_POLL_INTERVAL", 5*time.Second)
	schedulerInterval := cfg.Duration("SCHEDULER_INTERVAL", time.Second)
	shutdownTimeout := cfg.Duration("SHUTDOWN_TIMEOUT", 15*time.Second)
	opsAddr := cfg.String("OPS_ADDR", defaultOpsAddr)
	if err := cfg.Err(); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := observability.NewLogger(stdout, level)
	log.InfoContext(ctx, "configuration loaded", "config", cfg)

	// Opened in this order and closed in the reverse, once every loop has stopped: consumers' channels, the
	// publisher, the listener, the RabbitMQ connection, the database pool (ADR 0009).
	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	conn, err := rabbitmq.Connect(amqpURL, "worker")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	top := topology()
	if err := declare(conn, top); err != nil {
		return err
	}
	store := notification.NewStore(pool)
	listener, err := store.ListenOutbox(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close(context.WithoutCancel(ctx)) }()
	pub, err := rabbitmq.NewPublisher(conn)
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()
	client := delivery.NewClient(providerURL, providerTimeout)

	// The operations server: the metrics (ADR 0026), queue depths read when Prometheus scrapes, and the probes
	// (ADR 0027). A lost RabbitMQ connection also stops the worker; until it does, the worker is not ready.
	reg := observability.NewRegistry()
	deliveryMetrics, dispatchMetrics := delivery.NewMetrics(reg), dispatch.NewMetrics(reg)
	queues := []string{top.DeadLetter}
	for _, q := range top.Queues {
		queues = append(queues, q.Name)
	}
	reg.MustRegister(notification.NewWaitingCollector(store), rabbitmq.NewQueueDepthCollector(conn, queues))
	ops := http.NewServeMux()
	ops.Handle("GET /metrics", observability.MetricsHandler(reg))
	health.NewHandler(map[string]health.Check{
		"postgres": pool.Ping,
		"rabbitmq": func(context.Context) error {
			if conn.IsClosed() {
				return errRabbitMQClosed
			}
			return nil
		},
	}, log).RegisterRoutes(ops)
	opsListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", opsAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", opsAddr, err)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return httpx.Serve(gctx, httpx.NewServer(ops, log), opsListener, shutdownTimeout) })
	g.Go(func() error {
		return dispatch.NewRelay(store, listener, pub, dispatch.RelayConfig{
			Exchange: top.Exchange, BatchSize: relayBatch, PollInterval: pollInterval,
		}, dispatchMetrics, log).Run(gctx)
	})
	g.Go(func() error {
		return dispatch.NewScheduler(store, dispatch.SchedulerConfig{
			Interval: schedulerInterval, BatchSize: schedulerBatch, MaxAttempts: policy.MaxAttempts,
		}, dispatchMetrics, log).Run(gctx)
	})
	for _, q := range top.Queues {
		// Each consumer owns its channel: an AMQP channel must not be shared between goroutines (ADR 0009).
		ch, err := conn.Channel()
		if err != nil {
			return fmt.Errorf("open a channel for %s: %w", q.Name, err)
		}
		defer func() { _ = ch.Close() }()
		deliveries, err := rabbitmq.Consume(ch, q.Name, concurrency)
		if err != nil {
			return err
		}
		consumer := delivery.NewConsumer(store, client, delivery.ConsumerConfig{
			Channel: notification.Channel(q.RoutingKey), Concurrency: concurrency, RatePerSecond: float64(ratePerSecond),
			Lease: providerTimeout + leaseMargin, Policy: policy, ShutdownTimeout: shutdownTimeout,
			BreakerFailures: breakerFailures, BreakerOpen: breakerOpen,
		}, deliveryMetrics, log)
		g.Go(func() error { return consumer.Run(gctx, deliveries) })
	}
	// A lost connection stops the process; nothing reconnects in-process (ADR 0009).
	lost := conn.NotifyClose(make(chan *amqp.Error, 1))
	g.Go(func() error {
		select {
		case <-gctx.Done():
			return nil
		case err := <-lost:
			return fmt.Errorf("RabbitMQ connection lost: %w", err)
		}
	})

	log.InfoContext(ctx, "worker started", "queues", len(top.Queues), "concurrency", concurrency,
		"rate_per_second", ratePerSecond, "ops_addr", opsListener.Addr().String())
	if err := g.Wait(); err != nil {
		log.ErrorContext(ctx, "worker stopped by a failure", "error", err)
		return err
	}
	log.InfoContext(ctx, "stopped")
	return nil
}

// declare declares the topology on a channel of its own, closed afterwards.
func declare(conn *amqp.Connection, top rabbitmq.Topology) error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open a channel to declare the topology: %w", err)
	}
	defer func() { _ = ch.Close() }()
	return rabbitmq.Declare(ch, top)
}
