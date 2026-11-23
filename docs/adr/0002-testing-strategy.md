# 0002. Testing strategy

- Status: Accepted
- Date: 2026-11-23

## Context

The assessment asks for a test suite runnable with a single command. The riskiest code is concurrent (workers
claiming rows, rate limiting, retries) and SQL-heavy (row locking, idempotency constraints), which mocks cannot
prove correct.

## Decision

- **Standard `testing` package, table-driven tests**, with `google/go-cmp` (`cmp.Diff`) only where whole
  structs are compared.
- **Every layer is tested where its risk is:**

  | Layer | What is tested | How |
  |---|---|---|
  | `domain` | validation, status transitions, backoff | unit tests, no dependencies |
  | service | business rules (cancel only while pending, idempotency) | hand-written fake repository |
  | store | SQL, `SKIP LOCKED` concurrency, migrations | real PostgreSQL |
  | api | status codes, error envelope, OpenAPI coverage | `net/http/httptest` |
  | worker | rate limit, retry, provider errors and timeouts | provider replaced by an `httptest.Server` |

- **Real PostgreSQL through `testcontainers-go`**: a test that needs the database starts a throwaway container,
  applies the migrations and removes it afterwards.
- **The race detector is always on** in `make test` and in CI (`go test -race ./...`).
- **No test reaches an external service**; webhook.site is used only by a running stack.

## Consequences

- `make test` needs only Docker and Go; no service has to be started first, and CI runs the same command.
- The first run pulls the PostgreSQL image, which takes a few seconds.

### Deliberately not used

| Not used | Why not |
|---|---|
| `testify` | Popular, but the Go team's own guidance is plain `if got != want { t.Errorf(...) }`, which needs no dependency and gives precise failure messages; `go-cmp` covers the one case (struct diffs) where plain comparisons read badly. |
| Mocking SQL (`sqlmock`) | It tests the query text, not its behaviour; locking and constraint bugs would pass. |
| The Compose database for tests | Tests would depend on a service being started first and could mix with development data. |
