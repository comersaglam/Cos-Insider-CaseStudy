# 0023. The consumer: claiming, sending, recording, retrying

- Status: Accepted
- Date: 2026-11-23
- Amends: [0009](0009-concurrency-model.md) (how the number of sends in flight is bounded)

## Context

ADR 0005, 0007, 0009, 0012 and 0014 decided what each channel's consumer does: pace sends at 100 per second, run
up to `SEND_CONCURRENCY` of them at once, claim each notification under a lease before sending, record every
attempt, retry with full jitter, dead-letter what cannot be read, and leave acknowledgements to one goroutine.
Building it settled how, and tested one claim of ADR 0009 that did not hold.

ADR 0009 bounded the sends with a semaphore that each send released when it ended, and guarded against a deadlock
by buffering the results channel to the concurrency. A test with one slot and twenty waiting messages deadlocked:
a send put its result in the buffer and freed its slot; the loop, instead of reading the result, took the next
message and filled the slot; that send then found the buffer full and waited, holding the slot the loop was now
waiting for. Results waiting in the buffer and sends in flight together can exceed the concurrency, so the buffer
alone does not prevent the deadlock.

## Decision

- **The loop counts what it has not settled.** `inFlight` rises when a send starts and falls when the loop reads
  and settles its result, not when the send ends. While it equals `Concurrency`, the loop takes no message: the
  `select` reads from a `nil` channel, which is never ready, and waits only for results. Results never outnumber
  the buffer, so a send never waits to hand one over, and the loop never waits for a slot. No semaphore channel
  is needed.
- **Per message**, the loop waits for the rate limiter (`x/time/rate`, burst 1, so sends are spaced evenly), then a
  goroutine of the message's own parses it, claims the notification, sends it, and records the outcome. Only the
  loop acknowledges or rejects.
- **Claiming** (ADR 0012) spends an attempt and sets a lease of the provider timeout plus a margin. A notification
  not there to be sent (cancelled, finished, waiting, or in flight under another copy's live lease) is
  acknowledged without a send; one that does not exist is dead-lettered. A claim that takes over an expired lease
  is logged as a possible duplicate.
- **Recording**: `RetryPolicy.After` turns the outcome into the attempt's record and the next status: sent;
  failed (`provider_rejected`, or `attempts_exhausted` after the last attempt); or retrying at a full-jitter delay,
  at least the provider's `Retry-After`. The record and the status change are one transaction. An outcome that
  arrives after its claim was taken over is kept in the record and does not overwrite the newer state.
- **Every handled message is acknowledged**, retries included: a retry waits in PostgreSQL (ADR 0006), not in
  RabbitMQ. A message that cannot be read is rejected into the dead-letter queue, its notification failed as
  `unprocessable_message` when its message ID names one (ADR 0014).
- **A panic while sending** is recovered as a transient outcome: it costs the notification an attempt, not the
  process. A database failure stops the consumer (ADR 0009); the message stays unacknowledged.
- **Stopping**: sends in flight run on a context that is not cancelled, each bounded by the provider timeout, and
  are recorded and acknowledged within `ShutdownTimeout`; messages not yet taken stay unacknowledged and are
  delivered again.
- **The reaper** runs in the scheduler before each round: notifications whose lease ran out return to `retrying`,
  due at once, or fail when no attempt is left; the lost attempt is recorded as `unknown`.
- **Retrying by hand gives back every attempt**: the single and bulk retry reset `attempts` and the failure reason.

## Consequences

- The concurrency bound holds exactly (a test sees three sends at once with a concurrency of three, never four)
  and cannot deadlock however messages and results interleave.
- Every attempt is in `delivery_attempts` with its outcome, status, error and duration; notifications show
  `attempts`, `failureReason` and `providerMessageId`.
- A crash loop ends: each claim spends an attempt, and the last one fails the notification.

### Deliberately not done

| Not done | Why not |
|---|---|
| Nacking a transient failure back to RabbitMQ | The retry's delay is per message, with jitter, and waits in PostgreSQL, where it can be cancelled (ADR 0006). |
| A burst above one for the limiter | A burst lets a second start with a spike above the rate; the brief caps the rate per second. |
| Cancelling sends in flight on shutdown | A cancelled send is the ambiguous window of ADR 0012; letting it finish within the provider timeout is cheaper than a possible duplicate. |
