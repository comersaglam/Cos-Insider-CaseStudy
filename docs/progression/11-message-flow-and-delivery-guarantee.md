# 11. Message flow and delivery guarantee

- Date: 2026-11-23
- Commits: `cea7518`
- Decisions: [ADR 0012](../adr/0012-message-flow-and-delivery-guarantee.md), amending
  [ADR 0003](../adr/0003-postgresql-and-rabbitmq-with-outbox.md)

## Goal

Make explicit what earlier decisions implied but never stated: which messaging pattern carries notifications,
which delivery guarantee the system gives and what enforces it, and how duplicates and poison messages are
contained.

## Steps

1. Compared work queues, publish/subscribe and the RabbitMQ exchange types; a notification is a command for one
   worker, so it travels on a work queue through a direct exchange keyed by channel.
2. Compared at-most-once, at-least-once and exactly-once, and walked one notification through every crash point
   of the flow. ADR 0003's claim that a redelivery never becomes a second send held only for a provider that
   honours the idempotency key; webhook.site does not.
3. Found that two copies of one message (published twice by the relay) could both send while in flight, because
   a redelivered `sending` notification had to be claimable. Chose a leased claim and a reaper in the scheduler
   over trusting RabbitMQ's `redelivered` flag.
4. Found that a message crashing the worker would loop forever on a classic queue; the claim now spends an
   attempt, and a sender's panic is recovered as a failed attempt.
5. Kept phone numbers strictly E.164: the brief would allow normalising them, but a guessed country code can
   reach a stranger; ADR 0011 now says so.
6. Recorded ADR 0012, marked ADR 0003 as amended, and moved the work into the roadmap's blocks (columns in 13,
   exchange in 18, reaper in 19, claim and panic recovery in 21). Later blocks moved up by one; the platform
   layer is now block 12.

## Verification

A design block: no code changed and no tests ran. Every link in the tracked documents resolves, and the
requirements map points to the renumbered blocks.

## Follow-ups

Block 12, the platform layer: configuration from the environment, the JSON logger with correlation IDs, and the
HTTP helpers (middleware, the RFC 9457 error writer, JSON decoding).
