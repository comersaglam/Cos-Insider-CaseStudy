# Development

How to work on this service day to day. Decisions behind the tools are in [docs/adr](adr/README.md).

## Tools

Go (version pinned in `go.mod`), Docker, `golangci-lint`, `sqlc`, `govulncheck`, and VS Code with the official Go
extension (`gopls`, format on save).

## Inner loop

1. Write or change a test, run it alone and see it fail: `go test ./internal/<package>/... -run TestName`.
   While working, run only the package being changed (`make test PKG=./internal/<package>/...`); when a large
   block ends, run every package, one per command, so each result shows as it arrives.
2. Write the code until it passes.
3. Before committing, run the whole suite with the race detector: `make test`.
4. Run `make lint`; the linter's findings are fixed, not silenced. A false positive is the one exception: it is
   silenced on its own line with the reason (`//nolint:gosec // G404: jitter, not a secret`).

## Make targets

`make` alone lists the targets.

| Target | What it does |
|---|---|
| `make fmt` | `gofmt` and `goimports` over the module |
| `make lint` | `golangci-lint run`, including the architecture rules (`depguard`, [ADR 0008](adr/0008-modular-monolith-layout.md)) and doc-comment checks |
| `make test` | the tests, with `-race` and a 3-minute timeout; `PKG=./internal/<package>/...` runs one package. Tests that need PostgreSQL start their own through testcontainers |
| `make generate` | regenerates the typed query code from the SQL with `sqlc` |
| `make build` | both binaries into `bin/` |
| `make check` | `lint` and `test`, what CI runs |
| `make up` | builds the image, starts everything (PostgreSQL, RabbitMQ, migrations, the API on `:8080`, the worker) and waits until it is healthy |
| `make deps` | starts only PostgreSQL and RabbitMQ, for running the API from source |
| `make down` | stops the local stack and keeps its data |
| `make smoke` | creates and reads a notification through the running API (`scripts/smoke.sh`) |
| `make migrate` | applies the migrations to the local stack's database (`go run ./cmd/api migrate`) |
| `make api` | runs the API from source on `:8080` against the local stack (after `make deps`) |



## Local stack

`compose.yaml` runs the whole service: PostgreSQL, RabbitMQ, the migrations and the API
([ADR 0010](adr/0010-local-environment-with-docker-compose.md), [ADR 0019](adr/0019-container-image-and-the-one-command-stack.md)).
Tests do not need it.

| Service | Address | Credentials |
|---|---|---|
| PostgreSQL | `postgres://localhost:5432/notifications` | `notifications` / `notifications` |
| RabbitMQ (AMQP) | `amqp://localhost:5672` | `notifications` / `notifications` |
| RabbitMQ management UI | <http://localhost:15672> | `notifications` / `notifications` |
| API (after `make up`) | <http://localhost:8080> | none |
| Stand-in provider (WireMock), its received requests | <http://localhost:8081/__admin/requests> | none |
| Metrics: API, worker, summary | <http://localhost:8080/metrics>, <http://localhost:9090/metrics>, <http://localhost:8080/v1/metrics> | none |
| Probes: API, worker (`/healthz` the same) | <http://localhost:8080/readyz>, <http://localhost:9090/readyz> | none |

- `docker compose exec postgres psql -U notifications` opens a SQL shell.
- To change a port or a password, copy `.env.example` to `.env` and edit it.
- `docker compose down --volumes` deletes the data and starts the next `make up` from empty.
- Image tags are the ones in `internal/platform/testenv`; change both together, or `make test` fails.
- The worker sends to the stand-in provider unless `PROVIDER_URL` in `.env` names another, such as a webhook.site
  URL answering `202` ([ADR 0025](adr/0025-worker-in-compose-and-the-provider-stand-in.md)). The stand-in answers
  `+905550000503` with `503` (retries, the circuit breaker) and `+905550000422` with `422` (a permanent failure).
- To try the API: `make up` and `make smoke`. To work on it from source: `make deps`, `make migrate`, `make api`,
  then
  `curl -i -X POST localhost:8080/v1/notifications -H 'Content-Type: application/json' -d '{"to":"+905551234567","channel":"sms","content":"Hello"}'`.
  The contract is [api/openapi.yaml](../api/openapi.yaml).

## Where code goes

| Path | Holds |
|---|---|
| `cmd/api`, `cmd/worker` | composition roots: configuration, platform clients and modules are built and connected here |
| `internal/httpapi` | the central router: the middleware chain and the one place modules are mounted |
| `internal/<module>` | one feature: its types, use cases, handlers, storage and tests (`notification`, `delivery`, `dispatch`, `health`) |
| `internal/platform/<name>` | infrastructure without business meaning (`config`, `postgres`, `rabbitmq`, `httpx`, `observability`, and `testenv` for integration tests); its conventions are [ADR 0013](adr/0013-platform-conventions.md) |
| `migrations` | versioned SQL migrations (golang-migrate files), embedded in the binaries; each block adds the tables and columns it uses |
| `internal/<module>/queries.sql`, `internal/<module>/internal/db` | a module's SQL and the code `sqlc` generates from it, reachable only from that module |
| `api/openapi.yaml` | the API contract |
| `compose.yaml`, `Dockerfile` | the local stack and the application's image, at the root where Docker looks for them; `deploy/` only for configuration files mounted into containers |

## Go practices this codebase relies on

- **Errors are values.** Every error is returned, wrapped with context (`fmt.Errorf("claim batch: %w", err)`)
  and checked upstream with `errors.Is` or `errors.As`; none is ignored.
- **`context.Context` is the first parameter** of every function that does I/O, and carries cancellation and
  deadlines; every outbound call and query has a timeout.
- **Every goroutine has an owner that stops it** (context cancellation and a `sync.WaitGroup`); shutdown
  finishes in-flight work before the process exits.
- **Interfaces are small and declared where they are used**; constructors return concrete types.
- **Dependencies are wired by hand** in each `cmd/*/main.go`.
