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
- **Decided** ([ADR 0013](adr/0013-platform-conventions.md)): configuration with the standard library, every
  problem reported at once; `X-Correlation-ID`; problem `errors` located by JSON Pointer; `encoding/json/v2`.

### 13. Dead letters and replay
- **Scope**: bulk replay by filter, and marking a notification `failed` when its message cannot be processed
  ([ADR 0014](adr/0014-dead-letters-and-replay.md)); the transition `queued` → `failed` in the domain.
- **Tests**: `TestStatusCanTransitionTo` gains the new transition.

### 14. Database and store
- **Scope**: the first migration (`notifications`, `outbox`, the idempotency index); `platform/postgres` (pool,
  migration runner); `sqlc.yaml` and `make generate`; the testcontainers helper in `platform/testenv`, giving each
  test a copy of a migrated template database; the notification store (create with idempotency, get).
- **Tests**: migrations rolled back and reapplied; every store query against real PostgreSQL; twenty concurrent
  requests with one key create one notification.
- **Decided** ([ADR 0015](adr/0015-storage-ids-and-idempotency.md)): UUID v7 made in Go; keys without expiry,
  global; a reused key with another request is `422`; enumerations as `text` with `CHECK`; the column `recipient`;
  each later block migrates the tables and columns it uses.

### 15. Create, get, and a running API
- **Scope**: `POST /v1/notifications`, `GET /v1/notifications/{id}`; service; handlers; the `httpapi` router; the
  `cmd/api` composition root; graceful shutdown; notification and outbox rows in one transaction.
- **Tests**: handlers with a fake service; the service with a fake store; end to end with a real database; two
  requests with one `Idempotency-Key` make one notification; a request in progress completes during shutdown.
- **Watch**: server timeouts; `202 Accepted` rather than `201`, since the notification is accepted, not sent.
- **Decided** ([ADR 0016](adr/0016-http-api-and-the-api-process.md)): `202` with `Location` and
  `Idempotent-Replayed`; migrations as a separate `api migrate` step; `api/openapi.yaml` by hand, from this block on.

### 16. Batch create and batch status
- **Scope**: `POST /v1/batches` (up to 1000), `GET /v1/batches/{id}` with counts per status and the rejected items.
- **Tests**: 1000 accepted, 1001 refused; invalid items reported while the valid ones are accepted; nothing stored
  when nothing is valid; a repeated batch returns the first; all rows in one transaction.
- **Watch**: `COPY` through pgx `CopyFrom` for notifications and outbox rows.
- **Migration**: `batches` and `notifications.batch_id`, `batch_index`.
- **Decided** ([ADR 0017](adr/0017-batches.md)): partial acceptance; rejections kept as positions and reasons,
  never content; one key per batch; a 10 MiB body limit set by `BATCH_MAX_BODY_BYTES`.

### 17. List, cancel and retry
- **Scope**: `GET /v1/notifications` with status, channel, date range and batch filters and keyset cursors;
  `POST /v1/notifications/{id}/cancel` (only while waiting, otherwise 409);
  `POST /v1/notifications/{id}/retry` (only when `failed`); `POST /v1/notifications/retry`, the bulk replay by
  channel, batch and a required time window, with `dryRun` and a cap per request ([ADR 0014](adr/0014-dead-letters-and-replay.md)).
- **Tests**: filter combinations; no row lost or repeated across pages when timestamps tie; cancel racing a send;
  bulk replay requeues only `failed` rows inside the window, `dryRun` changes nothing, the cap holds.
- **Migration**: the `(created_at, id)` index the list pages over, and `failed_at`.
- **Decided** ([ADR 0018](adr/0018-listing-cancelling-and-retrying.md)): newest first, 50 per page up to 100,
  unknown parameters refused; cancel repeatable, `409` once sending; bulk retry capped at 10,000 per request.

### 18. One-command stack
- **Scope**: multi-stage Dockerfile at the root (one image, two binaries, non-root); `migrate` and `api`
  services added to `compose.yaml` next to the dependencies from block 09; `api` waits for `migrate` to complete;
  `make up`, `make deps`, `make smoke`.
- **Tests**: `docker compose up` brings the API up; `scripts/smoke.sh` creates a notification, reads it and repeats
  it; `docker compose stop` stops it cleanly.
- **Decided** ([ADR 0019](adr/0019-container-image-and-the-one-command-stack.md)): distroless `static` `nonroot`,
  pinned by digest, 23.5 MB; the worker service joins in block 24 and the API's health check in block 26.

## Phase 2: the processing engine

### 19. Platform: RabbitMQ
- **Scope**: connection; topology (the direct exchange `notifications`, one durable queue per channel bound by its
  name, with `x-max-priority`, single active consumer and a dead-letter exchange); publishing with confirms
  ([ADR 0012](adr/0012-message-flow-and-delivery-guarantee.md)).
- **Tests** (RabbitMQ through testcontainers): topology declaration is idempotent; an unconfirmed publish fails;
  priority order; a second consumer receives nothing under single active consumer.
- **Decided** ([ADR 0020](adr/0020-rabbitmq-platform.md)): the platform declares a topology it is given; mandatory
  publishes so an unroutable message fails instead of vanishing; batches of 256 confirmed before the next;
  amqp091-go v1.17 and the testcontainers RabbitMQ module.

### 20. Dispatch: outbox relay and scheduler
- **Scope**: the relay takes outbox rows with `SKIP LOCKED`, publishes them and deletes them after the confirm,
  woken by `LISTEN/NOTIFY` with a slow poll as fallback; the scheduler moves due `scheduled` and `retrying` rows to
  the outbox.
- **Tests**: two relays in parallel publish each row once; with RabbitMQ down the relay stops and the row waits,
  then goes out; the relay is woken by the trigger; the scheduler leaves rows that are not yet due.
- **Migration**: `notifications.next_attempt_at` with its partial index; the outbox's `NOTIFY` trigger; the
  outbox loses `published_at`.
- **Decided** ([ADR 0021](adr/0021-outbox-relay-and-scheduler.md)): the notification module owns the SQL, dispatch
  the loops; published rows are deleted; failures stop the loop (crash-only). The reaper moved to block 22, beside
  the lease it serves.

### 21. Delivery: provider client
- **Scope**: the webhook.site client (`to`, `channel`, `content`), timeout, `Idempotency-Key` and correlation
  headers, classification of outcomes ([ADR 0007](adr/0007-delivery-and-retry.md)), `Retry-After`.
- **Tests**: an `httptest.Server` for every status code, a timeout, malformed JSON, an oversized body.
- **Decided** ([ADR 0022](adr/0022-provider-client.md)): every 2xx succeeds, other 4xx and redirects are
  permanent, an unreadable acceptance is still a success, answers are read up to 64 KB, 64 idle connections.

### 22. Delivery: consumer, rate limit, sending and retry
- **Scope**: per-channel consumer under single active consumer; `x/time/rate` limiter; one goroutine per message
  bounded by a semaphore of `SEND_CONCURRENCY` ([ADR 0009](adr/0009-concurrency-model.md)); the claim before sending
  with a lease, spending an attempt (ADR 0012); outcome as an attempt row, a status update and an acknowledgement
  sent by the consumer; retries with full jitter; unreadable messages to the dead-letter queue, their notification
  marked `failed` (`unprocessable_message`) when its ID can be read; a panic in a sender
  recovered as a failed attempt.
- **Tests**: 500 messages never exceed about 100 per second; a cancelled message is not sent; two copies of one
  message in flight at once send once; an expired lease is claimed again and counted as a possible duplicate;
  a message that panics ends in `failed` after its attempts; backoff bounds; the deadlock guard (a full semaphore
  with pending results); `-race`.
- **Reaper** (moved here from block 20): the scheduler also returns `sending` rows with an expired lease to
  `retrying` or `failed`, recording the attempt as `unknown` ([ADR 0012](adr/0012-message-flow-and-delivery-guarantee.md)).
- **Migration**: `attempts`, `claimed_until`, `failure_reason`, the provider's message ID, and `delivery_attempts`.
  Single and bulk retry then reset `attempts` as they requeue.

### 23. Circuit breaker and pausing the pull
- **Scope**: `sony/gobreaker` per channel; while open the consumer takes no deliveries; half-open probe.
- **Tests**: five 503s open the breaker, no request is sent, no attempt is spent; when the provider recovers the flow
  resumes.

### 24. Worker composition root and an end-to-end test
- **Scope**: `cmd/worker` starts every goroutine under one `errgroup`, shuts down in order, and serves `/metrics` and
  `/healthz`.
- **Tests**: end to end: API → PostgreSQL → relay → RabbitMQ → consumer → fake provider → `sent`; shutdown with
  messages in flight loses none.
- **Compose**: the `worker` service joins `compose.yaml`, running the same image, after `migrate`, with
  `restart: unless-stopped` (ADR 0009, 0019).

## Phase 3: observability

### 25. Metrics
- **Scope**: HTTP request counts and latency histograms; sends per channel and outcome; provider latency; queue
  depth (waiting rows per status in PostgreSQL, ready messages in RabbitMQ, the dead-letter queue's depth)
  collected on scrape; breaker state;
  rate-limiter wait.
- **Watch**: label cardinality (a notification ID is never a label).
- **Open question**: Prometheus `/metrics` alone, or also a JSON summary endpoint for people; Grafana in Compose.

### 26. Health
- `/healthz` (the process runs) and `/readyz` (PostgreSQL and RabbitMQ answer); Compose health checks use them.

### 27. Correlation across the queue
- The request's correlation ID is stored on the notification, carried in an AMQP header, logged by the worker and
  sent to the provider.
- **Test**: one ID links the API's and the worker's log lines.

## Phase 4: deliverables

### 28. OpenAPI and Swagger UI
- `api/openapi.yaml` (3.1), written by hand since block 15 ([ADR 0016](adr/0016-http-api-and-the-api-process.md)),
  served with Swagger UI at `/docs`; a test checks every registered route is in the spec and the spec is valid,
  with a pinned validator.

### 29. README
- Setup, architecture diagram (Mermaid), curl examples, a summary of the decisions linking the ADRs, and what is
  deliberately out of scope.

### 30. CI (GitHub Actions)
- Lint, `test -race`, `govulncheck`, a check that generated `sqlc` code is current, Docker build.

## Phase 5: bonus features

| # | Bonus | Note |
|---|---|---|
| 31 | Scheduled notifications | The mechanism exists ([ADR 0006](adr/0006-delayed-work-waits-in-postgresql.md)); a `scheduledAt` field, its validation and cancellation. |
| 32 | Templates | `text/template`; a templates table with CRUD; a missing variable is an error. |
| 33 | WebSocket status updates | `LISTEN/NOTIFY` → hub → clients; the library is chosen in that block. |
| 34 | Distributed tracing | OpenTelemetry propagated over HTTP and AMQP headers; Jaeger in Compose. |
| 35 | Load and burst test | A script and results in the README showing the rate limit and priorities at work. |

Failure handling is covered by retries, the circuit breaker, the dead-letter queue and the retry endpoint (blocks
13, 17, 22, 23); CI/CD by block 30.

## Phase 6: closing

### 36. Final review
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
- **Admission control**: answering `429` or `503` when the backlog passes a threshold. In the first version the
  queue and PostgreSQL absorb bursts, which is what the asynchronous design is for; backpressure inside the
  system comes from the prefetch count, the semaphore, the rate limiter, the breaker and RabbitMQ's publisher
  blocking ([ADR 0012](adr/0012-message-flow-and-delivery-guarantee.md)).
- **Reconciling lost messages**: a command that republishes `queued` notifications whose messages a single
  RabbitMQ node lost with its disk ([ADR 0014](adr/0014-dead-letters-and-replay.md)).
- **Quorum queues** for a multi-node RabbitMQ cluster, from [ADR 0003](adr/0003-postgresql-and-rabbitmq-with-outbox.md).
