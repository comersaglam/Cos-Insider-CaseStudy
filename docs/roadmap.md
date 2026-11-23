# Implementation roadmap

The planned blocks from the first line of code to delivery. Each block is written tests first, ends with
`make check` green, its [progression](progression/README.md) file and one commit. Details (field names, limits,
endpoint shapes) are settled when a block starts; *Open question* marks what is decided then.

## Phase 1: foundation and the synchronous API

### 09. Local environment
- **Scope**: `compose.yaml` at the root with PostgreSQL and RabbitMQ (management UI), pinned image tags, health
  checks, ports bound to `127.0.0.1`; `.env.example`; `make up`, `down`
  ([ADR 0010](adr/0010-local-environment-with-docker-compose.md)).
- **Tests**: `TestComposeRunsTheTestImages` keeps the stack's images equal to the ones tests start
  (`internal/platform/testenv`); `make up` reports both services healthy.

### 10. Notification domain
- **Scope**: `Channel` (sms, email, push), `Priority` (high, normal, low), `Status` (queued, scheduled, sending,
  retrying, sent, failed, cancelled), the allowed status transitions, content validation per channel.
- **Tests**: table-driven: every allowed and refused transition, each channel's boundaries (limit, limit + 1),
  recipient formats.
- **Watch**: enums are string constants, readable in JSON and SQL; unknown values are refused when parsed.
- **Decided** ([ADR 0011](adr/0011-notification-domain.md)): the provider contract is fixed, so there is no e-mail
  subject; SMS content up to 1600 characters, counted in characters; recipients checked by shape with the
  standard library (E.164, one bare e-mail address, an opaque push token), never normalised.

### 11. Message flow and delivery guarantee
- **Scope**: the messaging pattern and exchange, the delivery guarantee and what enforces it, the claim's lease,
  the reaper and poison messages ([ADR 0012](adr/0012-message-flow-and-delivery-guarantee.md)). A design block:
  no code.

### 12. Platform: config, observability, httpx
- **Scope**: `Config` from the environment; `slog` JSON logger; correlation ID taken from the request or generated,
  carried in `context` and on every log line; middleware (recover, correlation, access log); RFC 9457 error writer;
  JSON decoding with a size limit and unknown fields refused.
- **Tests**: middleware through `httptest`; a panic becomes a 500 problem response; the correlation ID appears in the
  response and the log; recipients are masked in logs.
- **Watch**: one `slog.Handler` wrapper adds context fields; masking lives in one place.

### 13. Database and store
- **Scope**: migrations (`notifications` with `attempts` and `claimed_until`, `batches`, `outbox`,
  `delivery_attempts`, the idempotency index);
  `platform/postgres` (pool, transaction helper, migration runner); `sqlc.yaml`; the testcontainers helper in
  `platform/testenv`, starting `testenv.PostgresImage`; the notification store.
- **Tests**: migrations up, down, up; every store query against real PostgreSQL.
- **Watch**: indexes follow the queries (list filters, the scheduler's due query and the relay's unpublished query
  as partial indexes); `timestamptz`; `NOT NULL` matches the domain.
- **Open question**: the idempotency key's scope and lifetime; the same key with a different body (422?).

### 14. Create, get, and a running API
- **Scope**: `POST /v1/notifications`, `GET /v1/notifications/{id}`; service; handlers; the `httpapi` router; the
  `cmd/api` composition root; graceful shutdown; notification and outbox rows in one transaction.
- **Tests**: handlers with a fake service; the service with a fake store; end to end with a real database; two
  requests with one `Idempotency-Key` make one notification; a request in progress completes during shutdown.
- **Watch**: server timeouts; `202 Accepted` rather than `201`, since the notification is accepted, not sent.

### 15. Batch create and batch status
- **Scope**: `POST /v1/notifications/batch` (up to 1000), `GET /v1/batches/{id}` with counts per status.
- **Tests**: 1000 accepted, 1001 refused; behaviour with invalid items; all rows in one transaction.
- **Watch**: `COPY` through pgx `CopyFrom` for notifications and outbox rows.
- **Open question**: one invalid item refuses the whole batch (atomic), or valid items are accepted and invalid
  ones reported (partial).

### 16. List, cancel and retry
- **Scope**: `GET /v1/notifications` with status, channel, date range and batch filters and keyset cursors;
  `POST /v1/notifications/{id}/cancel` (only while waiting, otherwise 409);
  `POST /v1/notifications/{id}/retry` (only when `failed`).
- **Tests**: filter combinations; no row lost or repeated across pages when timestamps tie; cancel racing a send.
- **Watch**: cancel is one conditional `UPDATE`, so the database settles the race; cursors are opaque.

### 17. One-command stack
- **Scope**: multi-stage Dockerfile at the root (one image, two binaries, non-root); `migrate`, `api` and `worker`
  services added to `compose.yaml` next to the dependencies from block 09; `depends_on` conditions on health;
  `make migrate`.
- **Tests**: health endpoints after `docker compose up`; a smoke script that creates a notification and reads it.
- **Watch**: image size; the webhook.site URL comes from the environment.

## Phase 2: the processing engine

### 18. Platform: RabbitMQ
- **Scope**: connection; topology (the direct exchange `notifications`, one durable queue per channel bound by its
  name, with `x-max-priority`, single active consumer and a dead-letter exchange); publishing with confirms
  ([ADR 0012](adr/0012-message-flow-and-delivery-guarantee.md)).
- **Tests** (RabbitMQ through testcontainers): topology declaration is idempotent; an unconfirmed publish fails;
  priority order; a second consumer receives nothing under single active consumer.

### 19. Dispatch: outbox relay and scheduler
- **Scope**: the relay takes unpublished outbox rows with `SKIP LOCKED`, publishes and marks them after the confirm,
  woken by `LISTEN/NOTIFY` with a ticker as fallback; the scheduler moves due `scheduled` and `retrying` rows to the
  outbox, and its reaper moves `sending` rows with an expired lease to `retrying` or `failed`.
- **Tests**: two relays in parallel publish each row once; with RabbitMQ down a row waits and goes out later; the
  scheduler leaves rows that are not yet due; the reaper takes only expired leases and records the attempt as
  `unknown`.
- **Watch**: batch size; publication in priority order; the relay is at least once (a crash after the confirm
  republishes, which consumer idempotency absorbs).

### 20. Delivery: provider client
- **Scope**: the webhook.site client (`to`, `channel`, `content`), timeout, `Idempotency-Key` and correlation
  headers, classification of outcomes ([ADR 0007](adr/0007-delivery-and-retry.md)), `Retry-After`.
- **Tests**: an `httptest.Server` for every status code, a timeout, malformed JSON, an oversized body.

### 21. Delivery: consumer, rate limit, sending and retry
- **Scope**: per-channel consumer under single active consumer; `x/time/rate` limiter; one goroutine per message
  bounded by a semaphore of `SEND_CONCURRENCY` ([ADR 0009](adr/0009-concurrency-model.md)); the claim before sending
  with a lease, spending an attempt (ADR 0012); outcome as an attempt row, a status update and an acknowledgement
  sent by the consumer; retries with full jitter; unreadable messages to the dead-letter queue; a panic in a sender
  recovered as a failed attempt.
- **Tests**: 500 messages never exceed about 100 per second; a cancelled message is not sent; two copies of one
  message in flight at once send once; an expired lease is claimed again and counted as a possible duplicate;
  a message that panics ends in `failed` after its attempts; backoff bounds; the deadlock guard (a full semaphore
  with pending results); `-race`.

### 22. Circuit breaker and pausing the pull
- **Scope**: `sony/gobreaker` per channel; while open the consumer takes no deliveries; half-open probe.
- **Tests**: five 503s open the breaker, no request is sent, no attempt is spent; when the provider recovers the flow
  resumes.

### 23. Worker composition root and an end-to-end test
- **Scope**: `cmd/worker` starts every goroutine under one `errgroup`, shuts down in order, and serves `/metrics` and
  `/healthz`.
- **Tests**: end to end: API → PostgreSQL → relay → RabbitMQ → consumer → fake provider → `sent`; shutdown with
  messages in flight loses none.

## Phase 3: observability

### 24. Metrics
- **Scope**: HTTP request counts and latency histograms; sends per channel and outcome; provider latency; queue
  depth (waiting rows per status in PostgreSQL, ready messages in RabbitMQ) collected on scrape; breaker state;
  rate-limiter wait.
- **Watch**: label cardinality (a notification ID is never a label).
- **Open question**: Prometheus `/metrics` alone, or also a JSON summary endpoint for people; Grafana in Compose.

### 25. Health
- `/healthz` (the process runs) and `/readyz` (PostgreSQL and RabbitMQ answer); Compose health checks use them.

### 26. Correlation across the queue
- The request's correlation ID is stored on the notification, carried in an AMQP header, logged by the worker and
  sent to the provider.
- **Test**: one ID links the API's and the worker's log lines.

## Phase 4: deliverables

### 27. OpenAPI and Swagger UI
- `api/openapi.yaml` (3.1) served with Swagger UI at `/docs`; a test checks every registered route is in the spec
  and the spec is valid.
- **Open question**: written by hand (design first) or generated from code.

### 28. README
- Setup, architecture diagram (Mermaid), curl examples, a summary of the decisions linking the ADRs, and what is
  deliberately out of scope.

### 29. CI (GitHub Actions)
- Lint, `test -race`, `govulncheck`, a check that generated `sqlc` code is current, Docker build.

## Phase 5: bonus features

| # | Bonus | Note |
|---|---|---|
| 30 | Scheduled notifications | The mechanism exists ([ADR 0006](adr/0006-delayed-work-waits-in-postgresql.md)); a `scheduledAt` field, its validation and cancellation. |
| 31 | Templates | `text/template`; a templates table with CRUD; a missing variable is an error. |
| 32 | WebSocket status updates | `LISTEN/NOTIFY` → hub → clients; the library is chosen in that block. |
| 33 | Distributed tracing | OpenTelemetry propagated over HTTP and AMQP headers; Jaeger in Compose. |
| 34 | Load and burst test | A script and results in the README showing the rate limit and priorities at work. |

Failure handling is covered by retries, the circuit breaker, the dead-letter queue and the retry endpoint (blocks
16, 21, 22); CI/CD by block 29.

## Phase 6: closing

### 35. Final review
- An independent code review, the README tried on a clean machine, the commit history checked, progression and
  ADRs current.

## After the first version

Recorded so the first version stays small; each would get its own ADR.

- **Adaptive concurrency** for senders, following provider latency within bounds (AIMD or a gradient limiter),
  as described in [ADR 0009](adr/0009-concurrency-model.md).
- **Horizontal scaling**: the API scales out behind a load balancer (for example ECS or EKS with autoscaling);
  raising per-channel throughput beyond one consumer needs sharded queues or a distributed limiter, superseding
  [ADR 0005](adr/0005-rate-limit-with-single-active-consumer.md).
- **Per-channel retry policies** and **message expiry** (`expiresAt`), from [ADR 0007](adr/0007-delivery-and-retry.md).
- **Quorum queues** for a multi-node RabbitMQ cluster, from [ADR 0003](adr/0003-postgresql-and-rabbitmq-with-outbox.md).
