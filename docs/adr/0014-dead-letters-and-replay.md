# 0014. Dead letters and replay

- Status: Accepted
- Date: 2026-11-23
- Amends: [0007](0007-delivery-and-retry.md) (adds bulk replay) and [0011](0011-notification-domain.md) (adds the
  transition `queued` → `failed`)

## Context

[ADR 0007](0007-delivery-and-retry.md) made PostgreSQL the dead-letter store: a notification that runs out of
attempts, or is rejected for good, stays `failed` with every attempt recorded, and
`POST /v1/notifications/{id}/retry` sends one of them again. RabbitMQ's `notifications.dlq` receives only
messages that cannot be processed at all. Two cases were left open:

- **Replaying many notifications.** A wrong provider key answered `401` to 5000 SMS; all are `failed` at once,
  since `401` is permanent. After the key is fixed, retrying them one request at a time means a script of 5000
  calls.
- **A dead-lettered message whose notification can be identified.** Its notification stays `queued` in
  PostgreSQL forever: the reaper looks only at `sending`, and the API keeps showing it as waiting.

| Bulk replay | For | Against |
|---|---|---|
| One request per notification (as before) | Nothing new | A script of thousands of calls for a common recovery |
| Per batch: `POST /v1/batches/{id}/retry` | Simple, bounded | Misses notifications sent one at a time |
| **By filter: `POST /v1/notifications/retry`** | One request for any recovery, batches included | A wide filter could resend a lot; needs guards |

| A dead-lettered message with a readable ID | For | Against |
|---|---|---|
| **Mark the notification `failed` as well** | Visible through the API; replayed from PostgreSQL as a fresh, well-formed message | A new transition |
| Leave it only in the dead-letter queue | No change | The notification looks queued forever; replay means moving a malformed message back |

## Decision

### Replay always starts from PostgreSQL

A replay puts `failed` notifications back to `queued` with a fresh attempt budget and writes their outbox rows in
the same transaction, so the relay publishes new messages. Raw messages are never moved from the dead-letter
queue back into a work queue as a way of replaying notifications.

- One notification: `POST /v1/notifications/{id}/retry`, as before.
- Many: `POST /v1/notifications/retry` with a filter of `channel`, `batchId` and a required time window
  (`from`, `to`) on when they failed. It is one conditional `UPDATE` and one `INSERT ... SELECT` into the outbox,
  in one transaction, and answers how many notifications it requeued.
- Guards: the time window is required; `dryRun: true` answers how many notifications match without changing
  any; one request requeues at most a fixed number of notifications (set in the block that builds it), and a
  larger recovery is a few requests.

This is the same operation as "redrive" in Amazon SQS, which moves messages out of a dead-letter queue in bulk.

### Messages that cannot be processed

| The message | Handling |
|---|---|
| Unreadable, but its notification ID can be read | the notification moves `queued` → `failed` with the reason `unprocessable_message`; the message is also dead-lettered, as evidence |
| Without a readable ID | dead-lettered only; a metric shows the dead-letter queue's depth, and an alert fires when it is above zero |

A message left in `notifications.dlq` is inspected in the RabbitMQ management UI and, once the cause is fixed,
moved back with its "Move messages" action (the shovel plugin). No tool of our own is written for it.

## Consequences

- Every notification the system could not deliver is visible through the API as `failed`, with a reason, and can
  be replayed, one or many at a time.
- The domain gains the transition `queued` → `failed`.
- The notification stores a failure reason (`provider_rejected`, `attempts_exhausted`, `unprocessable_message`),
  decided with the schema.
- The dead-letter queue holds only messages nobody can attribute to a notification, which should never happen;
  its alert is therefore a bug report.

### Deliberately not done

| Not done | Why not |
|---|---|
| A replay tool for the dead-letter queue | The management UI moves messages already; and a replay from PostgreSQL produces a well-formed message instead of resending a malformed one. |
| Replaying `queued` notifications whose messages were lost (a single RabbitMQ node losing its disk) | Rare, and telling a lost message from one waiting in a long backlog needs care; recorded as later work: a reconcile command that republishes them. |
| Bulk replay as a background job | One transaction over a bounded number of rows takes seconds; a job queue would add state to track for no gain. |
