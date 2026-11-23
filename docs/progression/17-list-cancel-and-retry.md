# 17. List, cancel and retry

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0018](../adr/0018-listing-cancelling-and-retrying.md); bulk retry from
  [ADR 0014](../adr/0014-dead-letters-and-replay.md)

## Goal

List notifications with filters and stable pagination, cancel those still waiting, and send failed ones again,
one at a time or in bulk.

## Steps

1. No open question needed the owner: keyset pagination newest first (50 per page, up to 100), unknown query
   parameters refused, cancel repeatable and refused once sending, bulk retry capped at 10,000 per request, each
   with its reason in ADR 0018.
2. Domain, test first: `StatusesThatCanBecome`, so cancel's SQL takes its allowed statuses from the transition
   table; `ListFilter`, `Cursor`, `Page`, `RetryFilter`, `RetryResult`, `MaxRequeue`, `ErrNotCancellable`,
   `ErrNotRetryable`; notifications gain `BatchID` and `FailedAt`, which the struct conversion to the response
   forced the API to show.
3. `migrations/0003_list_and_retry`: `failed_at` with a `CHECK` tying it to the `failed` status, the
   `(created_at, id)` index and a partial index of failed rows. sqlc needed table aliases in the bulk retry's CTE.
4. Store, tests first against PostgreSQL: `List` (a batch's shared creation time paged three at a time without
   loss or repeat, every filter), `Cancel` (repeatable; refused once sending; raced twenty times against a claim),
   `Retry`, `CountFailed`, `RequeueFailed` (window, channel, batch, cap).
5. Service and handlers, tests first: `List` asks for one row more than the page; query parsing reports every bad
   parameter by name; `FieldProblem` gained `parameter` beside `pointer`; the opaque cursor is base64 of the time
   and ID. The router test's fake now embeds `*notification.Service`, so it no longer grows with every method.
6. The end-to-end test pages, cancels twice, retries, and runs a dry and a real bulk retry; `startAPI` now also
   returns the database URL, to mark notifications failed as the worker will.
7. Added the four operations and the new schemas to `api/openapi.yaml`; recorded ADR 0018.

## Verification

- Package by package with `-race`: `notification` (93% coverage), `httpapi`, `postgres` (migration 0003 rolls
  back and reapplies), `cmd/api` with three end-to-end tests. `golangci-lint`: 0 issues.
- `api/openapi.yaml` parses, holds eight operations, and every reference resolves.

## Follow-ups

Block 18, the one-command stack: the application's Dockerfile and the `migrate`, `api` and `worker` services in
`compose.yaml`.
