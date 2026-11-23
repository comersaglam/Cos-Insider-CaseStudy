# 0028. Correlation across the queue

- Status: Accepted
- Date: 2026-11-23
- Amends: [0013](0013-platform-conventions.md) (the correlation ID, "over HTTP now and AMQP later")

## Context

A correlation ID ties together every log line one request causes. The API takes it from `X-Correlation-ID` or makes
one (ADR 0013). A notification is sent later, by another process, after a trip through the outbox and RabbitMQ, and
the brief asks that its delivery can be followed. One ID should therefore find the API's line about the request
and the worker's lines about the sends it caused.

Most of the way was built with the blocks that needed it: the notification stores the ID (block 15), the relay puts
it in the message's `X-Correlation-ID` header (ADR 0020), the consumer logs under it once it has claimed the
notification, and the provider client sends it in its own `X-Correlation-ID` header (ADR 0022). Two questions were
left.

**Which ID does a notification queued again carry?** Worked example: a request with ID `A` creates notification `n`,
which fails; later an operator retries it with a request whose ID is `B`.

| Option | Searching `A` finds | Searching `B` finds | Cost |
|---|---|---|---|
| **The creating request's, for the notification's life** | everything about `n`, retries included | the API's line about the retry; its sends are found by `notification_id` | none |
| The ID of the request that queued it last | `n`'s first sends only | the retry and its sends | a column on the outbox, and another on the notification for the scheduler's automatic retries |

**May the worker trust the header?** Lines written before the notification is read (a message that cannot be read, a
notification that does not exist or has nothing left to send) can be linked only through the header. But anyone
who can publish to the exchange can set a header.

## Decision

- **A notification keeps the ID of the request that created it**, through every retry, manual or automatic. Every
  worker line about a notification also carries `notification_id`, which joins a retry request to its sends.
- **The consumer takes the ID from the message's header** before it reads anything else, checked as the API checks a
  client's (1 to 128 visible ASCII characters), and ignores a value that fails the check. Once the notification is
  claimed, the ID stored with it is used, so a message published without the header is still linked.
- The provider receives the ID in an `X-Correlation-ID` header. Its contract (`to`, `channel`, `content`) is about
  the body; a header adds no field to the message, as `Idempotency-Key` does not.

## Consequences

- `correlation_id` finds a request's API line and every worker line about the notifications it created, including
  dead-lettered messages; a test follows one ID from a created notification to the provider and the worker's log.
- A forged header cannot break a log line.

### Deliberately not done

| Not done | Why not |
|---|---|
| A new ID for each manual retry | Splits a notification's history across IDs; `notification_id` already joins the retry request to its sends. |
| W3C `traceparent` | Tracing is a bonus (block 34); it adds `trace_id` beside the correlation ID rather than replacing it. |
| The ID in the message body | The body names the notification only (ADR 0012); a header is where AMQP and HTTP carry metadata. |
