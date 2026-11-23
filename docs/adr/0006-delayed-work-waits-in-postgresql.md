# 0006. Delayed work waits in PostgreSQL; RabbitMQ carries only work that is due

- Status: Accepted
- Date: 2026-11-23

## Context

Two kinds of work must wait before it is sent: a retry after a failed attempt (with exponential backoff) and a
notification scheduled for a later time. Retries of many messages that failed together should not all fire in
the same second.

| Option | Why it was or was not chosen |
|---|---|
| **PostgreSQL holds waiting work in `next_attempt_at`; a scheduler publishes it when due** | Chosen: any delay per message with jitter, scheduling for free, cancellation exact until publication. |
| Per-tier wait queues in RabbitMQ (message TTL plus a dead-letter exchange back to the main queue) | Broker-native and common, but delays come in fixed tiers with no per-message jitter, each channel needs several extra queues, and scheduling at an arbitrary time still needs the database. |
| The `rabbitmq-delayed-message-exchange` plugin | Not in the default image; delayed messages are not replicated, and the plugin's own documentation advises against holding many of them. |

## Decision

- A notification row carries `next_attempt_at`. A new notification gets the current time, or its scheduled time.
- After a retryable failure the worker sets status `retrying` and `next_attempt_at = now + backoff + jitter`, then
  acknowledges the RabbitMQ message.
- A scheduler loop in the worker selects due rows (`FOR UPDATE SKIP LOCKED`) and writes their outbox rows in the
  same transaction; the relay publishes them as in [0003](0003-postgresql-and-rabbitmq-with-outbox.md).
- RabbitMQ therefore holds only messages that are due now.

## Consequences

- Backoff and jitter are computed per message, so a failed burst retries spread out.
- A scheduled or retrying notification can be cancelled with one conditional `UPDATE` while it waits.
- Queue depth has two parts, both measured: waiting in PostgreSQL by status, and ready in RabbitMQ.
- The scheduler polls on a short interval, a small steady load on the database, and adds up to one interval of
  delay to delayed work.
