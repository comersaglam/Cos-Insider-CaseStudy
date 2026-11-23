# 05. Delivery and retry design

- Date: 2026-11-23
- Commits: `67619f9`
- Decisions: [ADR 0007](../adr/0007-delivery-and-retry.md)

## Goal

Design what happens when a send fails, the part of the assessment left to the candidate: which failures are
retried, how often, what happens during a provider outage, and where final failures go.

## Steps

1. Classified every provider outcome as success, permanent or transient, with `401`/`403` treated as a
   configuration alarm and timeouts retried under the same idempotency key.
2. Chose one retry policy, exponential backoff with full jitter (base 10s, cap 15m, 8 attempts, configurable), over
   per-channel policies and a fixed schedule.
3. Chose to record every attempt in `delivery_attempts`, to keep final failures as `failed` in PostgreSQL, and to
   add `POST /v1/notifications/{id}/retry` to requeue them.
4. Chose a circuit breaker per channel that stops pulling from RabbitMQ while open, so an outage spends no attempts.

## Verification

No code yet; the design was reviewed by the owner.

## Follow-ups

The package layout: block 06.
