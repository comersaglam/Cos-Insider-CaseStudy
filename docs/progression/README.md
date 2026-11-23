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
| [12](12-platform.md) | Platform: config, observability, httpx | 2026-11-23 | Configuration reader, JSON logger with correlation IDs, RFC 9457 errors, strict JSON decoding, middleware | this block's commit |

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
