# 0036. Retries and the breaker under load

- Status: Accepted
- Date: 2026-11-24
- Amends: [0007](0007-delivery-and-retry.md) (the breaker's trip), [0024](0024-circuit-breaker.md) (its settings)

## Context

Block 35's second load run ([ADR 0035](0035-dashboards-and-load-tests.md)) found a livelock. Of 1511 SMS, 51
high-priority ones went to a number the provider answers `503`. Their retries came due together and were published at
high priority, so they failed five in a row and opened the SMS breaker, though the provider was healthy for every
other number. Each half-open probe was then one of these retries, since a retried high-priority message outranks the
healthy normal and low ones waiting; it failed, and the breaker opened for another 30 seconds. 968 healthy SMS waited
behind them, for what would have been hours.

Two rules met there, each sound alone: a retry keeps its notification's priority, and five failures in a row mean the
provider is down.

| Option | For | Against |
|---|---|---|
| Retries at the lowest priority | Probes and the queue's head are fresh notifications, so a failing few cannot hold the rest | A high-priority notification that failed once waits behind fresh ones |
| A breaker that opens on a failure ratio | Failures bunched together among many successes no longer look like an outage | Alone, once only retries are left, they can still open it and be its probes |
| **Both** | Neither failure mode is left | Two changes, and three settings in place of one |

## Decision

- **An automatic retry is published at AMQP priority 0**, below every fresh notification
  (`OutboxEntry.QueuePriority`): an outbox entry whose notification has spent attempts is a retry. A retry by hand
  gives the attempts back (ADR 0018), and so the notification's own priority.
- **The breaker opens on a failure ratio over a rolling window**: once, over the last `BREAKER_WINDOW` (10 s, in ten
  buckets), at least `BREAKER_MIN_REQUESTS` (20) sends were made and `BREAKER_FAILURE_PERCENT` (50) of them failed
  for now. What counts is unchanged: rejections and `429` are excluded. At 100 sends a second, 20 is a fifth of a
  second's traffic, and an outage of the provider still opens it within one.
- **A regression test** in the worker's end-to-end tests sends 20 high-priority SMS the provider refuses and 200
  healthy normal ones, and expects the healthy ones sent within ten seconds; before the fix 17 were.

## Consequences

- A notification the provider keeps refusing spends its attempts behind healthy traffic instead of ahead of it.
- A provider that refuses one recipient no longer stops a channel; one that is down for all of them still does.
- A high-priority notification that failed once is not urgent any more in the queue's eyes; its retry delay already
  set it back by seconds to minutes.
- **Run 2 again** (seed 2): all 1488 healthy SMS sent in 17 s; the breaker opened after them, when only the refused
  recipient's retries were left, and those 243 waited without spending attempts.
- **Held retries are given back before a probe.** The consumer holds up to its prefetch count (32) of delivered
  messages. If the breaker opened while they were all such retries, new healthy SMS arriving later were queued
  behind those 32, each a failing probe 30 seconds apart: up to 16 minutes, found by a last manual check. Now, while
  the breaker is half-open, a retry that was not delivered before (priority 0, not redelivered) is requeued once:
  RabbitMQ hands out the highest priority first, so a fresh notification is the probe if one waits, and the retry
  comes back redelivered and probes if none does (`TestBreakerProbesWithAFreshMessageBeforeARetry`).

### Deliberately not done

| Not done | Why not |
|---|---|
| A breaker per recipient or per error | The provider's health is the question; one recipient's failures are the retry policy's. |
| A priority between low and the retry | Three levels and "after them" are enough to explain and to test. |
