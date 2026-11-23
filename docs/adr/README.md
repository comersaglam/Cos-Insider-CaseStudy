# Architecture Decision Records

Each significant decision is recorded as one numbered file, in the format of Michael Nygard's ADRs: the context
that forced the decision, the decision itself, and its consequences, including what was deliberately left out.
A decision is never edited away: when it changes, a new record supersedes it and the old one says so.

Every decision follows the industry standard where one exists; a departure from it states why.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-go-toolchain-and-libraries.md) | Go toolchain and libraries | Accepted |
| [0002](0002-testing-strategy.md) | Testing strategy | Accepted |
| [0003](0003-postgresql-and-rabbitmq-with-outbox.md) | PostgreSQL as the source of truth, RabbitMQ for dispatch, joined by a transactional outbox | Accepted, amended by 0012 |
| [0004](0004-separate-api-and-worker-binaries.md) | Separate API and worker binaries from one image | Accepted |
| [0005](0005-rate-limit-with-single-active-consumer.md) | Enforce 100 messages per second per channel with a single active consumer | Accepted |
| [0006](0006-delayed-work-waits-in-postgresql.md) | Delayed work waits in PostgreSQL; RabbitMQ carries only work that is due | Accepted |
| [0007](0007-delivery-and-retry.md) | Delivery and retry | Accepted, amended by 0014 |
| [0008](0008-modular-monolith-layout.md) | Modular monolith: packages by feature, a platform layer and one central router | Accepted |
| [0009](0009-concurrency-model.md) | Concurrency model | Accepted, amended by 0023 |
| [0010](0010-local-environment-with-docker-compose.md) | Local environment with Docker Compose | Accepted, amended by 0019 |
| [0011](0011-notification-domain.md) | Notification domain: the provider contract, statuses and validation | Accepted, amended by 0014, 0015 and 0024 |
| [0012](0012-message-flow-and-delivery-guarantee.md) | Message flow and delivery guarantee | Accepted |
| [0013](0013-platform-conventions.md) | Platform conventions: configuration, logging, correlation, errors and JSON | Accepted |
| [0014](0014-dead-letters-and-replay.md) | Dead letters and replay | Accepted |
| [0015](0015-storage-ids-and-idempotency.md) | Storage: identifiers, idempotency and schema conventions | Accepted |
| [0016](0016-http-api-and-the-api-process.md) | The HTTP API and the API process | Accepted |
| [0017](0017-batches.md) | Batches: partial acceptance, kept rejections and a body limit | Accepted |
| [0018](0018-listing-cancelling-and-retrying.md) | Listing, cancelling and retrying | Accepted |
| [0019](0019-container-image-and-the-one-command-stack.md) | The container image and the one-command stack | Accepted, amended by 0025 |
| [0020](0020-rabbitmq-platform.md) | The RabbitMQ platform layer: topology, publishing and consuming | Accepted |
| [0021](0021-outbox-relay-and-scheduler.md) | The outbox relay and the scheduler | Accepted |
| [0022](0022-provider-client.md) | The provider client | Accepted |
| [0023](0023-the-consumer.md) | The consumer: claiming, sending, recording, retrying | Accepted |
| [0024](0024-circuit-breaker.md) | The circuit breaker | Accepted |
| [0025](0025-worker-in-compose-and-the-provider-stand-in.md) | The worker in Compose, and a stand-in for the provider | Accepted |
| [0026](0026-metrics.md) | Metrics: Prometheus for machines, a summary for people | Accepted |
| [0027](0027-health-probes.md) | Health probes: liveness, readiness, and a health check without a shell | Accepted |
| [0028](0028-correlation-across-the-queue.md) | Correlation across the queue | Accepted |
| [0029](0029-contract-tests-and-documentation-page.md) | Contract tests and the documentation page | Accepted |
| [0030](0030-continuous-integration-and-delivery.md) | Continuous integration and delivery | Accepted |
| [0031](0031-scheduled-notifications.md) | Scheduled notifications | Accepted |
| [0032](0032-templates.md) | Templates | Accepted |
| [0033](0033-status-updates-over-websocket.md) | Status updates over WebSocket | Accepted |

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
