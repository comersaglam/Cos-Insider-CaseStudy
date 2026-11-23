# 12. Platform: config, observability, httpx

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0013](../adr/0013-platform-conventions.md)

## Goal

Build the infrastructure every module uses, so all of them read settings, log, answer errors and decode JSON the
same way.

## Steps

1. Chose the standard library for configuration and `X-Correlation-ID` for the correlation header. Found that Go
   1.27 ships `encoding/json/v2` and a `uuid` package in the standard library, and checked with a small program
   that v2 matches member names case-sensitively where v1 does not.
2. Explained why notifications use a work queue rather than publish/subscribe, with a batch of 1000 as the
   example, and added it to ADR 0012. Noted the batch body size as an open question of block 15 and admission
   control as later work.
3. `internal/platform/config`: tests first, then `Reader` (`String`, `Required`, `Secret`, `Int`, `Duration`,
   `LogLevel`, `Err`, `LogValue`). gosec flagged password-like URLs in the test data; the data changed, the
   linter was not silenced.
4. `internal/platform/observability`: tests first, then `NewLogger` with a handler that adds the context's
   correlation ID, the correlation ID helpers, and `MaskRecipient`. A test showed log times in the host's time
   zone (`+03:00`); the logger now writes UTC.
5. `internal/platform/httpx`: tests first, then `Problem`, `WriteProblem`, `WriteJSON`, `Handle`, `DecodeJSON`,
   and the `Correlation`, `AccessLog` and `Recover` middleware with a response recorder that keeps `Unwrap`.
6. Recorded the conventions as ADR 0013.

## Verification

- `make check` passes; the three packages have 100% statement coverage, with `-race`.
- Deliberate bugs were caught: dropping the handler's `WithAttrs` and `WithGroup` lost the correlation ID on
  derived loggers, and dropping the recorder's `Unwrap` made `Flush` fail with `ErrNotSupported`.

## Follow-ups

Block 13, the database and store: migrations (with `attempts` and `claimed_until`), the PostgreSQL pool, sqlc,
the testcontainers helper and the notification store; the notification's ID type is chosen there, now that the
standard library has a `uuid` package.
