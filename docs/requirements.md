# Requirements

The Insider One software engineer assessment (Go), restated so the repository is complete without the brief,
which stays outside it. Each requirement links the block that builds it ([roadmap](roadmap.md)) and the decision
behind it ([ADRs](adr/README.md)).

## The task

A scalable notification system that accepts messages through an API and delivers them over SMS, e-mail and push
through an external provider, with high throughput, reliable delivery and status tracking. The business case:
millions of notifications a day, bursts (flash sales, breaking news), intelligent retries, and visibility of
delivery status for internal teams and API clients. Expected effort: about 12 hours.

## Functional requirements

| # | Requirement | Block | Decision |
|---|---|---|---|
| 1.1 | Create a notification with recipient, channel, content and priority | 10, 14 | [0011](adr/0011-notification-domain.md) |
| 1.2 | Batch creation, up to 1000 notifications per request | 15 | |
| 1.3 | Query a notification's status by ID, or a batch's by batch ID | 14, 15 | |
| 1.4 | Cancel pending notifications | 16 | [0011](adr/0011-notification-domain.md) |
| 1.5 | List notifications filtered by status, channel and date range, with pagination | 16 | |
| 2.1 | Process notifications asynchronously through queue workers | 11, 18–23 | [0003](adr/0003-postgresql-and-rabbitmq-with-outbox.md), [0012](adr/0012-message-flow-and-delivery-guarantee.md), [0004](adr/0004-separate-api-and-worker-binaries.md), [0009](adr/0009-concurrency-model.md) |
| 2.2 | Rate limit: at most 100 messages per second per channel | 21 | [0005](adr/0005-rate-limit-with-single-active-consumer.md) |
| 2.3 | Priority queues: high, normal, low | 18, 19 | [0003](adr/0003-postgresql-and-rabbitmq-with-outbox.md) |
| 2.4 | Content validation: character limits, required fields | 10 | [0011](adr/0011-notification-domain.md) |
| 2.5 | Idempotency, so a notification is not sent twice | 11, 13, 14, 21 | [0003](adr/0003-postgresql-and-rabbitmq-with-outbox.md), [0012](adr/0012-message-flow-and-delivery-guarantee.md) |
| 3 | Delivery and retry logic, designed by the candidate | 11, 20–22 | [0006](adr/0006-delayed-work-waits-in-postgresql.md), [0007](adr/0007-delivery-and-retry.md), [0012](adr/0012-message-flow-and-delivery-guarantee.md) |
| 4.1 | A real-time metrics endpoint: queue depth, success and failure rates, latency | 24 | |
| 4.2 | Structured logging with correlation IDs | 12, 26 | |
| 4.3 | A health check endpoint | 25 | |

## The external provider

webhook.site stands in for the provider. Its contract is fixed and cannot be extended
([ADR 0011](adr/0011-notification-domain.md)):

```http
POST https://webhook.site/{uuid}

{"to": "+905551234567", "channel": "sms", "content": "Your message"}
```

The expected answer is `202 Accepted` with `{"messageId": "<uuid>", "status": "accepted", "timestamp": "<ISO 8601>"}`.

## Bonus features (optional)

| Bonus | Block |
|---|---|
| Failure handling | 16, 21, 22 |
| Scheduled notifications | 30 |
| Templates with variable substitution | 31 |
| Real-time status updates over WebSocket | 32 |
| Distributed tracing | 33 |
| GitHub Actions CI/CD: automated tests and linting | 29 |

## Deliverables

| Deliverable | Where |
|---|---|
| Source code in a GitHub or GitLab repository with a clean commit history | this repository |
| `README.md`: setup, architecture overview, API examples | block 28 |
| Docker Compose: one-command setup (`docker compose up`) | blocks 09, 17 |
| API documentation: Swagger / OpenAPI | `api/openapi.yaml`, block 27 |
| Versioned database migrations | `migrations/`, block 13 |
| A test suite runnable with a single command | `make test` |

The technology is Go (1.2x).
