# 37. Retries and the breaker under load

- Date: 2026-11-24
- Commits: this block's commit
- ADR: [0036](../adr/0036-retries-and-the-breaker-under-load.md)

## Goal

Fix the livelock block 35's load test found, as the owner chose: retries at the lowest priority, and a breaker that
opens on a failure ratio.

## Steps

1. Tests first, each seen failing: `TestHealthySendsAreNotHeldBehindFailingRetries` (worker end to end: 17 of 200
   healthy SMS sent in 10 s), `TestRetriesArePublishedAtTheLowestPriority` (relay: the retry went at 3),
   `TestBreakerOpensOnAFailureRatioNotARun`.
2. `OutboxEntry.QueuePriority`: 0 once attempts were spent; the outbox query reads `attempts`.
3. The breaker: a rolling window (`BREAKER_WINDOW`, ten buckets), `BREAKER_MIN_REQUESTS`, `BREAKER_FAILURE_PERCENT`
   in place of `BREAKER_FAILURES`.
4. Run 2 repeated on empty volumes: every healthy SMS sent; new screenshot.

## Verification

delivery, dispatch, notification and cmd/worker green with `-race`; golangci-lint clean.

## Follow-ups

- Held messages can still delay new healthy traffic up to 16 minutes while the breaker is open (ADR 0036, "a limit
  left"); found by a last manual check, left for later.
