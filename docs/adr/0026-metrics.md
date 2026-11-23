# 0026. Metrics: Prometheus for machines, a summary for people

- Status: Accepted
- Date: 2026-11-23

## Context

The brief asks for a real-time metrics endpoint: queue depth, success and failure rates, latency. Prometheus's
`/metrics` is the industry's format, but it holds counters and histogram buckets; a rate or a percentile is
computed by a PromQL query, so a person reading it with `curl` sees raw counts. The owner chose both:

| Option | Read by a person with curl | Industry standard | Cost |
|---|---|---|---|
| `/metrics` only | raw counters | yes | lowest |
| **`/metrics` and a JSON summary** | the brief's numbers, ready to read | the standard plus a convenience | one endpoint, three queries |
| Both, with Prometheus and Grafana in Compose | the same, and a dashboard | a full stack | two containers and a dashboard; planned for the load-test block |

## Decision

- **`prometheus/client_golang`**, each process on a registry of its own (`observability.NewRegistry`, with the Go
  runtime's and the process's metrics), every series under the `notifications_` namespace.
- **The API** serves `/metrics` beside its routes: `http_requests_total{method, route, status}` and
  `http_request_duration_seconds{method, route}`. The route is the matched pattern (`/v1/notifications/{id}`), never
  the path; an unmatched request is `unmatched` and an unknown method `OTHER`, so labels take a bounded set of values
  and no ID ever becomes a time series.
- **The worker** serves `/metrics` on its operations server (`OPS_ADDR`, `:9090`), run under its `errgroup`:
  - `delivery_attempts_total{channel, outcome}`, `provider_request_duration_seconds{channel}`,
    `circuit_breaker_state{channel}` (0 closed, 1 half-open, 2 open), `rate_limiter_wait_seconds{channel}`,
    `sends_in_flight{channel}`, `claim_takeovers_total{channel}` (possible duplicates, ADR 0012),
    `dead_lettered_total{channel}`;
  - `outbox_published_total`, `due_queued_total`, `claims_reaped_total`;
  - read when scraped (ADR 0009): `waiting{channel, status}` and `outbox_pending` from PostgreSQL, and
    `rabbitmq_queue_messages{queue}` and `rabbitmq_queue_consumers{queue}` from RabbitMQ, the dead-letter queue
    included. A source that does not answer is reported as a scrape error, never as a stale value.
- **`GET /v1/metrics`** on the API is the summary for people, computed from the database so every replica answers the
  same: waiting notifications by channel and status, the last five minutes' attempts by outcome, the success rate,
  and the provider's latency at p50, p95 and p99 (`percentile_cont`). A rate or a latency with no attempt behind it
  is `null`, not 0. The window is measured by the database's clock.
- **Indexes** for the scrape and the summary: waiting notifications by channel and status (partial), attempts by
  time.

## Consequences

- Prometheus can scrape both processes as they are; the brief's numbers are one `curl` away.
- Queue depth costs nothing between scrapes and is never stale, and agrees across replicas.
- The summary's queries grow with the window's attempts, not the table: five minutes of 100 sends a second per channel
  is 90,000 rows read through an index.

### Deliberately not done

| Not done | Why not |
|---|---|
| The library's global registry | Tests make as many registries as they need; nothing registers behind our back. |
| A notification's ID, or a path, as a label | Each value is a series Prometheus keeps for ever. |
| Computing rates and percentiles in memory for the summary | Per process, lost on restart, and different on each replica; the database has every attempt. |
| OpenTelemetry metrics | The tracing bonus adopts OpenTelemetry; Prometheus's client is the standard for metrics on their own. |
