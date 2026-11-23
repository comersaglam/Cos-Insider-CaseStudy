# 03. Messaging architecture

- Date: 2026-11-23
- Commits: `7589500`
- Decisions: [ADR 0003](../adr/0003-postgresql-and-rabbitmq-with-outbox.md),
  [ADR 0004](../adr/0004-separate-api-and-worker-binaries.md),
  [ADR 0005](../adr/0005-rate-limit-with-single-active-consumer.md),
  [ADR 0006](../adr/0006-delayed-work-waits-in-postgresql.md)

## Goal

Settle the decisions that shape the whole system: where work is queued, how processes are split, how the
per-channel rate limit holds across replicas, and where delayed work waits.

## Steps

1. Compared four queueing options against one scenario (a flash sale of ten thousand high-priority SMS, a
   cancellation and a scheduled message): PostgreSQL only, PostgreSQL + RabbitMQ, Kafka, Redis with asynq.
   Chose PostgreSQL as the source of truth and RabbitMQ for dispatch, joined by a transactional outbox (ADR 0003).
2. Chose two binaries, `cmd/api` and `cmd/worker`, built into one image (ADR 0004).
3. Showed that a per-replica limiter breaks the 100 messages per second limit with several replicas, and chose a
   single active consumer per channel queue with an in-process token bucket (ADR 0005).
4. Chose to keep retries and scheduled notifications in PostgreSQL (`next_attempt_at`) with a scheduler that
   publishes them when due, over RabbitMQ wait queues and the delayed-message plugin (ADR 0006).

## Verification

No code yet; the decisions were reviewed by the owner.

## Follow-ups

Install the toolchain and connect the remote: block 04. The delivery and retry design: block 05.
