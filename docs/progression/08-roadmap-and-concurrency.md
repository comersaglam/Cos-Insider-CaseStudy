# 08. Roadmap and concurrency model

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0009](../adr/0009-concurrency-model.md)

## Goal

Plan every remaining block from the first line of code to delivery, including the bonus features, and decide how
the worker's goroutines are structured before any of them is written.

## Steps

1. Drafted the roadmap in Turkish for discussion: six phases, blocks 09–33, each with scope, tests, points to watch
   and the questions to settle when the block starts.
2. Explained the worker's goroutine tree, sizing by Little's Law, and the shutdown order; compared options for the
   sender shape, AMQP channel ownership and supervision.
3. Chose one goroutine per message bounded by a semaphore of configurable size, a single owner of each AMQP channel,
   and crash-only supervision with `errgroup` (ADR 0009). Found and recorded the deadlock a full semaphore and an
   unbuffered results channel would cause.
4. Recorded adaptive concurrency and horizontal scaling as future work, noting that rising latency calls for less
   concurrency rather than more and that worker replicas add availability, not per-channel throughput.
5. Wrote `docs/roadmap.md` (English, tracked) and synced the Turkish twin.

## Verification

No code changed, so no tests ran. Every ADR and progression link in the roadmap points to an existing file.

## Follow-ups

Block 09, the notification domain, starts with its open questions: the SMS limit, the e-mail subject, the push
recipient format and phone number format.
