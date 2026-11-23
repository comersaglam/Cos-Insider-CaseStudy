# 0011. Notification domain: the provider contract, statuses and validation

- Status: Accepted; amended by [0014](0014-dead-letters-and-replay.md) (`queued` → `failed`)
- Date: 2026-11-23

## Context

The brief fixes the provider's request: `{"to": "+905551234567", "channel": "sms", "content": "Your message"}`,
answered with `202` and `{"messageId", "status", "accepted", "timestamp"}`. That contract cannot be extended. The
brief also asks for content validation (character limits, required fields), three priorities, cancellation of
pending notifications, and status queries. Everything later (the API, the database, the worker) builds on the
types decided here.

## Decision

### The provider contract is fixed

A notification's message is exactly `to`, `channel` and `content`, under those names in the API, the code and the
database. The domain has no message field the provider cannot receive: no e-mail subject, no push title, no sender.
Fields that steer our own processing and are never sent (priority, a scheduled time, an idempotency key, a batch)
are allowed.

### Types

`Channel` (`sms`, `email`, `push`), `Priority` (`high`, `normal`, `low`) and `Status` are string types with named
constants. The values read the same in JSON, SQL and logs. Any string converts to these types, so each has a
`Valid` method and input from outside is checked with it.

### Statuses

A notification starts as `queued`, or as `scheduled` when it is to be sent later.

| From | May move to | By |
|---|---|---|
| `scheduled` | `queued`, `cancelled` | the scheduler when due; a client |
| `queued` | `sending`, `cancelled` | a worker claiming it; a client |
| `sending` | `sent`, `retrying`, `failed` | the worker, from the provider's answer ([ADR 0007](0007-delivery-and-retry.md)) |
| `retrying` | `queued`, `cancelled` | the scheduler when the retry is due; a client |
| `failed` | `queued` | the retry endpoint |
| `sent`, `cancelled` | nothing | final |

"Pending" in the brief means `scheduled`, `queued` or `retrying`: a notification can be cancelled until a worker
claims it, and not after.

### Validation

`Draft.Validate` checks every field and reports all problems at once, each with the field's contract name and a
stable code (`required`, `invalid`, `too_long`) next to a human-readable message. Clients and tests rely on the
code; the message may change.

| Field | Rule |
|---|---|
| `channel` | required, one of the three |
| `to` (sms) | E.164: `+`, a country code not starting with zero, at most 15 digits |
| `to` (email) | one bare address, checked with `net/mail`, no display name, at most 254 bytes (RFC 5321) |
| `to` (push) | an opaque device token: visible ASCII, at most 512 bytes |
| `content` | required and not blank; valid UTF-8; no NUL character (PostgreSQL `text` cannot store it) |
| `content` length | in characters, not bytes: sms 1600, email 100,000, push 1,000 |
| `priority` | required, one of the three |

Recipients are never normalised: a value the provider would not take as written is refused with a reason, not
guessed at.

## Consequences

- The provider client maps a stored notification to the request without translation.
- Turkish text is counted fairly: "ş" is one character, although it takes two bytes.
- A crash can leave a notification in `sending`; once its lease expires, a redelivered message's claim takes it
  again without a status change. That is a property of the claim, not a transition
  ([ADR 0012](0012-message-flow-and-delivery-guarantee.md)).
- The entity with an ID and timestamps (`Notification`) comes with its storage, where the ID's type is chosen.

### Deliberately not done

| Not done | Why not |
|---|---|
| An SMS limit of 160 characters | One GSM-7 segment; it refuses ordinary longer messages, and with any Turkish letter a segment holds only 70. SMS APIs accept multi-part messages and split them. |
| Counting SMS segments (GSM-7 or UCS-2) | Segments are what the provider bills, and the contract has no notion of them; counting them would add an encoding detector with no effect on delivery. |
| An e-mail subject or a push title | The contract has no field for them; accepting a value that is never delivered would mislead the client. |
| `libphonenumber` (checking a number exists in its country's plan) | A large dependency for a check the provider makes anyway; the E.164 shape catches malformed input. |
| Normalising recipients (adding `+90`, removing spaces) | The brief would allow it, since only the provider request is fixed and it would still receive E.164. But a wrong guess at a country code sends a message to a stranger, and messaging APIs (Twilio, Amazon SNS, Vonage) take E.164 at the boundary and leave formatting to the client. |
| A state-machine library | The rules are a seven-entry map and one lookup. |
