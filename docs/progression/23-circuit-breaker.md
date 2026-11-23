# 23. Circuit breaker and pausing the pull

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0024](../adr/0024-circuit-breaker.md), applying [ADR 0007](../adr/0007-delivery-and-retry.md)

## Goal

Stop sending to a provider that is down without spending the notifications' attempts, and resume with one probe.

## Steps

1. No question needed the owner. Added `sony/gobreaker/v2` v2.4.0, approved earlier. Chose to ask the breaker after
   the claim, so a message with nothing to send never passes as the probe, and to undo the claim when the breaker
   refuses.
2. Tests first: the transition `sending` → `queued`; `Store.ReleaseClaim` against PostgreSQL (attempt given back, a
   stale release ignored); the consumer opening after three failures and taking nothing more (three sends, three
   claims), probing and closing, ignoring rejections and `429`, and releasing and requeueing a claim the breaker
   refused. The fake acknowledger now records requeues.
3. The consumer: gobreaker's two-step breaker, the loop's limit set by the breaker's state (all, one, none), a look
   every 100 ms while it is not closed, `breakerReport` mapping outcomes, and the `requeue` action.
4. Recorded ADR 0024.

## Verification

- `go test -race ./internal/delivery/`: green, 92.2% coverage; the notification tests for statuses, claims and
  releases green. `golangci-lint`: 0 issues.
- Removing `IsExcluded` made the test with rejections and `429` fail: the breaker opened on answers that say nothing
  of an outage.

## Follow-ups

Block 24, the worker's composition root: configuration, the topology, every loop under one `errgroup`, the ordered
shutdown, the `worker` service in Compose, and the end-to-end test from the API to the provider.
