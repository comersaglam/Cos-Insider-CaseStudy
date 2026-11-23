# Progression

How the project was built, block by block. A block is one piece of work that can be checked on its own
([CONTRIBUTING.md](../../CONTRIBUTING.md)); each has one file here, written in the same commit that finishes it.

This log does not repeat other records: *why* a decision was taken lives in the [ADRs](../adr/README.md), *what
changed in the code* lives in the commit messages. A block's file says what the block set out to do, the steps it
took, how it was verified, and links to both.

A commit cannot contain its own hash, so a block's file is written with `Commits: this block's commit`, and the
next block's commit replaces that with the hash. `git log --diff-filter=A -- docs/progression/NN-*.md` always
finds the commit that finished block NN.

| # | Block | Date | Outcome | Commits |
|---|---|---|---|---|
| [01](01-working-rules.md) | Working rules | 2026-11-23 | Rules chosen and written to `CONTRIBUTING.md` | `054e9b4` |
| [02](02-go-working-style.md) | Go working style | 2026-11-23 | Toolchain, libraries and testing strategy decided | `2d4736e` |
| [03](03-messaging-architecture.md) | Messaging architecture | 2026-11-23 | PostgreSQL + RabbitMQ with outbox, two binaries, rate limit, delayed work | `7589500` |
| [04](04-toolchain-and-remote.md) | Toolchain and remote | 2026-11-23 | Tools verified, repository pushed to GitHub | none |
| [05](05-delivery-and-retry.md) | Delivery and retry design | 2026-11-23 | Retry policy, attempt history, circuit breaker | `67619f9` |
| [06](06-project-layout.md) | Project layout | 2026-11-23 | Modular monolith skeleton with enforced import rules | `ad58ebb` |
| [07](07-progression-log.md) | Progression log | 2026-11-23 | This log, backfilled for blocks 01–06 | `4c1d2fc` |
| [08](08-roadmap-and-concurrency.md) | Roadmap and concurrency model | 2026-11-23 | Blocks 09–33 planned; goroutine structure decided | `8e6f57e` |
| [09](09-local-environment.md) | Local environment | 2026-11-23 | PostgreSQL and RabbitMQ in Compose, image tags shared with tests | `e2e5740` |
| [10](10-notification-domain.md) | Notification domain | 2026-11-23 | Channels, priorities, statuses and their transitions, validation per channel | `2d19a8d` |
| [11](11-message-flow-and-delivery-guarantee.md) | Message flow and delivery guarantee | 2026-11-23 | Work queue on a direct exchange; at least once with a leased claim, a reaper and bounded poison messages | `cea7518` |
| [12](12-platform.md) | Platform: config, observability, httpx | 2026-11-23 | Configuration reader, JSON logger with correlation IDs, RFC 9457 errors, strict JSON decoding, middleware | `8080430` |
| [13](13-dead-letters-and-replay.md) | Dead letters and replay | 2026-11-23 | Bulk replay by filter from PostgreSQL; unprocessable messages marked `failed` | `545e91b` |
| [14](14-database-and-store.md) | Database and store | 2026-11-23 | First migration, pool and migrator, per-test databases, sqlc, idempotent create | `6ccf017` |
| [15](15-create-get-and-a-running-api.md) | Create, get and a running API | 2026-11-23 | `POST` and `GET` notifications served by `cmd/api`, graceful shutdown, `api migrate`, the OpenAPI contract begun | `d222e94` |
| [16](16-batches.md) | Batch create and batch status | 2026-11-23 | Up to 1000 per request, valid items accepted and invalid ones reported, rejections kept without content | `78e1dcb` |
| [17](17-list-cancel-and-retry.md) | List, cancel and retry | 2026-11-23 | Keyset-paged list with filters, safe cancel, single and bulk retry | `44b22f8` |
| [18](18-one-command-stack.md) | One-command stack | 2026-11-23 | A 23.5 MB distroless image; `docker compose up` migrates and serves the API | `b569c52` |
| [19](19-rabbitmq-platform.md) | Platform: RabbitMQ | 2026-11-23 | Topology, confirmed mandatory publishing and consuming, tested against RabbitMQ 4.4.1 | `f1fb6b4` |
| [20](20-outbox-relay-and-scheduler.md) | Dispatch: outbox relay and scheduler | 2026-11-23 | Outbox rows reach RabbitMQ at commit, woken by NOTIFY; due work is queued | `825c4bc` |
| [21](21-provider-client.md) | Delivery: provider client | 2026-11-23 | One attempt to the provider, every answer classified | `f632ea1` |
| [22](22-the-consumer.md) | Delivery: consumer, rate limit, sending and retry | 2026-11-23 | Claim, send, record and retry under a rate and a bound; the reaper; a deadlock in ADR 0009 found and fixed | `64d1b66` |
| [23](23-circuit-breaker.md) | Circuit breaker and pausing the pull | 2026-11-23 | The consumer stops taking messages while the provider is down, spending no attempt | `65d6795` |
| [24](24-worker.md) | Worker composition root and an end-to-end test | 2026-11-23 | `docker compose up` sends notifications end to end, to a stand-in or to webhook.site | `840d923` |
| [25](25-metrics.md) | Metrics | 2026-11-23 | Prometheus `/metrics` on both processes, queue depth on scrape, a JSON summary for people | this block's commit |

## Template

```markdown
# NN. Block name

- Date: YYYY-MM-DD
- Commits: `abc1234`
- Decisions: ADR NNNN

## Goal
What the block set out to do, in one or two sentences.

## Steps
1. Each step taken, in order, naming the files and code it touched.

## Verification
The tests and checks run, and their result.

## Follow-ups
What was left open on purpose, and what the next block is.
```
