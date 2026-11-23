# 0020. The RabbitMQ platform layer: topology, publishing and consuming

- Status: Accepted
- Date: 2026-11-23

## Context

ADR 0003, 0005 and 0012 decided what RabbitMQ carries and how: a direct exchange, one durable queue per channel
with three priorities and a single active consumer, a dead-letter queue, persistent messages and publisher
confirms. This record fixes how the code does it with `rabbitmq/amqp091-go`, the RabbitMQ team's client.

A publisher confirm alone does not prove a message reached a queue: RabbitMQ confirms a message it routed nowhere,
for example to a routing key no queue is bound with. The relay would then mark the outbox row published and the
notification would never be sent.

## Decision

- **`internal/platform/rabbitmq` knows no business names.** It declares a `Topology` it is given; the names
  (`notifications`, `notifications.sms`, ...) are set where the worker is assembled, so the relay and the consumer
  share them without importing each other (ADR 0008).
- **`Declare`** creates the direct exchange, a fanout dead-letter exchange with one queue of the same name, and the
  work queues: durable, `classic`, `x-max-priority`, `x-single-active-consumer`, `x-dead-letter-exchange`, each
  bound by its routing key. Declaring again changes nothing, so every process declares on start; a queue that
  exists with other settings is an error, which stops the process instead of letting it run against a topology it
  does not expect.
- **`Publisher`** owns a channel in confirm mode. Every message is persistent, `application/json`, carries the
  notification's ID as its message ID, a priority, a timestamp and the correlation ID in the `X-Correlation-ID`
  header. Publishes are **mandatory**: a message no queue takes comes back and `Publish` fails with
  `ErrUnroutable`; a negative confirm fails with `ErrNotConfirmed`. Messages go out in batches of 256, each batch's
  confirms awaited before the next; the channel that receives returned messages holds 256, so it can never fill
  and block the client's reader. On any error the caller publishes everything again; consumers drop duplicates.
- **`Consume`** sets the prefetch count and consumes with manual acknowledgement: the backpressure of ADR 0009.
- **`Connect`** names the connection for the management UI and sets a 10-second heartbeat. A lost connection is
  not re-established; the process stops and is restarted (ADR 0009).
- **Tests run against RabbitMQ 4.4.1 in a container** (`testenv.RabbitMQURL`), one per test process; each test
  declares names of its own.

## Consequences

- A message is reported published only when it sits in a queue on disk.
- Priority order, failover between consumers, dead-lettering and negative confirms are tested against the broker,
  not assumed.
- A routing mistake surfaces as an error at the first publish instead of as notifications that never leave.

### Deliberately not done

| Not done | Why not |
|---|---|
| Reconnecting on our own | Crash-only supervision restarts the process and single active consumer moves the queue; reconnect logic is a known source of subtle bugs (ADR 0009). |
| Declaring the topology from a definitions file loaded into RabbitMQ | The code that uses the topology declares it, so the two cannot drift. |
| Transient or auto-delete queues | Work must survive a restart; RabbitMQ 4 also refuses transient non-exclusive queues. |
| One channel per publish | Opening a channel is a round trip; one confirm-mode channel per publisher is the client's intended use. |
