# 0017. Batches: partial acceptance, kept rejections and a body limit

- Status: Accepted
- Date: 2026-11-23

## Context

The brief asks for batch creation of up to 1000 notifications per request. A flash-sale campaign of 1000
recipients may hold a few malformed numbers. Three questions follow: what one invalid item does to the rest,
what remains of the invalid items, and how large a request may be, since 1000 e-mails of 100,000 characters
could reach about 600 MB with every character escaped.

| An invalid item | For | Against |
|---|---|---|
| Refuses the whole batch (atomic) | Simple: all or nothing | 999 good messages are not sent because of one typo; the client must find it and resend everything |
| **Is reported; the valid items are accepted (partial)** | The campaign goes out; the client fixes only what was reported | The answer must say which items failed and why |

Partial acceptance is how bulk APIs commonly behave: Amazon SQS's `SendMessageBatch` and Firebase's
`sendEach` report a result per item.

## Decision

- **Endpoints**: `POST /v1/batches` creates a batch, `GET /v1/batches/{id}` reads it; creating and reading the
  same resource share its collection. The roadmap had named the create `POST /v1/notifications/batch`.
- **Each item is validated on its own.** The valid items become `queued` notifications with their outbox rows; the
  invalid ones are reported by position (counted from zero) and reason, located by JSON Pointer such as
  `/notifications/3/to`.
- **The answer** is `202` with the batch and its `Location` while at least one item is accepted. It lists the ID
  each accepted item was given, by position. A batch with nothing valid answers `422 validation_failed` with
  every item's problems, and nothing is stored, as a single invalid create stores nothing. An empty batch or one
  of more than 1000 items is `422` on `/notifications`.
- **Rejections are kept, their content is not.** The `batches` row holds the totals and the rejected items'
  positions and reasons (`jsonb`); a `CHECK` makes the rejections add up to `total - accepted`. The content of a
  rejected item is personal data the client still has, so it is not stored. `GET /v1/batches/{id}` shows the
  counts, the rejections, and the batch's notifications counted by their status now.
- **A rejected item is not replayed**: it would fail the same way. A valid item that later fails delivery
  becomes `failed` after its attempts (ADR 0007) and is replayed by the bulk retry with a `batchId` filter
  (ADR 0014).
- **One transaction** writes the batch, its notifications and their outbox rows; the notifications and outbox
  rows go through `COPY`, one round trip for up to a thousand rows. Each notification records its batch and
  position (`batch_id`, `batch_index`, unique together).
- **An `Idempotency-Key` covers the whole batch**, fingerprinted over every item in order, rejected ones
  included. A repeat returns the first batch with the same IDs, marked `Idempotent-Replayed`; the same key with a
  different batch is `422 idempotency_key_reused`. Batch keys and notification keys live in separate indexes.
- **The body limit is 10 MiB by default**, set by `BATCH_MAX_BODY_BYTES`. A full batch of the longest SMS or push
  messages (about 4 MB) fits, as does one of e-mails averaging 10 KB; larger e-mails go in smaller batches. For
  comparison, SendGrid accepts 30 MB in one mail request.

## Consequences

- One malformed number delays nobody else's message.
- Every batch can be audited later: how many items came, how many were accepted, and why the rest were not.
- The client keeps the content of its rejected items and resends them once fixed, in a new batch or alone.

### Deliberately not done

| Not done | Why not |
|---|---|
| Storing rejected items' content | It is personal data, invalid, and still with the client. |
| Per-item idempotency keys | The batch key makes a repeated request safe; per-item keys would add a second scheme to explain. |
| Listing a batch's notifications in `GET /v1/batches/{id}` | Up to a thousand of them; the list endpoint filters by batch and pages. |
| `207 Multi-Status` | It belongs to WebDAV; `202` with results per item says the same in the shape bulk APIs use. |
| A batch's own status (`processing`, `done`) | Derived from its notifications' statuses, which the read already counts. |
