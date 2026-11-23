# 22. Delivery: consumer, rate limit, sending and retry

- Date: 2026-11-23
- Commits: `64d1b66`
- Decisions: [ADR 0023](../adr/0023-the-consumer.md), amending [ADR 0009](../adr/0009-concurrency-model.md);
  applying ADR 0005, 0007, 0012 and 0014

## Goal

Send each channel's notifications: at most 100 per second, at most `SEND_CONCURRENCY` at once, each claimed under a
lease, every attempt recorded, transient failures retried with backoff, lost work reaped, unreadable messages
dead-lettered.

## Steps

1. Did the part that needed no new dependency first. `migrations/0005_delivery`: `attempts`, `claimed_until`,
   `failure_reason`, `provider_message_id` with `CHECK`s tying the lease to `sending` and the reason to `failed`,
   and `delivery_attempts`. Test helpers that set statuses by SQL now satisfy them.
2. Store, tests first against PostgreSQL: `Claim` (a first claim, a second under a live lease refused, a takeover of
   an expired lease, the last attempt), `FinishAttempt` (sent, retrying, failed; an outcome after a takeover kept
   but not applied), `MarkUnprocessable`, `ReapExpiredClaims`, `Attempts`; retry now gives back every attempt.
   Notifications gain `Attempts`, `FailureReason` and `ProviderMessageID`, shown by the API.
3. `delivery.RetryPolicy`, tests first: full jitter within its ceiling over 500 draws per attempt, no overflow at
   attempt 100, `Retry-After` honoured. gosec flagged `math/rand` for the jitter, a false positive; it is silenced
   on its line with the reason, the one exception `docs/development.md` now names.
4. The reaper joined the scheduler, tested against PostgreSQL with a lost worker's notification.
5. Asked for the remaining dependencies; the owner approved `golang.org/x/time`, `golang.org/x/sync` and
   `sony/gobreaker/v2`. Added `x/time` v0.16.0.
6. `delivery.Consumer`, tests first with a fake acknowledger, store and sender: send, record and acknowledge;
   retrying; unreadable messages, nothing to send, a missing notification; a lost claim; a panic; a database
   failure; the concurrency bound; the deadlock case; the rate; stopping with a send in flight.
7. The deadlock test failed against the design of ADR 0009: a semaphore released by the send and a results buffer
   of the concurrency deadlocked with one slot. The loop now counts sends until it settles their result and takes
   no message at full concurrency (a `nil` channel in the `select`). Recorded as ADR 0023, amending ADR 0009.
8. An integration test against RabbitMQ, PostgreSQL and a fake provider: each answer leads to its status, a
   cancelled notification is never sent, a message published twice is sent once.

## Verification

- Package by package with `-race`: `notification` (91.8%), `delivery` (91.1%), `dispatch` green; migration 0005
  rolls back and reapplies; `cmd/api` and `httpapi` green. `golangci-lint`: 0 issues.
- At most three sends ran at once with a concurrency of three; sixty sends at fifty per second took over a second
  and no second held more than fifty-one.

## Follow-ups

Block 23, the circuit breaker: `sony/gobreaker/v2` per channel, and the consumer taking no message while it is open.
