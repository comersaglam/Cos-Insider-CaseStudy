# 25. Metrics

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0026](../adr/0026-metrics.md)

## Goal

Answer the brief's "real-time metrics endpoint (queue depth, success and failure rates, latency)" for both
Prometheus and a person with `curl`.

## Steps

1. The owner chose Prometheus `/metrics` plus a JSON summary, and approved `prometheus/client_golang` v1.25.0.
2. `observability.NewRegistry` and `MetricsHandler`; `httpx.HTTPMetrics`, tests first: counts by method, route
   (the pattern, never the path) and status, an unmatched route and an unknown method folded into bounded values.
   The router mounts `/metrics` and the middleware right inside the correlation middleware, where it sees the mux's
   pattern and a recovered panic's 500.
3. `delivery.Metrics` and `dispatch.Metrics`, tests first: attempts by outcome, provider latency, breaker state,
   limiter wait, sends in flight, takeovers, dead-lettered; published, queued, reaped. Every test consumer, relay and
   scheduler now takes its metrics.
4. Collectors read on scrape, tests first against PostgreSQL and RabbitMQ: `notification.WaitingCollector` (every
   channel and waiting status, zero included, and the outbox's backlog) and `rabbitmq.QueueDepthCollector`. A source
   that does not answer is a scrape error.
5. The worker serves `/metrics` on an operations server (`OPS_ADDR`) under its `errgroup`; its end-to-end test reads
   the operations address from the log and checks the series.
6. `migrations/0006_metrics` (two indexes), `Store.Summary` (percentiles by `percentile_cont`, tested against a
   computed p50, p95 and p99) and `GET /v1/metrics`, null where no attempt stands behind a rate or a latency. Added to
   `api/openapi.yaml`; Compose publishes the worker's port 9090.
7. The owner asked, for the end: Grafana, load tests at several scales and seeds shown in it with screenshots in the
   README, the README led by the make targets, a D2 architecture diagram after research, and a detailed OpenAPI. They
   are in the roadmap's blocks 28, 29 and 35.
8. Recorded ADR 0026.

## Verification

Package by package with `-race`: `observability`, `httpx`, `httpapi`, `notification`, `delivery`, `dispatch`,
`rabbitmq`, `cmd/api` and `cmd/worker` green, the last two checking `/metrics` and `/v1/metrics` end to end.
`golangci-lint`: 0 issues. `api/openapi.yaml` parses with nine operations and no unresolved reference.

## Follow-ups

Block 26, health: `/healthz` and `/readyz` on both processes, and Compose health checks that use them.
