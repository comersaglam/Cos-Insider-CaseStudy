# 0021. The outbox relay and the scheduler

- Status: Accepted
- Date: 2026-11-23

## Context

ADR 0003 joins PostgreSQL and RabbitMQ with a transactional outbox, and ADR 0006 keeps delayed work in PostgreSQL
until it is due. Two loops carry it out: the relay, which hands committed outbox rows to RabbitMQ, and the
scheduler, which queues waiting notifications when their time comes. The questions are who owns their SQL, how
the relay learns of new rows, what becomes of a published row, and what a loop does when RabbitMQ or the database
fails.

### Waking the relay

| Option | Latency | Cost |
|---|---|---|
| Poll every second | up to a second | a query a second, whether or not there is work |
| **`LISTEN/NOTIFY` from a trigger, with a slow poll as fallback** | as soon as the row commits | one connection held open; the poll catches a notification lost to a reconnect |

### A published row

| Option | For | Against |
|---|---|---|
| **Delete it** | The table holds only pending work; its queries stay small | No record of when it was published |
| Mark it `published_at` | A record of publication | The table grows without end and needs a cleanup job; the notifications already keep the history |

## Decision

- **The notification module owns the SQL** of its tables, `outbox` included: `Store.DrainOutbox`,
  `Store.QueueDue` and `Store.ListenOutbox`. The dispatch module holds the loops and calls them, so it writes no SQL
  (ADR 0008).
- **`DrainOutbox`** takes up to a batch of rows, oldest first, with `FOR UPDATE OF outbox SKIP LOCKED`, hands them
  to the publish function, and deletes them in the same transaction once the publish has returned. Two relays never
  take one row; a failed publish leaves the rows; a commit lost after a publish republishes them, and consumers
  drop the duplicates (ADR 0012).
- **A trigger on `outbox`** sends `NOTIFY outbox` once per statement, so a batch written with `COPY` sends one, at
  commit. The relay listens on a connection of its own, outside the pool, and also looks every `PollInterval`.
- **Messages** are routed by the notification's channel, carry its priority's rank (high 3, normal 2, low 1) as
  AMQP priority, its ID as message ID, and a body of `{"notificationId": ...}`: the worker reads the rest from the
  database, where the status says whether the notification is still to be sent.
- **The scheduler** runs one statement every `Interval`: it moves up to a batch of `scheduled` and `retrying`
  notifications whose `next_attempt_at` has passed to `queued`, with their outbox rows, under `SKIP LOCKED`.
  `next_attempt_at` exists exactly while a notification waits in one of those statuses (a `CHECK`), and a partial
  index covers the query.
- **Failures stop the loop.** Either loop returns the error when RabbitMQ or the database fails; the process stops
  and is restarted (ADR 0009). Rows stay where they are and are picked up after the restart.
- **Publication order is the outbox's order**, oldest first. Priority takes effect in RabbitMQ's priority queues;
  publishing a backlog takes seconds, while sending it takes minutes.
- **The reaper** of ADR 0012 belongs with the claim and its lease, and is built with them in the consumer's block.

## Consequences

- A notification reaches RabbitMQ within milliseconds of its commit, not on the next poll.
- RabbitMQ being down delays notifications but loses none: they wait in the outbox.
- Any number of relays and schedulers can run; `SKIP LOCKED` divides the work.

### Deliberately not done

| Not done | Why not |
|---|---|
| Ordering the outbox by priority | It would need the priority copied into the outbox and indexed; the queues order by priority already, and a backlog leaves the outbox in seconds. |
| Logical replication or Debezium to read the outbox | The standard at larger scale, but it needs another component to run; `SKIP LOCKED` polling with `NOTIFY` is enough for one database. |
| Retrying a failed publish inside the relay | A broken connection is not recovered in-process (ADR 0009); the restart retries. |
