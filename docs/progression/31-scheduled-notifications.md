# 31. Scheduled notifications

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0031](../adr/0031-scheduled-notifications.md)

## Goal

Let a client schedule a notification, the first of the brief's optional bonuses, on the mechanism ADR 0006 built.

## Steps

1. Chose the rules, as the owner asked for limits ("choose a reasonable number and tell me why"): up to 30 days
   ahead; a time already come is sent at once; the time is kept and shown after the notification is queued.
2. Tests first for the draft: the horizon, the starting status, the fingerprint with a schedule, one instant in two
   offsets as one fingerprint, and the fingerprint of a request without a schedule pinned to its value before this
   block, so stored idempotency keys still match. `Draft.Validate` now takes `now`.
3. `migrations/0007`: `scheduled_at` with a CHECK while scheduled. The test helper that forces a status keeps to
   the CHECK.
4. Tests first for the store: a scheduled notification has no outbox row and is due at its time, the scheduler
   queues it and its time stays; a cancelled one is never queued; a batch schedules item by item.
5. The handler takes `scheduledAt` and shows it; a malformed one is `400 invalid_json`.
6. End to end: the worker sends a notification scheduled 1.5 s ahead only after its time; the API stores, lists,
   cancels and refuses one too far ahead, every answer checked against the contract.
7. A mutation that wrote outbox rows for scheduled notifications failed the store's test but not the worker's:
   the consumer claims only queued notifications, so the message was dropped and the scheduler queued it on time.
   Recorded in ADR 0031 as defense in depth.
8. The contract documents `scheduledAt` in the create, batch items and the notification, with examples taken from
   the running stack, and the 30-day limit in its table.

## Verification

`internal/notification`, `internal/dispatch`, `internal/delivery`, `internal/platform/postgres`, `api/contract`,
`cmd/api` and `cmd/worker` with `-race` green; `make verify` and `golangci-lint` clean.

## Follow-ups

Block 32, templates with variable substitution.
