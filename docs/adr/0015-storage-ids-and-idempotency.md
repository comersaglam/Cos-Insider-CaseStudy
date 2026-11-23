# 0015. Storage: identifiers, idempotency and schema conventions

- Status: Accepted
- Date: 2026-11-23
- Amends: [0011](0011-notification-domain.md) (the recipient's column is named `recipient`, not `to`)

## Context

The first tables are written: `notifications` and the `outbox`. Before they are, four questions need an answer:
what a notification's ID is and who makes it; how long an idempotency key holds; what a reused key with a
different request gets; and how enumerations are stored. A batch of 1000 writes 1000 notifications and 1000
outbox rows that point at them, through PostgreSQL's `COPY`, which does not return values the database generates.

### Identifiers

| Option | Batch with its outbox rows | Index | What it reveals |
|---|---|---|---|
| **UUID v7 made in Go** (`uuid.NewV7`, standard library since Go 1.27) | IDs known before writing; `COPY` writes both tables | grows at one end, like a sequence | the creation time, which the API returns anyway |
| UUID v7 made by PostgreSQL (`DEFAULT uuidv7()`, PostgreSQL 18) | `COPY` returns no IDs; rows one by one with `RETURNING` | same | same |
| UUID v4 | as the first | random inserts across the index: page splits, cache misses, more WAL | nothing |
| `bigserial` | as the second | best | the number of notifications; IDs can be guessed |

### Idempotency keys

| Option | For | Against |
|---|---|---|
| **No expiry: a unique index on the notification** | One index; a repeat a year later is still caught | The index grows with the table, and goes with it under a retention policy |
| 24 hours in a separate table (Stripe) | Bounded table | A cleanup job; a retry after the window sends a second message |

| A key reused with a different request | Why |
|---|---|
| **`422 idempotency_key_reused`** | The IETF draft on the `Idempotency-Key` header names 422 for this case; the client's mistake becomes visible |
| Return the first notification | Hides the mistake: the client believes its new content was sent |
| `409 Conflict` | The draft uses 409 for a first request still in progress |

## Decision

- **IDs are version 7 UUIDs made in Go.** The column is `uuid` with `DEFAULT uuidv7()` as a fallback for rows
  written by hand. pgx reads and writes the standard library's `uuid.UUID` with no adapter; a test checked it.
- **An idempotency key holds as long as its notification.** Its scope is global, since the API has no notion of
  client. The notification stores the key and the SHA-256 fingerprint of the request (length-prefixed fields, so
  moving characters between fields changes it); a partial unique index covers rows that have a key.
- **Creating is one transaction**: `INSERT ... ON CONFLICT (idempotency_key) DO NOTHING`, then the outbox row.
  When the key is taken, the insert waits for the transaction holding it, then the stored notification is
  returned if the fingerprints match, or `ErrIdempotencyKeyReused` (answered `422`) if they differ. Twenty
  concurrent requests with one key create one notification.
- **Enumerations are `text` with a `CHECK`**, not PostgreSQL `ENUM` types: the same guarantee, and changing the
  set of values is one statement, while an `ENUM` value cannot be removed or renamed easily.
- **The recipient column is `recipient`.** `TO` is a reserved word in SQL; the API and the provider still call it
  `to`, and the store maps between the two.
- **Times come from the database** (`DEFAULT now()`), one clock for every replica, and are returned in UTC; pgx
  reads `timestamptz` in the host's zone, so the store converts.
- **Each block migrates what it uses.** The first migration creates `notifications` and `outbox` only; batches,
  attempts, leases and failure reasons arrive as later migrations with the code that needs them.
- **Queries are written in SQL and turned into Go by sqlc** into `internal/notification/internal/db`, which Go's
  `internal` rule keeps out of every other module.
- **Tests run against a database of their own**: one PostgreSQL container per test process, a template database
  migrated once, and a copy of it for each test (`CREATE DATABASE ... TEMPLATE`), which takes milliseconds. A
  depguard rule keeps the test helper, and testcontainers, out of production code.

## Consequences

- A batch can be written with `COPY`, IDs and outbox rows included.
- A client that retries a request, however late, never creates a second notification.
- The first-version schema is small and grows migration by migration, which shows how the schema evolved.

### Deliberately not done

| Not done | Why not |
|---|---|
| Idempotency keys per client | The API has no authentication; the scope can narrow when clients exist. |
| A cleanup job for keys | Keys live and go with their notifications. |
| Quoting `"to"` as a column name | Every hand-written query would need the quotes; one missed quote is a syntax error. |
| A transaction helper of our own | `pgx.BeginFunc` commits on nil and rolls back on an error or a panic. |
| Resetting tables between tests | Copying a template gives each test a clean database and lets tests run in parallel. |
