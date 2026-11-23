# 0013. Platform conventions: configuration, logging, correlation, errors and JSON

- Status: Accepted
- Date: 2026-11-23

## Context

Every module reads settings, logs, answers errors and decodes JSON. Done differently in each module, these
would give the service several error formats, logs that cannot be joined across a request, and settings that
fail one at a time. [ADR 0001](0001-go-toolchain-and-libraries.md) already chose environment variables read with
the standard library, `log/slog`, and RFC 9457; this record fixes how each is used.

## Decision

### Configuration

- `config.Reader` reads variables through a lookup function (`os.LookupEnv` in production, a map in tests).
  Each method records a problem instead of stopping, and `Err` returns all of them, so a misconfigured process
  lists every mistake in one start and exits.
- Numbers and durations must be greater than zero, and durations carry a unit (`10s`, `250ms`). An empty
  variable counts as unset, since Compose passes an empty string for a referenced but undefined variable.
- A secret is read with `Secret` and shown as `***` when the configuration is logged at start-up; the Reader is a
  `slog.LogValuer`, so the whole configuration is one log attribute.
- Each module declares the settings it needs; its composition root fills them. There is no global struct holding
  every setting of both binaries.

### Logging

- One JSON object per line through `log/slog`, times in UTC whatever the host's zone.
- A handler wrapper adds the context's `correlation_id` to every record, so code that receives only a context
  logs it too. Code logs through the `*Context` methods (`InfoContext`, `ErrorContext`).
- Recipients pass through `observability.MaskRecipient` (`+90***67`, `a***@example.com`); content is never
  logged; the access log leaves out query strings.

### Correlation ID

- Carried in `X-Correlation-ID`, the term the brief uses, over HTTP now and AMQP later.
- A client's value is kept when it is 1 to 128 visible ASCII characters; otherwise, or when absent, a new
  version 7 UUID is generated with the standard library's `uuid` package (Go 1.27). The ID is sent back in the
  response.

### Errors

- Every 4xx and 5xx answer is `application/problem+json` (RFC 9457): `type` (`about:blank`), `title` (the
  status text), `status`, `detail`, plus `code` (stable, for clients to switch on), `correlationId`, and
  `errors`, a list of `{pointer, code, detail}` where `pointer` is a JSON Pointer such as
  `/notifications/3/to`, as RFC 9457's own example does.
- Handlers return errors; `httpx.Handle` writes them. A `*Problem` anywhere in the error's chain is written as it
  is; any other error is logged and answered with `500 internal_error` without its text.

### JSON requests

- Decoded with `encoding/json/v2` (in the standard library since Go 1.27): member names match case-sensitively,
  duplicate members and invalid UTF-8 are refused. Unknown members are refused too.
- The body must be `application/json` (else `415`), within a size limit (else `413`), and exactly one value.

### Middleware order

`Correlation`, then `AccessLog`, then `Recover`, then the router: the ID exists before anything logs, and a
recovered panic appears in the access log with its `500`. `Recover` lets `http.ErrAbortHandler` through, which
`net/http` uses to abort a response on purpose.

## Consequences

- One request can be followed through every log line by one ID, and the client sees that ID in any answer.
- Clients handle every error the same way, by `code`, and can point a user at the exact field, even inside a batch.
- A setting that is wrong fails the start, not the first request that needs it.

### Deliberately not done

| Not done | Why not |
|---|---|
| `caarlos0/env`, `kelseyhightower/envconfig` | Small and well known, but the standard library and four short methods read our settings; struct tags would hide the parsing rules. |
| One global `Config` struct | Each binary would carry, and require, the other's settings. |
| `X-Request-ID` | It names one request; our ID follows a notification through the queue and the worker as well. |
| Only `traceparent` (W3C Trace Context) | It needs tracing in place, a bonus feature; when tracing arrives, logs gain `trace_id` beside the correlation ID. |
| `encoding/json` (v1) for requests | It matches `"TO"` to the field `to` and lets a duplicate member silently win. |
| Problem `type` URIs per error | They should resolve to documentation we do not host; `code` carries the same meaning. |
| Logging request or response bodies | They hold recipients and content, which are personal data. |
