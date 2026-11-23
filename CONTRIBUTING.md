# Contributing

How work is done in this repository. These rules apply to every change, whoever or whatever makes it.
What the code and its tests already say is not repeated here.

## Workflow

- **Work in blocks.** A block is one piece of work that can be checked on its own. It ends with its tests
  green and one commit. A small block runs the tests of the packages it touched; a large one runs `make test`.
- **Tests first.** A block's tests are written before its code and seen failing for the right reason, then
  made to pass.
- **Commit only your own paths**: `git commit -- <paths>`, never a bare `git commit` over a shared index.
- **No branch, push, pull request or history rewrite** (rebase, `reset --hard`, amending a pushed commit,
  force-push) unless the owner asks for it in the current request.
- **Surgical changes**: touch only what the task needs, match the style already there, do not refactor what is
  not broken, and remove only what your own change orphaned.
- **Built-ins first, industry standard where a choice is open**: the Go standard library before a dependency,
  a well-known dependency before code of our own.

## Commit messages

The format of the Go project itself: `package: what now holds`, lower case, imperative, no trailing period.

```
queue: claim due notifications with FOR UPDATE SKIP LOCKED

ClaimDue in internal/store/postgres now selects due rows for one channel
ordered by priority and next_attempt_at, and locks them with SKIP LOCKED
so parallel workers never wait on or take the same row.

Why: a plain SELECT then UPDATE let two workers claim one row under load.
Rejected: an advisory lock per channel, which serialises all workers.

Behaviour: a claimed row moves to status processing with locked_until set.
Tests: TestClaimDue_ParallelWorkersNeverShareARow (integration, -race).
```

The body says what changed (naming packages, functions, endpoints and migrations), why, the alternative that
was rejected, what behaves differently, and which tests cover it. Its length follows the change. Nothing says
who decided something or when.

## Code

- **Self-explanatory and not over-engineered.** No abstraction for a single use, no option or layer for a need
  that does not exist yet. A bare number becomes a named constant. A reason a name cannot hold becomes a test
  whose name states the behaviour.
- **Comments follow Go convention.** Every package has a package comment and every exported identifier has a
  doc comment that starts with its name (enforced by the linter). Inside a function a comment says *why*,
  never *what*. Tutorial-style explanations do not belong in code.
- **Layers point inward**: `api` (HTTP handlers) → `service` (business rules) → `store` (SQL); every layer may
  import `domain`, and `domain` imports nothing of ours. A handler reads the request and writes the answer; it
  holds no business rule.
- **Required means the same in every layer**: a field that `Validate()` requires is in the OpenAPI schema's
  `required` list and is `NOT NULL` in the database, and the other way round.
- **One clock**: every moment is stored as `timestamptz` in UTC and sent as ISO 8601 UTC.
- **One error envelope** for every 4xx and 5xx answer, with a stable machine-readable code per error.
- **Production defaults everywhere**: there is no global environment switch. A stand-in (a fake provider) runs
  only when its own setting turns it on.

## API

- Paths are kebab-case and versioned (`/v1/...`); JSON fields are camelCase, matching the provider contract.
- `api/openapi.yaml` is the contract. A test checks that every route the server registers is in it.
- Lists use keyset (cursor) pagination ordered by `(created_at, id)`.

## Tests

- `make test` runs the whole suite with one command.
- No test reaches an external service: the notification provider is replaced by an `httptest.Server`.
- Tests that need PostgreSQL run against a real database, never a mock of SQL.

## Logging

- Structured JSON through `log/slog`, with the request's correlation ID on every line.
- Recipient phone numbers, e-mail addresses and message content are personal data: they are never logged in
  full. A secret is written as `***`, never left out and never shown.

## Documentation

- **Docs move with the code**: a commit that changes behaviour updates `README.md`, `docs/` and
  `api/openapi.yaml` in the same commit.
- **English in every tracked file.** Each English doc `docs/<name>.md` may have a Turkish working copy
  `docs/<name>.tr.md`, which git ignores. The English file is the source of truth; its Turkish twin is updated
  in the same block.
- `docs/learning/*.tr.md` holds local study notes on the Go concepts each block used.
