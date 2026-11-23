# 02. Go working style

- Date: 2026-11-23
- Commits: `2d4736e`
- Decisions: [ADR 0001](../adr/0001-go-toolchain-and-libraries.md), [ADR 0002](../adr/0002-testing-strategy.md)

## Goal

Decide the tools, libraries, test approach and daily loop for a team coming to Go from Python and Kotlin,
following the industry standard and recording what is deliberately left out.

## Steps

1. Mapped Python habits to their Go equivalents (dependency management, formatting, errors as values, context
   instead of async, small interfaces and hand-written fakes instead of patching).
2. Listed the practices Go needs that Python does not stress: the race detector, timeouts on every I/O call,
   goroutine ownership, graceful shutdown, zero values, `govulncheck`.
3. Chose the stack: `net/http`, pgx with sqlc, golang-migrate, `log/slog`, Prometheus client, `x/time/rate`,
   RFC 9457 errors, golangci-lint, and VS Code with gopls; rejected router frameworks, ORMs, DI frameworks, mock
   generators and viper, each with its reason (ADR 0001).
4. Chose the test strategy: standard `testing` with `go-cmp`, real PostgreSQL through testcontainers-go, the race
   detector always on, a test layer per risk (ADR 0002).
5. Wrote `docs/development.md` (inner loop, make targets, Go practices) and pointed `CONTRIBUTING.md` to the ADRs.

## Verification

No code yet; the decisions were reviewed by the owner.

## Follow-ups

The architecture of queueing and delivery: block 03.
