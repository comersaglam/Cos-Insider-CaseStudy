# 21. Delivery: provider client

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0022](../adr/0022-provider-client.md), applying [ADR 0007](../adr/0007-delivery-and-retry.md)

## Goal

Make one attempt to send a notification to the provider and say, in one value, what came of it.

## Steps

1. No question needed the owner; the tests use a fake provider (`httptest.Server`), never webhook.site (ADR 0002).
   The owner's webhook.site URL is needed when the worker runs, in block 24.
2. Tests first in `internal/delivery`: the request (exactly the contract's members, `Idempotency-Key`,
   `X-Correlation-ID`), every status's class, `Retry-After` in seconds and as a date, a timeout, an unreachable
   provider, an acceptance that cannot be read, a 10 MB answer, an ended context.
3. `delivery.Client` and `Outcome`: classification per ADR 0007 and ADR 0022, no redirects followed, answers read
   up to 64 KB, 64 idle connections kept to the provider.
4. Recorded ADR 0022.

## Verification

`go test -race ./internal/delivery/`: every test passes, 95.7% coverage. `golangci-lint`: 0 issues.

## Follow-ups

Block 22, the consumer: rate limit, the claim with its lease, sending, recording attempts, retrying with backoff,
the reaper, dead-lettering, and the semaphore of ADR 0009.
