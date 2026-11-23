# 0001. Go toolchain and libraries

- Status: Accepted
- Date: 2026-11-23

## Context

The service is written in Go (1.2x, per the assessment). Go's culture favours the standard library and small,
explicit dependencies over frameworks. Every dependency added is code a reviewer must trust and the team must
be able to explain, so each one has to earn its place against what the standard library already offers.

## Decision

### Used

| Concern | Choice | Why |
|---|---|---|
| HTTP routing and server | `net/http` (`ServeMux`, Go 1.22+ method and path patterns) | Since Go 1.22 the standard mux matches `GET /v1/notifications/{id}`; a router framework adds nothing we need. |
| Database driver | `jackc/pgx/v5` with `pgxpool` | The de facto PostgreSQL driver for Go: native protocol, connection pool, `COPY` for batch inserts. |
| Query layer | `sqlc` generating code over pgx | SQL stays hand-written and visible (needed for `FOR UPDATE SKIP LOCKED`, partial indexes, keyset pagination), while row scanning is generated and type-checked at compile time. |
| Migrations | `golang-migrate` with versioned `NNNNNN_name.up.sql` / `.down.sql` | Widely used, plain SQL files, runnable as a CLI in Docker Compose and as a library in tests. |
| Logging | `log/slog` (standard library), JSON handler | Structured logging is in the standard library since Go 1.21. |
| Metrics | `prometheus/client_golang`, exposed at `GET /metrics` | The industry standard metrics format; queue depth, success and failure counts and latency histograms map directly onto gauges, counters and histograms. |
| Rate limiting | `golang.org/x/time/rate` | The Go team's token-bucket implementation. |
| Error responses | RFC 9457 `application/problem+json`, extended with a stable `code` field | The IETF standard for HTTP API errors. |
| Configuration | Environment variables read into a `Config` struct with the standard library | Twelve-factor configuration; there are few settings. |
| Formatting and linting | `gofmt`/`goimports`, `golangci-lint` (includes `go vet`, `staticcheck`, `revive`) | One enforced style; the linter also checks that exported identifiers have doc comments. |
| Vulnerability scanning | `govulncheck` | The Go team's scanner for known vulnerabilities in dependencies. |
| Editor | VS Code with the official Go extension (`gopls`) | Formats on save and runs single tests from the editor. |

### Deliberately not used

| Not used | Why not |
|---|---|
| Gin, Echo, Fiber, chi | The standard `ServeMux` covers our routes; a framework would be a dependency without a need. |
| GORM or another ORM | Hidden queries make row locking and partial indexes hard to control; Go practice favours explicit SQL. |
| Hand-written `row.Scan` everywhere | Repetitive, and a column-order mistake only shows at run time; `sqlc` removes both problems. |
| `google/wire`, `uber-go/fx` | Dependency injection is done by hand in `main`, where the whole graph can be read in one place. |
| `mockery`, `gomock` | Interfaces are small and defined by their consumer; a hand-written fake is a few readable lines. |
| `spf13/viper` | Built for layered config files and flags; a handful of environment variables does not need it. |
| `testify` | See [0002](0002-testing-strategy.md). |
| `golang-standards/project-layout` | Not an official standard and larger than this service needs. |

## Consequences

- Fewer dependencies to trust and update; most of the code reads as plain Go.
- `sqlc generate` is a build step: generated files are committed and CI checks they are up to date.
- Routing features a framework would provide (route groups, binding) are written as small middleware where
  needed.
