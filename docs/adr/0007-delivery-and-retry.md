# 0007. Delivery and retry

- Status: Accepted
- Date: 2026-11-23

## Context

The provider (simulated with webhook.site) answers `202 Accepted` on success. It can also reject a message for
good, fail for a moment, or be down for minutes. The assessment leaves the delivery and retry design to the
candidate; the business needs reliable delivery under burst traffic and visibility into why a message was or was
not sent.

## Decision

### Every outcome is classified

| Provider outcome | Class | Result |
|---|---|---|
| `202` | success | status `sent`, the provider's `messageId` stored |
| `400`, `404`, `422` | permanent | status `failed` at once: the same request would fail again |
| `401`, `403` | permanent, alerting | status `failed`, logged at error level: our configuration is wrong |
| `408`, `429` | transient | retried; on `429` the wait is at least the provider's `Retry-After` |
| `5xx`, network error, timeout | transient | retried; a timeout is ambiguous (the message may have arrived), so every attempt carries the same idempotency key |

### One retry policy: exponential backoff with full jitter

The wait before attempt *n + 1* is a random duration in `[0, min(cap, base × 2^(n−1))]`, with defaults
`base = 10s`, `cap = 15m`, `maxAttempts = 8`, each set by configuration. The wait is stored in
`next_attempt_at` ([0006](0006-delayed-work-waits-in-postgresql.md)). With these defaults the last attempt starts
at most about 21 minutes after the first.

Full jitter is the variant measured best by the AWS Architecture Blog ("Exponential Backoff and Jitter"): when a
burst fails together, its retries spread out instead of striking the provider again in the same second.

### Every attempt is recorded

Each attempt is a row in `delivery_attempts`: attempt number, HTTP status, class, error message, latency and time.
It answers "why was this message not sent?" for both the API and the team.

### Final failure stays in PostgreSQL

When attempts run out, the notification moves to `failed` and stays in PostgreSQL, which serves as the dead letter
store. `POST /v1/notifications/{id}/retry` puts a failed notification back to `queued` with a fresh attempt
budget, after someone has looked at it. `notifications.dlq` in RabbitMQ only receives messages that cannot be
processed at all, such as an unreadable body.

### A circuit breaker per channel stops pulling during an outage

The active consumer of each channel ([0005](0005-rate-limit-with-single-active-consumer.md)) wraps provider calls
in a circuit breaker (`sony/gobreaker`):

- **closed**: calls flow; transient failures are counted, permanent ones and `429` are not;
- **open** after 5 consecutive transient failures: no provider call for 30 seconds. The consumer stops taking
  messages; once its unacknowledged messages reach the prefetch count, RabbitMQ stops delivering, and the rest
  wait in the queue. **A message held by an open breaker does not spend an attempt**;
- **half-open** after the wait: one call is let through; success closes the breaker, failure opens it again.

Thresholds and the open interval are configuration.

### Rejected

| Option | Why not |
|---|---|
| A policy per channel | Better fitted (an SMS code is useless after minutes), but triples the settings and tests; the single policy's values are configuration, so per-channel values can be added without a design change. |
| A fixed retry schedule (10s, 1m, 5m, ...) | Easy to read, but without jitter a failed burst retries in lockstep. |
| No circuit breaker | A ten-minute outage would burn every message's attempts and fail them in bulk, while flooding a dead provider. |
| Returning messages to PostgreSQL while the breaker is open | Thousands of needless writes, and messages bounce between the two stores. |

## Consequences

- An outage shorter than the attempt window loses nothing, and a long one does not fail messages that never got
  a chance.
- The breaker's state and every transition are exposed as metrics and logged.
- Expiry of stale messages (an SMS code that is no longer useful) is out of scope; it would be an optional
  `expiresAt` checked before sending.
