# 16. Batch create and batch status

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0017](../adr/0017-batches.md)

## Goal

Accept up to 1000 notifications in one request without letting one invalid item refuse the rest, keep a record of
what was rejected and why, and report a batch's progress by status.

## Steps

1. Settled the block's questions with the owner: invalid items are reported and the valid ones accepted; the body
   limit is 10 MiB, configurable as `BATCH_MAX_BODY_BYTES`. Explained that a rejected item never becomes a
   notification, so the 8-attempt rule and replay apply only to valid items that later fail delivery.
2. `internal/notification`, tests first: `MaxBatchSize`, `BatchFingerprint`, `NewBatch`, `Batch`, `Rejection`,
   `BatchError`, and `Service.CreateBatch` and `GetBatch`. The service's fake store gained the batch methods.
3. `migrations/0002_batches`: the `batches` table (totals, rejections as `jsonb` with a `CHECK` that they add up,
   the idempotency key) and `notifications.batch_id` and `batch_index`, unique together. sqlc now maps `integer`
   to `int` and nullable `uuid` to `*uuid.UUID`; the batch queries include two `:copyfrom` queries for `COPY`.
4. `Store.CreateBatch` and `GetBatch`, tests first against PostgreSQL: a full batch of 1000, a repeated batch, a
   key reused for another batch, counts by status, and five concurrent requests with one key creating one batch.
5. `POST /v1/batches` and `GET /v1/batches/{id}`, tests first, with the shared `idempotencyKey` and JSON Pointer
   helpers; `cmd/api` reads `BATCH_MAX_BODY_BYTES`; the end-to-end test sends a batch with one invalid item.
   The route `POST /v1/notifications/batch` of the roadmap became `POST /v1/batches`.
6. `TestRunMigrate` had dropped the tables it knew by name and failed once `batches` existed; it now drops the
   whole schema.
7. Added both endpoints to `api/openapi.yaml`; recorded ADR 0017.

## Verification

- Package by package with `-race`: `notification` (93% coverage), `httpapi`, `postgres` (migration 0002 rolls
  back and reapplies), `cmd/api` including `TestAPIBatchEndToEnd` against PostgreSQL. `golangci-lint`: 0 issues.
- `api/openapi.yaml` parses and every reference resolves.

## Follow-ups

Block 17, list, cancel and retry: `GET /v1/notifications` with filters and keyset pagination, cancel while
waiting, single and bulk retry (ADR 0014).
