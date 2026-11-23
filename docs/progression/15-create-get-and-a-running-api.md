# 15. Create, get and a running API

- Date: 2026-11-23
- Commits: `d222e94`
- Decisions: [ADR 0016](../adr/0016-http-api-and-the-api-process.md)

## Goal

Serve the first two endpoints from a running process: accept a notification, read it back, and stop without
dropping a request in flight. Start the API's contract.

## Steps

1. Settled the block's questions: `202 Accepted` for a create, migrations as a separate `api migrate` step, the
   OpenAPI contract written by hand from this block on. Defaults: `priority` means `normal` when left out,
   `422` for invalid notifications and reused keys, `404` for unknown or malformed IDs, a 1 MiB body limit.
2. `internal/platform/httpx`: tests first, then `NewServer` (timeouts, errors to the JSON log), `Serve` (graceful
   shutdown bounded by a timeout) and `ProblemFallback` (404 and 405 as problems, the mux's redirects untouched).
   A test first expected `301` for an unclean path; Go 1.27 answers `307`, so the test now compares with the mux
   itself. A needless receive in `Serve` was removed when errcheck and staticcheck disagreed about it.
3. `internal/notification`: tests first, then `Service` (validation, a queued notification with a UUID v7 and the
   correlation ID, the fingerprint for the key, a log line with the recipient masked) and the handlers behind
   `RegisterRoutes`, with `toProblem` mapping the module's errors to answers.
4. `internal/httpapi.NewRouter` mounts the module, adds `ProblemFallback` and the middleware chain. Its test was
   written in the same step as the code; removing the correlation middleware and the fallback each made it fail.
5. `cmd/api`: tests first, then `run(ctx, args, lookup, stdout)` with the `serve` and `migrate` commands; `main`
   only connects signals, arguments, environment and output. `make migrate` and `make api` added.
6. Ran the binary against the local stack: a create answered `202` with `Location`, an invalid request `422`
   with pointers, logs were JSON in UTC with the URL hidden and the recipient masked, and SIGTERM stopped it.
7. Wrote `api/openapi.yaml` (OpenAPI 3.1, rules per channel as `if`/`then`); checked that it parses and that
   every reference resolves. Linting it with a container image was refused by the session's permission
   classifier (an unpinned image); a pinned validator arrives with the OpenAPI block.
8. Recorded ADR 0016.

## Verification

- Package by package, with `-race`: `httpx` 100% coverage, `notification` and `httpapi` green, `cmd/api` 4.6 s
  including the end-to-end test against PostgreSQL (create, read back, repeat with one key, reuse the key,
  invalid input, unknown path, clean stop).
- A request in flight finishes during shutdown while new connections are refused
  (`TestServeFinishesARequestInFlightWhenStopped`); one that outlasts the timeout makes `Serve` report it.
- `golangci-lint run ./...`: 0 issues.

## Follow-ups

Block 16, batch create and batch status: `POST /v1/notifications/batch` and `GET /v1/batches/{id}`, with the
`batches` migration; it starts with its open questions (atomic or partial batches, the body limit).
