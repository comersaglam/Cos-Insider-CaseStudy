# Event-driven notification system

[![CI](https://github.com/comersaglam/Cos-Insider-CaseStudy/actions/workflows/ci.yml/badge.svg)](https://github.com/comersaglam/Cos-Insider-CaseStudy/actions/workflows/ci.yml)

A Go service that accepts notifications through an HTTP API and delivers them over SMS, e-mail and push through an
external provider: queue workers, a per-channel rate limit, priorities, scheduling, templates, retries with backoff,
a circuit breaker, idempotency, status updates over WebSocket, metrics, structured logs and health probes. Written for the Insider One software engineer assessment;
the brief is restated in [docs/requirements.md](docs/requirements.md).

## Quick start

You need Docker with Compose v2 and `make`; Go 1.27.4 too, for the tests and for running from source.

```sh
make up      # build the image; start PostgreSQL, RabbitMQ, a stand-in provider, the migrations, the API and the worker; wait until healthy
make smoke   # create a notification, repeat it with its idempotency key, wait until the worker has sent it
```

Then open the API's documentation at <http://localhost:8080/docs>. `make down` stops everything and keeps the data;
`docker compose down --volumes` deletes it. Without `make`, `docker compose up --build --wait` does the same as
`make up`.

| Address | What |
|---|---|
| <http://localhost:8080> | the API |
| <http://localhost:8080/docs> | the API's contract, rendered by Swagger UI |
| <http://localhost:8080/v1/metrics> | the service's state as a person reads it |
| <http://localhost:8080/metrics>, <http://localhost:9090/metrics> | Prometheus metrics of the API and the worker |
| <http://localhost:15672> | RabbitMQ's management UI (`notifications` / `notifications`) |
| <http://localhost:8081/__admin/requests> | what the stand-in provider received |

## Running the tests

```sh
make test                                  # the whole suite, with the race detector
make test PKG=./internal/notification/...  # one package
make lint                                  # golangci-lint, including the architecture rules
make check                                 # everything CI runs: verify, lint, then test
```

CI runs the same on every push and pull request ([.github/workflows/ci.yml](.github/workflows/ci.yml)): the linters,
the whole suite with the race detector, and the image build. Pushing a `v*` tag also publishes the image to
`ghcr.io` ([ADR 0030](docs/adr/0030-continuous-integration-and-delivery.md)).

The tests need Docker, not the running stack: those that use PostgreSQL or RabbitMQ start their own containers
through testcontainers, each test on a database or virtual host of its own. No test reaches an external service;
the provider is an `httptest.Server`. The suite holds:

- **unit tests** of the domain rules, the retry policy, the circuit breaker and the HTTP handlers;
- **integration tests** against real PostgreSQL and RabbitMQ: the store's queries, claims under a lease, the outbox
  relay taking rows with `FOR UPDATE SKIP LOCKED`, publisher confirms, the consumer;
- **end-to-end tests** of both binaries: the API serving a real database, and the worker taking notifications
  through the outbox, RabbitMQ and a fake provider to `sent`, `retrying` and `failed`;
- **contract tests**: `api/openapi.yaml` is valid, its operations are exactly the routes the server registers, and
  every answer the tests above see has a status and a body the contract documents.

## Make targets

| Target | What it does |
|---|---|
| `make up` | build and start the whole service and wait until it is healthy |
| `make smoke` | create and read a notification through the running API, and wait until it is sent |
| `make down` | stop the stack, keeping its data |
| `make deps` | start only PostgreSQL and RabbitMQ, for running the API from source |
| `make migrate` | apply the database migrations (to the local stack by default) |
| `make api` | run the API from source against the local stack (`make deps` first) |
| `make test` | run the tests with the race detector; `PKG=` picks one package |
| `make lint` | run the linters |
| `make verify` | check that modules are tidy, the query code is generated, and no known vulnerability reaches the code |
| `make check` | verify, lint and test, as CI does |
| `make fmt` | format the code |
| `make build` | build both binaries into `bin/` |
| `make generate` | regenerate the typed query code from the SQL (sqlc) |
| `make help` | list the targets |

## Trying the API

The contract with every operation, its errors and examples is at <http://localhost:8080/docs>
([api/openapi.yaml](api/openapi.yaml)). A few calls against `make up`:

```sh
# Accept a notification. 202: it is stored as queued; a worker sends it.
curl -i -X POST localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: order-42-sms' -H 'X-Correlation-ID: req-7f3c9a' \
  -d '{"to":"+905551234567","channel":"sms","content":"Your order has shipped.","priority":"high"}'

# The same request again returns the same notification, marked Idempotent-Replayed: true.

# Schedule one, up to 30 days ahead: it waits as scheduled, then is sent; it can be cancelled until then.
curl -s -X POST localhost:8080/v1/notifications -H 'Content-Type: application/json' \
  -d '{"to":"+905551234567","channel":"sms","content":"Flash sale starts now.","scheduledAt":"2026-11-24T09:00:00+03:00"}'

# Read it: status, attempts, and once sent, the provider's message ID.
curl -s localhost:8080/v1/notifications/<id>

# Up to 1000 at once: valid items are accepted, invalid ones reported with their position and reasons.
curl -s -X POST localhost:8080/v1/batches -H 'Content-Type: application/json' -d '{"notifications":[
  {"to":"+905551234567","channel":"sms","content":"Flash sale: 40% off until midnight."},
  {"to":"05551234567","channel":"sms","content":"Flash sale: 40% off until midnight."},
  {"to":"ayse@example.com","channel":"email","content":"Flash sale: 40% off until midnight.","priority":"high"}]}'

# A template, then a notification rendered from it: only text and {{.name}} placeholders are allowed.
curl -s -X POST localhost:8080/v1/templates -H 'Content-Type: application/json' \
  -d '{"name":"order-shipped","channel":"sms","body":"Hi {{.name}}, your order {{.order}} has shipped."}'
curl -s -X POST localhost:8080/v1/notifications -H 'Content-Type: application/json' \
  -d '{"to":"+905551234567","templateId":"<template id>","variables":{"name":"Ayşe","order":"42"}}'

# A batch's progress: its notifications counted by status.
curl -s localhost:8080/v1/batches/<batch id>

# List, newest first: filters combine; pass nextCursor as cursor for the next page.
curl -s 'localhost:8080/v1/notifications?status=failed&channel=sms&limit=20'

# Cancel one still waiting; send a failed one again; send a window of failures again, counting first.
curl -s -X POST localhost:8080/v1/notifications/<id>/cancel
curl -s -X POST localhost:8080/v1/notifications/<id>/retry
curl -s -X POST localhost:8080/v1/notifications/retry -H 'Content-Type: application/json' \
  -d '{"channel":"sms","failedFrom":"2026-11-23T10:00:00Z","failedTo":"2026-11-23T11:00:00Z","dryRun":true}'

# Waiting work, success rate and provider latency by channel.
curl -s localhost:8080/v1/metrics

# Follow a batch's status changes as they happen, over a WebSocket (any WebSocket client; websocat here).
websocat "ws://localhost:8080/v1/status-updates?batchId=<batch id>"
```

The stand-in provider answers `+905550000503` with `503`, which shows retries and the circuit breaker, and
`+905550000422` with `422`, a permanent failure. Every other recipient is accepted with `202`.

### Sending to webhook.site

The brief's provider is a webhook.site URL. Create one at <https://webhook.site>, set its default response to status
`202` with a JSON body such as `{"messageId": "...", "status": "accepted"}`, then:

```sh
cp .env.example .env    # git ignores .env
# set PROVIDER_URL=https://webhook.site/<your uuid> in .env
make up
```

Treat the URL as a secret: anyone who has it can read what is sent to it. The service logs it as `***`.

## How it works

Two processes run from one image: **`api`** accepts and reads notifications, and **`worker`** delivers them.

1. The API validates a notification and, in one transaction, stores it in PostgreSQL as `queued` and writes an
   outbox row. PostgreSQL is the source of truth: a notification is never lost between the database and the
   queue (the transactional outbox, [ADR 0003](docs/adr/0003-postgresql-and-rabbitmq-with-outbox.md)).
2. The worker's **relay**, woken by `LISTEN/NOTIFY`, publishes outbox rows to RabbitMQ with publisher confirms and
   deletes them. A direct exchange routes each message to its channel's queue, which orders it by priority
   ([ADR 0012](docs/adr/0012-message-flow-and-delivery-guarantee.md), [0020](docs/adr/0020-rabbitmq-platform.md),
   [0021](docs/adr/0021-outbox-relay-and-scheduler.md)).
3. Each channel's queue has a **single active consumer**, which paces sends at 100 a second and runs up to 32 at
   once ([ADR 0005](docs/adr/0005-rate-limit-with-single-active-consumer.md),
   [0009](docs/adr/0009-concurrency-model.md), [0023](docs/adr/0023-the-consumer.md)). For each message it
   **claims** the notification under a lease, so two workers never send it at the same time, and asks the channel's
   **circuit breaker** before sending ([ADR 0024](docs/adr/0024-circuit-breaker.md)).
4. The **provider client** sends `{to, channel, content}` with the notification's ID as `Idempotency-Key`. Its
   answer decides: `sent`; `failed` on a refusal; or `retrying` on a timeout, 408, 429 or 5xx, after an exponential
   delay with full jitter, up to 8 attempts ([ADR 0007](docs/adr/0007-delivery-and-retry.md),
   [0022](docs/adr/0022-provider-client.md)). Every attempt is recorded.
5. A retry waits in PostgreSQL, not in RabbitMQ. The **scheduler** queues retries when they are due and reaps the
   claims of workers that died mid-send ([ADR 0006](docs/adr/0006-delayed-work-waits-in-postgresql.md)). A
   message that cannot be read goes to a dead-letter queue ([ADR 0014](docs/adr/0014-dead-letters-and-replay.md)).

Delivery is **at least once**: a worker that dies during a send cannot know whether the provider received it, so
the notification is sent again, and the provider can drop the repeat by its `Idempotency-Key`. The code is a
modular monolith: packages by feature under `internal/`, infrastructure under `internal/platform`, and one router
([ADR 0008](docs/adr/0008-modular-monolith-layout.md)).

## How the brief is met

| Requirement | How |
|---|---|
| Create, batch (≤1000), read, cancel, list with filters and pagination | `/v1/notifications`, `/v1/batches`; a batch accepts its valid items and reports the rest; keyset pagination |
| Asynchronous processing through queue workers | transactional outbox → RabbitMQ → a consumer per channel |
| 100 messages per second per channel | a single active consumer per channel queue with a rate limiter |
| Priority queues | RabbitMQ priority queues; high before normal before low within a channel |
| Content validation | per-channel recipient format and length limits, every invalid field reported |
| Scheduled notifications (bonus) | `scheduledAt`, up to 30 days ahead; waits in PostgreSQL and is queued when due |
| Templates (bonus) | `/v1/templates` with `{{.name}}` placeholders, rendered when a notification is created |
| Real-time status updates (bonus) | `/v1/status-updates` over WebSocket, fed by a PostgreSQL trigger through `LISTEN/NOTIFY` |
| Idempotency | `Idempotency-Key` on create and batch; the notification's ID sent to the provider; claims under a lease |
| Retries | exponential backoff with full jitter, `Retry-After` honoured, a circuit breaker per channel, single and bulk retry by hand |
| Real-time metrics | `/v1/metrics` (queue depth, success rate, latency percentiles) and Prometheus `/metrics` on both processes |
| Structured logs with correlation IDs | JSON logs; `X-Correlation-ID` follows a notification from the request to the worker and the provider |
| Health check | `/healthz` and `/readyz` on both processes; Compose waits on them |
| One-command setup, migrations, API docs, one-command tests | `make up`, `migrations/`, `/docs`, `make test` |

The full list, with the block and the decision behind each, is [docs/requirements.md](docs/requirements.md).

## Observability

- **Logs**: JSON through `log/slog`, in UTC, each line with its `correlation_id`. Recipients are masked and message
  content is never logged; secrets are printed as `***`.
- **Metrics**: the API counts requests by route and status and times them; the worker counts attempts by channel and
  outcome, times the provider, and reports breaker states, sends in flight and queue depths in PostgreSQL and
  RabbitMQ, the dead-letter queue included ([ADR 0026](docs/adr/0026-metrics.md)).
- **Probes**: `/healthz` says the process runs; `/readyz` says its dependencies answer
  ([ADR 0027](docs/adr/0027-health-probes.md)).

## Configuration

Both processes read environment variables; Compose sets them, and `.env` overrides its defaults
([.env.example](.env.example)). The main ones:

| Variable | Process | Default | Meaning |
|---|---|---|---|
| `DATABASE_URL` | both | required | PostgreSQL URL |
| `AMQP_URL` | worker | required | RabbitMQ URL |
| `PROVIDER_URL` | worker | required | the provider's endpoint, such as a webhook.site URL |
| `RATE_PER_SECOND` | worker | `100` | sends started per second per channel |
| `SEND_CONCURRENCY` | worker | `32` | sends in flight per channel |
| `MAX_ATTEMPTS` | worker | `8` | attempts before a notification fails |
| `RETRY_BASE`, `RETRY_CAP` | worker | `10s`, `15m` | the first retry's ceiling, doubled each attempt, and the highest |
| `BREAKER_FAILURES`, `BREAKER_OPEN` | worker | `5`, `30s` | failures in a row that open a channel's breaker, and how long it stays open |
| `HTTP_ADDR`, `OPS_ADDR` | api, worker | `:8080`, `:9090` | where each process listens |
| `LOG_LEVEL` | both | `info` | `debug`, `info`, `warn` or `error` |

Every setting is listed in the package comments of [cmd/api](cmd/api/main.go) and [cmd/worker](cmd/worker/main.go).

## Project layout

```
api/                  the OpenAPI contract, embedded; api/contract checks answers against it in tests
cmd/api, cmd/worker   the two composition roots
internal/notification notifications and their templates: the domain, use cases, HTTP handlers and storage
internal/dispatch     the outbox relay and the scheduler
internal/delivery     the consumer, the provider client, retries and the circuit breaker
internal/health       liveness and readiness
internal/httpapi      the router: the middleware chain and the one place modules are mounted
internal/platform     infrastructure: config, postgres, rabbitmq, httpx, observability, testenv
migrations/           versioned SQL migrations, embedded in the binaries
deploy/, compose.yaml, Dockerfile   the local stack and the image
docs/                 requirements, decisions (ADRs), the roadmap, the progression log, the development guide
```

## Decisions and history

Every significant choice is an architecture decision record in [docs/adr](docs/adr/README.md), with what was
chosen, what was left out, and why. [docs/progression](docs/progression/README.md) logs each block of work with its
verification and commits, and [docs/roadmap.md](docs/roadmap.md) holds the plan. How to work on the code is in
[docs/development.md](docs/development.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## Not done, on purpose

- **Authentication**: the brief does not ask for it; it would sit in a gateway in front, or in one middleware.
- **Exactly-once delivery**: not possible with a provider that is called over HTTP; delivery is at least once, and
  the provider receives an idempotency key to drop repeats.
- **Distributed tracing**, the brief's last optional bonus, is planned in the [roadmap](docs/roadmap.md).
