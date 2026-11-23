# 0024. The circuit breaker

- Status: Accepted
- Date: 2026-11-23
- Amends: [0011](0011-notification-domain.md) (adds the transition `sending` → `queued`)

## Context

ADR 0007 put a circuit breaker per channel in front of the provider: five transient failures in a row open it for
30 seconds, then one probe decides; rejections and `429` are not counted; while it is open the consumer takes no
message, so the queue keeps them and no attempt is spent. Building it raised two questions: where in a message's
handling the breaker is asked, and what happens to a notification already claimed when the breaker refuses it.

| Where the breaker is asked | For | Against |
|---|---|---|
| Before the claim | The claim is never undone | A message with nothing to send (cancelled, finished) would pass as the half-open probe and close the breaker without a word from the provider |
| **After the claim** | Only real sends count, probe included | When the breaker opened between the loop's look at it and the send, the claim has to be undone |

## Decision

- **`sony/gobreaker/v2`'s two-step breaker**, one per consumer and so per channel: `MaxRequests` 1, `Timeout`
  `BreakerOpen`, tripping on `BreakerFailures` consecutive failures. A send reports its outcome after it ends:
  success closes or keeps closed, a transient failure other than `429` counts, and a permanent rejection, an alert or
  a `429` is excluded (`IsExcluded`), counting for neither side.
- **The loop follows the breaker's state** for how many sends may be in flight: the concurrency while closed, the one
  probe while half-open, none while open. Not taking messages is the backpressure of ADR 0009: RabbitMQ stops at the
  prefetch count and the rest wait in the queue. The loop looks at an open breaker every 100 ms to resume when it
  lets the probe through.
- **The breaker is asked after the claim.** If it refuses, because it opened meanwhile, the claim is undone in one
  conditional `UPDATE` (`ReleaseClaim`): the notification goes back to `queued` with its attempt given back, and the
  message is returned to the queue (`nack` with requeue). The domain gains the transition `sending` → `queued` for
  it.
- **State changes are logged** at warning level with the channel.

## Consequences

- A ten-minute outage costs each channel five attempts, not eight attempts of every message in flight.
- Recovery needs one successful probe; a half-open breaker that fails opens again for another interval.
- A breaker is per process. With single active consumer there is one active consumer per channel (ADR 0005), so it is
  in effect per channel for the whole system.

### Deliberately not done

| Not done | Why not |
|---|---|
| A breaker shared across processes (in Redis) | Single active consumer already makes one process the channel's sender. |
| Counting `429` as a failure | A rate limit is the provider working as designed; the retry honours its `Retry-After` instead (ADR 0007). |
| Holding a claimed notification until the breaker closes | It would hold a lease, and a slot, for an unknown time; giving it back to the queue costs nothing. |
