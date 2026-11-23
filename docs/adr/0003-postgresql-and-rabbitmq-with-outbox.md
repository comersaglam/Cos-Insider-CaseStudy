# 0003. PostgreSQL as the source of truth, RabbitMQ for dispatch, joined by a transactional outbox

- Status: Accepted
- Date: 2026-11-23

## Context

Notifications are accepted by the API and delivered asynchronously by workers, with priorities, cancellation,
idempotency and burst traffic (a flash sale can enqueue ten thousand messages at once). The options considered:

| Option | Why it was or was not chosen |
|---|---|
| PostgreSQL only, as a queue (`FOR UPDATE SKIP LOCKED`, `LISTEN/NOTIFY`) | Simplest and fully transactional, but workers poll the database and the design does not show event-driven messaging. |
| **PostgreSQL + RabbitMQ with a transactional outbox** | Chosen. A broker built for work distribution: push delivery, native priorities, acknowledgements, dead-lettering, single active consumer. The team has worked with it. |
| PostgreSQL + Kafka | Built for event streaming and replay at very large scale; it has no per-message priority, delay, cancellation or acknowledgement, all of which this problem needs. |
| Redis with a job library (asynq) | The library would implement the queueing, retry and scheduling that this service is meant to design; Redis persistence and the dual-write problem remain. |

Writing a notification to the database and publishing it to a broker are two writes to two systems; if the
second fails after the first succeeds, a stored notification is never sent (the dual-write problem).

## Decision

- **PostgreSQL is the source of truth** for every notification and its status.
- **The transactional outbox**: the API writes the notification and an `outbox` row in the same transaction. A
  relay reads unpublished outbox rows (`FOR UPDATE SKIP LOCKED`, so several relays never publish the same row),
  publishes them, and marks them published only after RabbitMQ confirms.
- **Topology**: one durable queue per channel, `notifications.sms`, `notifications.email`, `notifications.push`,
  each a classic queue with `x-max-priority: 3` for the high, normal and low priorities, and
  `x-single-active-consumer: true` ([0005](0005-rate-limit-with-single-active-consumer.md)). Messages that cannot
  be processed at all are dead-lettered to `notifications.dlq`.
- **Reliability**: persistent messages, publisher confirms, and manual acknowledgement after the database has
  been updated. Delivery is therefore **at least once**.
- **Idempotency and cancellation are checked at consumption**: before sending, the worker reads the
  notification's status; a cancelled or already sent notification is acknowledged without sending. The provider
  call carries the notification's ID as its idempotency key.
- **Client**: `rabbitmq/amqp091-go`, the RabbitMQ team's Go client.

## Consequences

- No notification is lost when RabbitMQ is briefly unavailable: its outbox row waits and the relay retries.
- A message may be delivered to a worker twice (after a crash between send and acknowledgement); the status
  check and the provider idempotency key keep that from becoming a second send.
- Priority inside a queue is strict: under sustained high-priority load, low-priority messages wait. This is
  accepted for burst traffic and shown by the queue-depth metric per priority.
- Classic queues are not replicated. A multi-node production cluster would use quorum queues, which support
  only two priority levels, or one queue per priority; that change is out of scope for a single-node setup.
- The relay adds a small publication delay (its poll interval) between commit and dispatch.
