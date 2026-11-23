# 19. Platform: RabbitMQ

- Date: 2026-11-23
- Commits: `f1fb6b4`
- Decisions: [ADR 0020](../adr/0020-rabbitmq-platform.md)

## Goal

Give the worker what it needs from RabbitMQ: the topology of ADR 0012, publishing that reports a message only once
it sits in a queue, and consuming with backpressure, each proved against a real broker.

## Steps

1. Added `rabbitmq/amqp091-go` v1.17.0 and the testcontainers RabbitMQ module v0.45.0 with the owner's approval.
2. `testenv.RabbitMQURL`: one RabbitMQ 4.4.1 container per test process, from the image Compose runs.
3. Tests first in `internal/platform/rabbitmq`: declaring twice, confirmed publishing into the right queues, an
   unroutable message, a negative confirm (from a queue that refuses messages when full), message properties,
   priority order, single active consumer with failover, dead-lettering, a queue that exists with other settings,
   an ended context, an unreachable broker.
4. `Connect`, `Topology` and `Declare`, `Publisher` (confirm mode, mandatory, batches of 256 with a returns buffer
   of the same size), `Consume`. A helper of mine to split slices gave way to the standard `slices.Chunk`.
5. One test declared a transient auto-delete queue, which RabbitMQ 4 refuses; it now declares a durable one.
6. Recorded ADR 0020.

## Verification

- `go test -race ./internal/platform/rabbitmq/`: every test passes in about 7 s, 81.5% coverage; the lines left
  are the client's error returns.
- Turning the mandatory flag off made the unroutable test fail with `Publish = <nil>`: the silent loss the flag
  prevents is real.
- `golangci-lint`: 0 issues.

## Follow-ups

Block 20, the dispatch module: the outbox relay publishing with this layer, and the scheduler with the reaper of
ADR 0012.
