# 0018. Listing, cancelling and retrying

- Status: Accepted
- Date: 2026-11-23

## Context

The brief asks to list notifications with filters (status, channel, date range) and pagination, and to cancel
pending ones; ADR 0007 and ADR 0014 add retrying one failed notification and many. The questions are how pages
follow each other, what a list accepts, what cancel and retry do when the notification is not in the state they
need, and how much one bulk retry may do.

### Pagination

| Option | For | Against |
|---|---|---|
| Offset (`?page=3`) | Simple, can jump to any page | Rows inserted meanwhile shift every page, so rows are repeated or skipped; deep pages scan everything before them |
| **Keyset (cursor) on `(created_at, id)`** | Stable while rows arrive; every page costs the same through the index | No jumping to page N, which a notification list does not need |

`created_at` alone is not enough: the notifications of one batch share it, being written in one transaction.

## Decision

- **`GET /v1/notifications`**, newest first by `(created_at, id)`, read backwards from the index
  `notifications_created`. Filters: `status` and `channel` (repeatable, any of their values), `batchId`,
  `createdFrom` (inclusive) and `createdTo` (exclusive) as RFC 3339 times with their offset. `limit` is 1 to 100,
  50 by default. Pages are `{items, nextCursor}`; `nextCursor` is opaque and absent on the last page. The service
  asks the store for one row more than the page to know whether another follows, without counting.
- **Query errors** are `400 invalid_query`, every bad parameter reported at once in `errors` by `parameter`,
  since a JSON Pointer cannot reach the query; body errors keep `pointer`. An unknown parameter is refused, so a
  misspelt filter (`stauts=failed`) does not silently list everything.
- **`POST /v1/notifications/{id}/cancel`** is one conditional `UPDATE` whose allowed statuses come from the
  domain's transition table (`StatusesThatCanBecome`), so the table stays the one source of the rule. It answers
  `200` with the notification. A notification being sent or finished is `409 not_cancellable`, naming its
  status; a worker claiming it at the same moment and the cancel cannot both win, which a test runs twenty
  times. Cancelling a cancelled notification answers `200` again: the request is safe to repeat.
- **`POST /v1/notifications/{id}/retry`** puts a `failed` notification back to `queued` with a new outbox row in
  one transaction and answers `202`; any other status is `409 not_retryable`.
- **`POST /v1/notifications/retry`** (ADR 0014) requeues the failed notifications that failed inside a required
  window (`failedFrom`, `failedTo`), optionally of one channel or batch, oldest failure first, at most **10,000**
  per request: at 100 messages per second per channel that is under two minutes of sending, and one statement
  writes it in well under a second. It answers `{matched, requeued}` with `202`, or `200` on a `dryRun`, which
  only counts. `FOR UPDATE SKIP LOCKED` keeps two bulk retries from requeueing one notification twice.
- **`failed_at`** records when a notification failed; a `CHECK` ties it to the `failed` status, and a partial
  index covers the failed rows bulk retry reads. Notifications now show `batchId` and `failedAt`.

## Consequences

- A client can walk every notification of a campaign by `batchId`, page by page, while new ones keep arriving.
- Cancel and retry are safe under concurrency without locks of our own: the conditional `UPDATE` decides.
- Recovering from an outage of 50,000 failures is five requests, each reporting what is left.

### Deliberately not done

| Not done | Why not |
|---|---|
| Offset pagination | Unstable while rows arrive, and slower the deeper the page. |
| A total count in each page | A `count(*)` over a large filtered table on every page; bulk retry's dry run counts where a count is needed. |
| Sorting options | Newest first is what a notification list is read for; each other order needs its own index. |
| Indexes per filter combination | The list reads `(created_at, id)` and filters as it goes; a combination that proves slow gets its index when it does. |
| Resetting the attempt count on retry now | There is no attempt count yet; the worker's migration adds it, and retry resets it there. |
