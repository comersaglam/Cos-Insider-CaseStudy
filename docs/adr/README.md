# Architecture Decision Records

Each significant decision is recorded as one numbered file, in the format of Michael Nygard's ADRs: the context
that forced the decision, the decision itself, and its consequences, including what was deliberately left out.
A decision is never edited away: when it changes, a new record supersedes it and the old one says so.

Every decision follows the industry standard where one exists; a departure from it states why.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-go-toolchain-and-libraries.md) | Go toolchain and libraries | Accepted |
| [0002](0002-testing-strategy.md) | Testing strategy | Accepted |
| [0003](0003-postgresql-and-rabbitmq-with-outbox.md) | PostgreSQL as the source of truth, RabbitMQ for dispatch, joined by a transactional outbox | Accepted |
| [0004](0004-separate-api-and-worker-binaries.md) | Separate API and worker binaries from one image | Accepted |
| [0005](0005-rate-limit-with-single-active-consumer.md) | Enforce 100 messages per second per channel with a single active consumer | Accepted |
| [0006](0006-delayed-work-waits-in-postgresql.md) | Delayed work waits in PostgreSQL; RabbitMQ carries only work that is due | Accepted |
| [0007](0007-delivery-and-retry.md) | Delivery and retry | Accepted |
| [0008](0008-modular-monolith-layout.md) | Modular monolith: packages by feature, a platform layer and one central router | Accepted |
| [0009](0009-concurrency-model.md) | Concurrency model | Accepted |
| [0010](0010-local-environment-with-docker-compose.md) | Local environment with Docker Compose | Accepted |

## Template

```markdown
# NNNN. Title

- Status: Proposed | Accepted | Superseded by NNNN
- Date: YYYY-MM-DD

## Context
What forces the decision: requirements, constraints, the options considered.

## Decision
What we do, stated in the present tense.

## Consequences
What becomes easier, what becomes harder, and what we deliberately do not do, each with its reason.
```
