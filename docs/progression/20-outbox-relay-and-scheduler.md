# 20. Dispatch: outbox relay and scheduler

- Date: 2026-11-23
- Commits: `825c4bc`
- Decisions: [ADR 0021](../adr/0021-outbox-relay-and-scheduler.md)

## Goal

Move committed work to RabbitMQ: publish outbox rows as soon as they commit, and queue waiting notifications when
their time comes.

## Steps

1. No question needed the owner. Kept the SQL of `notifications` and `outbox` in the notification module and the
   loops in dispatch; chose deleting published rows over marking them; moved the reaper to block 22, beside the
   lease and attempt columns it needs.
2. Domain, tests first: `Priority.Rank` (the AMQP priority) and `QueueMessage`, the message body, which refuses a
   body that names no notification.
3. `migrations/0004_dispatch`: the outbox loses `published_at` and gains a statement-level `NOTIFY` trigger;
   notifications gain `next_attempt_at`, tied to the waiting statuses by a `CHECK`, and a partial index. Cancel now
   clears `next_attempt_at`.
4. Store, tests first against PostgreSQL: `DrainOutbox` (oldest first, rows kept when publishing fails, the limit,
   four concurrent relays over fifty rows), `QueueDue` (only what is due, three concurrent schedulers),
   `ListenOutbox` (wakes at commit; its connection survives two timeouts). A test goroutine had called `t.Fatal`;
   `go vet` caught it and it now reports with `t.Error`.
5. `dispatch.Relay` and `dispatch.Scheduler`, tests first: against PostgreSQL and RabbitMQ, the relay publishes what
   was stored before and after it started, with a poll interval of a minute, so the trigger woke it; two relays
   publish a hundred rows exactly once; with RabbitMQ down the relay stops and the row waits for the next relay; the
   scheduler queues only what is due; both loops stop with a database error.
6. Recorded ADR 0021.

## Verification

- Package by package with `-race`: `notification` and `dispatch` (92.7% coverage) green; migration 0004 rolls back
  and reapplies. `golangci-lint`: 0 issues.
- Removing the trigger made the relay test time out: what wakes the relay is the `NOTIFY`, not the poll.

## Follow-ups

Block 21, the provider client: the webhook.site request, its timeout and headers, and the classification of every
answer (ADR 0007).
