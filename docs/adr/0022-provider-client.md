# 0022. The provider client

- Status: Accepted
- Date: 2026-11-23

## Context

ADR 0007 classifies the provider's answers (202 success; 400, 404, 422 permanent; 401, 403 permanent and alerting;
408, 429, 5xx, network errors and timeouts transient) and ADR 0011 fixes the request
(`{"to", "channel", "content"}`). The client that makes the call has to settle what the table leaves out: other
statuses, redirects, an acceptance it cannot read, an answer of any size, and connection reuse under 32 concurrent
sends.

## Decision

- **`delivery.Client.Send`** makes one attempt and returns an `Outcome`: its class, the HTTP status (0 without an
  answer), the provider's message ID, the `Retry-After` it asked for, the latency, and the error. It never retries;
  the consumer decides what an outcome leads to.
- **The request** is a `POST` of exactly the contract's three members as JSON, with `Idempotency-Key` set to the
  notification's ID on every attempt and `X-Correlation-ID` carrying the request's ID to the provider.
- **Classes beyond ADR 0007's table**: every `2xx` is a success (the contract names 202); every other `4xx` is
  permanent, since the same request would fail again; a redirect is not followed and is permanent, since
  re-sending a `POST` elsewhere is not the client's to decide.
- **A success whose answer cannot be read is a success** without a message ID: the provider took the message, and
  retrying would send it twice.
- **`Retry-After`** is read in seconds or as an HTTP date; an absent or unreadable one is 0.
- **At most 64 KB of an answer is read**, and a failure keeps 256 bytes of it for the attempt's record.
- **The timeout** is the client's, set by configuration; a timeout is transient, since the message may have
  arrived, and the next attempt carries the same idempotency key.
- **Connections are reused**: the transport keeps up to 64 idle connections to the provider, where Go keeps two
  by default.

## Consequences

- The attempt record says what happened in the provider's own words, bounded in size.
- A provider that misbehaves (huge answers, redirects, unreadable acceptances) cannot make the worker resend a
  message or run out of memory.

### Deliberately not done

| Not done | Why not |
|---|---|
| Retries inside the client | Retry belongs to the consumer's policy, which records each attempt and waits in PostgreSQL (ADR 0006, 0007). |
| Following redirects | A redirected `POST` changes destination without anyone deciding it. |
| Authentication headers | webhook.site needs none; a real provider's credentials would come from configuration as a secret. |
