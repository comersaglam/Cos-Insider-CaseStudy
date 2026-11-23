# 14. Database and store

- Date: 2026-11-23
- Commits: `6ccf017`
- Decisions: [ADR 0015](../adr/0015-storage-ids-and-idempotency.md), amending
  [ADR 0011](../adr/0011-notification-domain.md)

## Goal

Store notifications in PostgreSQL: the first migration, the connection pool, the migration runner, typed queries,
a database for every test, and a store that creates a notification and its outbox row exactly once per
idempotency key.

## Steps

1. Settled the block's questions: UUID v7 made in Go, idempotency keys without expiry, `422` for a key reused
   with another request, enumerations as `text` with `CHECK`. Added pgx v5.13, golang-migrate v4.20 and
   testcontainers-go v0.45 with the owner's approval.
2. Checked first that pgx reads and writes the standard library's `uuid.UUID` with no adapter, including `NULL`
   and PostgreSQL's own `uuidv7()`.
3. Wrote `migrations/0001_notifications` (`notifications`, `outbox`, the partial unique index on the key), named
   the recipient column `recipient` since `TO` is reserved in SQL, and embedded the files in package
   `migrations`. Tables used by later blocks are left to their own migrations.
4. `internal/platform/postgres`: `Connect` and `Migrate`, with a test that rolls every migration back and applies
   them again. `internal/platform/testenv.PostgresDSN`: one container per test process, a migrated template, a
   copy per test. A depguard rule keeps it, and testcontainers, out of production code; a deliberate import
   proved the rule fires.
5. `internal/notification`: tests first for `Draft.Fingerprint`, then `Notification`, `IdempotencyKey`,
   `ErrNotFound`, `ErrIdempotencyKeyReused` and the fingerprint.
6. `sqlc.yaml`, `queries.sql` and the generated `internal/notification/internal/db`; tests first for the store,
   then `Store.Create` and `Store.Get`. `make generate` added.
7. A full `make check` hung for ten minutes. The cause was in this block: moving the migrator's `Close` to
   `t.Cleanup` made it run after the deferred `pool.Close`, which waits for every connection, so the test
   deadlocked. The pool now closes in a cleanup registered first, which runs last. `make test` gained a
   3-minute timeout and `PKG=` to run one package; CONTRIBUTING now asks for package-by-package runs.

## Verification

- Each package run on its own with `-race`: `postgres` 4.6 s, `notification` 4.4 s (95.4% coverage; the lines
  left are database error wrapping), `config`, `observability`, `httpx`, `testenv` about 1.4 s each.
- `golangci-lint run ./...`: 0 issues.
- The store tests cover create and read, a missing ID, a repeated request, a key reused for another request,
  requests without keys, and twenty concurrent requests with one key creating one notification.

## Follow-ups

Block 15, create and get through a running API: the service, the handlers, the `httpapi` router and the
`cmd/api` composition root with graceful shutdown.
