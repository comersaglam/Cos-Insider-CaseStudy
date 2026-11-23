# 0031. Scheduled notifications

- Status: Accepted
- Date: 2026-11-23
- Builds on: [0006](0006-delayed-work-waits-in-postgresql.md) (delayed work waits in PostgreSQL)

## Context

The brief's bonus asks for scheduled notifications. ADR 0006 already made delayed work wait in PostgreSQL: a row in
`scheduled` or `retrying` carries `next_attempt_at`, and the scheduler queues it when that time comes. What was left
is the API: how a client asks for a time, which times are allowed, and what the notification shows afterwards.

Worked example: at 20:00 UTC a client sends `scheduledAt: "2026-11-24T09:00:00+03:00"`, 06:00 UTC the next day.

| Question | Choice | Alternative left out |
|---|---|---|
| A time that has already come | sent at once, as SendGrid's `send_at` does | refused with 422: a client clock a few seconds behind would fail its requests |
| How far ahead | at most 30 days, refused beyond with `/scheduledAt` `invalid` | no limit: a mistyped year waits for decades; a year: no use case asks for it |
| Shown afterwards | `scheduledAt`, kept after the notification is queued | only while scheduled: the time would vanish from the record once it is sent |

## Decision

- **`scheduledAt`** (RFC 3339 with its offset) is an optional member of a create and of each batch item. Like
  `priority`, it is ours: the provider receives `{to, channel, content}` only.
- **The draft decides**: `Draft.Validate(now)` refuses a time more than `MaxScheduleAhead` (30 days) ahead, and
  `Draft.Status(now)` starts the notification `scheduled` when its time is ahead and `queued` otherwise. The service
  takes `now` once per request, so validation and status agree, and a batch's items are judged at one moment.
- **Storage** (`migrations/0007`): a `scheduled_at` column keeps the time asked for; a scheduled notification also
  gets it as `next_attempt_at`, and a CHECK requires `scheduled_at` while the status is `scheduled`. Only a
  notification due now gets an outbox row; a scheduled one is queued by the scheduler, with its outbox row, when its
  time comes (ADR 0006). Cancelling clears `next_attempt_at`, so a cancelled notification is never queued.
- **Idempotency**: `scheduledAt` is part of the request's fingerprint, written in UTC, so one instant in two offsets
  is one request; a request without it keeps the fingerprint it had before, so stored keys still match.
- **A malformed time** fails decoding (`400 invalid_json`), as any member of the wrong type does.

## Consequences

- `GET /v1/notifications?status=scheduled` lists what waits, `GET /v1/metrics` counts it per channel, and cancelling
  works until the time comes.
- A notification is queued within about one scheduler interval (one second by default) of its time, then sent in
  priority order with the channel's other due work, within the rate limit.
- Defense in depth: a scheduled notification that reached the queue by mistake would not be sent early, because a
  consumer claims only `queued` notifications (ADR 0012); the store's test, not the end-to-end one, catches the
  mistake.

### Deliberately not done

| Not done | Why not |
|---|---|
| Rescheduling a notification | Not asked for; cancel and create again does it. |
| Recurring schedules | A different feature (a cron of notifications); nothing in the brief asks for it. |
| A schedule per batch | Each item carries its own `scheduledAt`, which also covers the whole batch at one time. |
