# 0012. Message flow and delivery guarantee

- Status: Accepted
- Date: 2026-11-23
- Amends: [0003](0003-postgresql-and-rabbitmq-with-outbox.md) (the exchange, and the wording of the duplicate
  guarantee, repeated in [0005](0005-rate-limit-with-single-active-consumer.md) and
  [0009](0009-concurrency-model.md))

## Context

ADR 0003 chose RabbitMQ with an outbox and at-least-once delivery, but left three things open:

- **The messaging pattern and the exchange.** It named the queues, not how messages reach them.
- **The exact guarantee.** It said the status check and the idempotency key keep a redelivery from becoming a
  second send. That is too strong: the provider must honour the key for it to hold, and webhook.site does not.
- **Two failure cases.** If the relay publishes a message twice, two copies of one notification can be in flight
  at once. And a message that crashes the worker is redelivered forever, because classic queues have no delivery
  limit.

### Patterns

A *work queue* (competing consumers) gives each message to one consumer; *publish/subscribe* gives every
subscriber a copy, which RabbitMQ does with a *fanout* exchange. A notification is a **command** ("send this"),
which exactly one worker must carry out, so the pattern is a work queue. Publish/subscribe suits **events**
("this was sent") that several parties react to; none is needed here (status updates to WebSocket clients use
PostgreSQL `LISTEN/NOTIFY`).

| Exchange | For | Against |
|---|---|---|
| **Direct exchange, routing key = channel** | The publisher knows only the channel; another queue (an audit or debugging tap) binds without touching it | One more object to declare |
| Default exchange, routing key = queue name | Nothing to declare | The publisher hard-codes queue names |
| Topic exchange | Pattern routing (`sms.high`) | Priority is a message property already; patterns add nothing |
| Fanout exchange | Broadcast | Every queue would get every notification |

### Guarantees

| Guarantee | How it is built | On a crash |
|---|---|---|
| At most once | acknowledge, then send | the message can be lost, never duplicated |
| **At least once** | send and record, then acknowledge | the message is redelivered, and can be sent twice |
| Exactly once | not possible end to end with an external HTTP provider: no transaction spans our database and the provider's | what is sold as exactly once is at least once with an idempotent receiver |

## Decision

### The flow

```
API ─(one transaction)─> notifications + outbox
relay ─(publish, persistent, confirmed)─> exchange "notifications" (direct)
      ─(routing key sms | email | push)─> queue notifications.<channel> (priority, single active consumer)
consumer ─> breaker ─> rate limiter ─> semaphore slot ─> claim ─> provider ─> record outcome ─> acknowledge
```

The publisher sets the routing key to the notification's channel and the AMQP priority to its priority. The
claim comes after the rate limiter's wait, so the claim's lease covers only the provider call.

### At least once, effectively once when the provider honours the idempotency key

| What enforces it | What it prevents |
|---|---|
| The outbox, written in the notification's transaction | loss between the database and the broker |
| Persistent messages, durable queues, publisher confirms | loss while publishing or when RabbitMQ restarts |
| Manual acknowledgement after the outcome is stored | loss in the worker |
| The claim (below) | a second send of finished or in-flight work |
| `Idempotency-Key: <notification id>` on every attempt | a second send in the one window the claim cannot close |

What each crash point leads to, for one notification:

| The process stops | Then | Outcome |
|---|---|---|
| after the commit, before the relay publishes | the outbox row waits | sent later |
| after the confirm, before the outbox row is marked | the row is published again: two copies | the second copy finds the notification claimed or finished and is dropped |
| after the claim, before the provider call | redelivered; the lease expires | claimed again and sent once |
| **after the provider accepted, before the outcome is stored** | redelivered; the lease expires | **sent again**, unless the provider honours the idempotency key |
| after the outcome is stored, before the acknowledgement | redelivered; the notification is final | dropped |

The fourth window lasts from the provider's answer to one `UPDATE`. A claim that takes over an expired lease
records the earlier attempt with the outcome `unknown` and increments a metric, so such possible duplicates are
counted rather than hidden.

### The claim holds a lease

The claim is one conditional `UPDATE`:

- it succeeds when the notification is `queued`, or `sending` with `claimed_until` in the past (its worker was lost);
- it sets `sending`, `claimed_until = now + lease` and `attempts = attempts + 1`, in that one statement;
- the lease is the provider timeout plus a margin, set in configuration.

A delivery whose claim fails is acknowledged and dropped. That is safe in every case: the notification is final;
or it is in flight under another copy's live lease; or it is waiting and will be published again when it is due.

### A reaper returns lost work

The scheduler also finds `sending` rows whose lease has expired. It records the attempt as `unknown` and moves
the row to `retrying` with a backoff, or to `failed` when no attempts are left. A notification whose worker
crashed is therefore never stuck, even when its redelivered message arrived before the lease ran out and was
dropped. The reaper and a redelivered claim race on the same conditional `UPDATE`, and only one of them wins.

### Poison messages

| Kind | Handling |
|---|---|
| A body that cannot be read, or an ID that does not exist | rejected without requeue, so it is dead-lettered to `notifications.dlq` |
| A message the provider rejects for good | `failed`, kept in PostgreSQL with its attempts ([0007](0007-delivery-and-retry.md)) |
| A message that crashes the worker | each claim spends an attempt before the call, so a crash loop ends in `failed` after `maxAttempts`; a panic in a sender goroutine is recovered and recorded as a failed attempt instead of stopping the process |

## Consequences

- Duplicates are bounded to one narrow, measured window, and prevented entirely by a provider that honours
  idempotency keys.
- The `notifications` table gains `attempts` and `claimed_until`; the scheduler gains the reaper query.
- A crash costs one attempt for each notification it interrupted.
- A copy dropped because of a live lease relies on the lease holder or the reaper to finish the work, so the
  reaper is required, not an optimisation.

### Deliberately not done

| Not done | Why not |
|---|---|
| Re-claiming every `sending` notification on redelivery | Two copies in flight at once would both send. |
| Re-claiming only when RabbitMQ marks the delivery `redelivered` | After a channel drops and the process reconnects, the old sender may still be running; and nothing would rescue a notification whose message was dropped. |
| Acknowledging before sending (at most once) | A crash loses the notification silently. |
| Quorum queues for their delivery limit | They support two priority levels rather than three, and the attempt counter covers crash loops already ([0003](0003-postgresql-and-rabbitmq-with-outbox.md)). |
| A fanout exchange for status events | No component needs them; WebSocket updates use `LISTEN/NOTIFY`. |
