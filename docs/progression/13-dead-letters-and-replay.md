# 13. Dead letters and replay

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0014](../adr/0014-dead-letters-and-replay.md), amending [ADR 0007](../adr/0007-delivery-and-retry.md)
  and [ADR 0011](../adr/0011-notification-domain.md)

## Goal

Decide how notifications that could not be delivered are replayed, one or many at a time, and what happens to a
notification whose message cannot be processed.

## Steps

1. Reviewed what ADR 0007 had decided (PostgreSQL as the dead-letter store, a retry endpoint for one
   notification, the dead-letter queue for unreadable messages) and found two gaps: no bulk replay, and a
   notification left `queued` forever when its message was dead-lettered.
2. Chose bulk replay by filter (`POST /v1/notifications/retry`: channel, batch, a required time window,
   `dryRun`, a cap per request) over replay per batch or one at a time; replay always starts from PostgreSQL.
3. Chose to mark a notification `failed` (`unprocessable_message`) when its message cannot be processed but its
   ID can be read; messages without an ID stay in the dead-letter queue behind a depth metric and an alert.
4. Added the transition `queued` → `failed`: the test first (it failed on the new pair), then the table in
   `internal/notification/status.go`.
5. Checked that every decision discussed so far is in an ADR. Admission control had been recorded only in the
   roadmap; ADR 0012 now lists it under what is deliberately not done.
6. Recorded ADR 0014; the roadmap inserts this block as 13 and moves later blocks up by one, adding the bulk
   replay (17), the failure reason column (14), the dead-letter handling (22) and its metric (25).

## Verification

`make check` passes; `TestStatusCanTransitionTo` covers the new transition. Every link in the tracked documents
resolves.

## Follow-ups

Block 14, the database and store: migrations, the PostgreSQL pool, sqlc, the testcontainers helper and the
notification store, with the notification's ID type chosen.
