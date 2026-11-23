# 10. Notification domain

- Date: 2026-11-23
- Commits: `2d19a8d`
- Decisions: [ADR 0011](../adr/0011-notification-domain.md)

## Goal

Define what a notification is before anything stores, serves or sends one: its channels, priorities and
statuses, which status may follow which, and the rules a client's request must meet.

## Steps

1. Settled the block's open questions: SMS content up to 1600 characters, recipients checked by shape with the
   standard library, and no e-mail subject, because the provider contract is fixed and has no field for one.
2. Restated the brief in `docs/requirements.md`, mapping each requirement to its block and ADR; the brief itself
   stays outside the repository.
3. Wrote the tests first in `internal/notification`: `TestChannelValid`, `TestPriorityValid`, `TestStatusValid`,
   `TestStatusCanTransitionTo` (every pair of statuses), `TestDraftValidate` (each channel's limit and limit + 1,
   recipient formats, blank, NUL and non-UTF-8 content, all problems at once),
   `TestDraftValidateReturnsNilWhenValid` and `TestValidationErrorMessage`. Against an empty stub they failed on
   their assertions.
4. Wrote `types.go` (`Channel`, `Priority`), `status.go` (`Status` and its transition table) and `draft.go`
   (`Draft.Validate`, `ValidationError`, `FieldError`, `Code`).
5. Recorded the decisions, and what was left out, as ADR 0011.

## Verification

- `go test -race ./internal/notification/` passes with 100% statement coverage; `make check` passes.
- Two deliberate bugs were each caught: counting content in bytes failed three boundary cases written with
  two-byte letters, and returning an empty `*ValidationError` for a valid draft failed
  `TestDraftValidateReturnsNilWhenValid`.

## Follow-ups

Block 11 settles the message flow and delivery guarantee that ADR 0003 left open; the platform layer follows as
block 12.
