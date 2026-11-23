# 0009. Concurrency model

- Status: Accepted
- Date: 2026-11-23

## Context

The worker must send up to 100 messages per second per channel to a provider that takes hundreds of milliseconds
to answer, shut down without losing work, and stay simple enough that every goroutine's lifetime is obvious.

By Little's Law, the work in flight equals the arrival rate times the time each item takes. At 100 messages per
second and about 300 ms per provider call, about 30 sends must be in flight at once to reach the limit; a single
sequential sender would manage about 3 per second.

## Decision

### The API starts no goroutines of its own

`net/http` serves each connection on its own goroutine. Batch creation is one transaction with `COPY`, which needs
no parallelism. The API's `main` only runs the server and its graceful shutdown.

### The worker's goroutines, each with one owner

```
main: signal.NotifyContext cancels ctx on SIGTERM; errgroup.WithContext owns every goroutine below
 ├─ relay        1   outbox → RabbitMQ; woken by LISTEN/NOTIFY, with a ticker as fallback
 ├─ scheduler    1   due retries and scheduled notifications → outbox, on a ticker
 ├─ ops server   1   /metrics, /healthz
 └─ per channel (sms, email, push):
     consumer    1   owns the AMQP channel, the circuit breaker and the rate limiter
       └─ senders    one goroutine per message, at most SEND_CONCURRENCY at once
```

### Senders: one goroutine per message, bounded by a semaphore

- For each delivery, the consumer waits for the circuit breaker and the rate limiter, acquires a slot from a
  semaphore (a buffered channel of size `SEND_CONCURRENCY`, default 32) and starts `go send(...)`. The sender
  releases its slot when done.
- `SEND_CONCURRENCY` is configuration, per process, the same for every channel. The RabbitMQ prefetch count is set
  to the same value, so a consumer never holds more messages than it can work on.
- Chosen over a fixed pool of long-lived workers because nothing idles, and it is the pattern recommended in
  "Rethinking Classical Concurrency Patterns" (Bryan C. Mills, GopherCon 2018).

### The AMQP channel has a single owner

Only the consumer goroutine touches its AMQP channel. A sender reports its outcome on a `results` channel and the
consumer sends the acknowledgement. RabbitMQ's client guidance advises against sharing a channel between threads,
and acknowledging a message twice closes the channel with `PRECONDITION_FAILED`.

**Deadlock guard**: `results` is buffered to `SEND_CONCURRENCY`. Otherwise the consumer could block acquiring a slot
while every sender blocks handing it a result, and neither would move.

### Crash-only supervision

Every goroutine runs under one `errgroup`. A fatal error cancels the shared context, the others stop cleanly, and
the process exits non-zero; Docker's restart policy (`unless-stopped`) starts it again. Meanwhile RabbitMQ's single
active consumer has already moved the process's channels to another replica, and unacknowledged messages are
redelivered. There is no reconnect logic: `pgxpool` already replaces broken database connections, and a lost
RabbitMQ connection is a fatal error.

### Shutdown order

1. SIGTERM cancels the context.
2. Consumers stop taking deliveries.
3. In-flight senders finish, bounded by `SHUTDOWN_TIMEOUT`.
4. Consumers send the last acknowledgements; relay and scheduler finish their current round.
5. The AMQP connection closes, then the database pool.

Anything still unacknowledged is redelivered to another consumer, and idempotency prevents a second send.

### Queue depth is read on scrape

Queue depth is collected when Prometheus scrapes `/metrics` (a custom `prometheus.Collector`), not by a goroutine
that refreshes a gauge, so it is always current and needs no extra goroutine.

## Consequences

- Every goroutine has an owner and a way to stop; `go test -race` and shutdown tests check them.
- A RabbitMQ outage restarts worker processes instead of being bridged in process; delivery resumes when the
  broker returns.

### Rejected

| Option | Why not |
|---|---|
| A fixed pool of long-lived sender goroutines | Equally correct; idle goroutines and a separate jobs channel buy nothing over a semaphore. |
| One sequential sender per channel | About 3 messages per second at 300 ms latency, far below the limit. |
| Senders acknowledging directly | Shares the AMQP channel between goroutines; a double acknowledgement closes it. |
| In-process supervisors and reconnect logic | Reconnecting and redeclaring topology is complex and error-prone, and the restart it avoids is already safe. |

## Future work

These are deliberately not part of the first version.

- **Adaptive concurrency.** The needed concurrency follows provider latency (Little's Law: at 100 per second,
  1 s latency needs 100 in flight). The semaphore's size could follow observed latency within configured bounds.
  Rising latency often means an overloaded provider, where more concurrency makes things worse, so the established
  algorithms (AIMD, the gradient limiters of Netflix's `concurrency-limits`, Envoy's adaptive concurrency filter)
  shrink the limit when latency rises and grow it slowly when it falls. Rather than a raw `rate × latency` formula,
  one of those is the starting point.
- **Horizontal scaling.** The API is stateless and scales out behind a load balancer, for example as an ECS or
  EKS service with autoscaling on CPU or request rate. Worker replicas add availability, not throughput: under
  [ADR 0005](0005-rate-limit-with-single-active-consumer.md) each channel is consumed by one process, capped at
  100 per second. Raising per-channel throughput would need queues sharded per channel (each a single active
  consumer with a share of the limit) or a distributed limiter, recorded as a new ADR superseding 0005.
