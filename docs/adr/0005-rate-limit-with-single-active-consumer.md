# 0005. Enforce 100 messages per second per channel with a single active consumer

- Status: Accepted
- Date: 2026-11-23

## Context

At most 100 messages per second may be sent per channel, across the whole system. RabbitMQ does not rate-limit
consumers: prefetch (QoS) bounds how many messages are in flight, not how many per second. If three worker
replicas each consumed a channel's queue with an in-process limit of 100 per second, the channel would send up
to 300.

| Option | Why it was or was not chosen |
|---|---|
| **Single active consumer per channel queue, with an in-process token bucket** | Chosen: exact system-wide limit, no new infrastructure, failover handled by the broker. |
| A distributed limiter in Redis (GCRA) | The industry standard when every replica must consume; it adds Redis and a network call per send, and unacknowledged messages pile up while waiting for tokens. |
| Dividing the rate by the replica count | Wrong as soon as the replica count changes. |
| A per-channel leader elected with a PostgreSQL advisory lock | Correct, but reimplements what RabbitMQ's single active consumer already provides. |

## Decision

- Each channel queue is declared with `x-single-active-consumer: true`. Every worker replica subscribes to every
  channel queue, and RabbitMQ delivers a queue's messages to one of them at a time. When that consumer's
  connection drops, RabbitMQ moves delivery to the next subscriber.
- The active consumer takes a token from a `golang.org/x/time/rate` limiter (100 per second, burst sized from
  configuration) before each send, and hands sends to a bounded pool of goroutines so that slow provider calls do
  not lower throughput.
- The prefetch count is set to the size of that pool, so the consumer never holds more messages than it can work
  on.

## Consequences

- The limit holds whatever the number of replicas; adding replicas adds standby capacity, not throughput.
- One process carries all traffic of a channel. RabbitMQ does not balance channels across replicas, so all three
  channels may land on one process: at most 300 provider calls per second, well within one Go process.
- Failover takes as long as RabbitMQ needs to notice the lost connection (heartbeat timeout, seconds).
  Unacknowledged messages of the failed consumer are redelivered; idempotency ([0003](0003-postgresql-and-rabbitmq-with-outbox.md))
  prevents a second send.
