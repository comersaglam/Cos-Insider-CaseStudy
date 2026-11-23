# 0016. The HTTP API and the API process

- Status: Accepted
- Date: 2026-11-23

## Context

The first endpoints, `POST /v1/notifications` and `GET /v1/notifications/{id}`, make the API a running process.
That needs an answer to how a create is acknowledged, who applies the schema, how the contract is written, and a
set of smaller conventions every later endpoint follows.

### The status of a create

| Option | Says | Used by |
|---|---|---|
| **`202 Accepted`** | accepted for processing, not finished; the answer points at where to follow it (RFC 9110) | SendGrid's mail send; the provider contract itself |
| `201 Created` | a resource now exists at `Location` | Twilio's messages, which start `queued` |

A client asks for a message to be sent, and that happens later; `201` reads as "sent".

### Applying migrations

| Option | A failed migration | Privileges | Extra parts |
|---|---|---|---|
| **A separate step, `api migrate`** | the deployment stops; the running version keeps serving | the API's database user needs no DDL | none: the same binary, migrations embedded |
| The API migrates as it starts | every replica crashes on start and restarts in a loop | the API needs DDL | none |
| golang-migrate's own CLI image | as the first | as the first | another image to pin; migrations read from disk, not the binary |

It is the step a Django project runs as `manage.py migrate`, or Kubernetes as a Job before a rollout.

### Writing the contract

| Option | Source of truth | Effect on the code |
|---|---|---|
| **By hand, design first** (`api/openapi.yaml`, OpenAPI 3.1) | the file a reviewer reads | none |
| From comments (`swaggo/swag`) | annotations the compiler does not check | handlers carry a comment language |
| From code (a framework such as `huma`) | Go types | handlers rewritten in the framework's shape |

## Decision

- **A create answers `202 Accepted`** with the notification, its `Location`, and its `queued` status. A request
  repeating an earlier one by `Idempotency-Key` gets the same answer with `Idempotent-Replayed: true`, the header
  Stripe uses.
- **Migrations are a separate step**: `api migrate` applies them and exits; `api` (or `api serve`) serves. The
  one-command stack runs `api migrate` as a service the API and the worker wait for. ADR 0004's two binaries stay
  two.
- **`api/openapi.yaml` is written by hand** and grows with each endpoint. OpenAPI 3.1 takes JSON Schema 2020-12,
  so the rules per channel are written as `if`/`then`, and `maxLength` counts characters, as the domain does. A
  test that every registered route is in the file, the file's validation, and Swagger UI come with the OpenAPI
  block.
- **Conventions for every endpoint:**
  - `priority` may be left out and means `normal`; the API fills it in before the domain validates.
  - Errors: `422 validation_failed` with `errors` located by JSON Pointer; `422 idempotency_key_reused`;
    `400` for a malformed body or `Idempotency-Key` (1 to 255 visible ASCII characters); `404 not_found` for an
    unknown ID and for one that is not a UUID, since it cannot name a notification either.
  - A create body is at most 1 MiB: the largest valid one, an e-mail of 100,000 characters, is about 600 KB even
    with every character escaped.
  - A request no route matches is answered as a problem too: `404 not_found`, or `405 method_not_allowed` with
    its `Allow` header, which `ServeMux` alone would answer in plain text.
  - The answer shows a notification in its own type, converted from the domain's; a field added to the domain
    does not compile there until someone decides whether the API shows it.
- **The server**: `ReadHeaderTimeout` 5 s (against clients that send headers slowly to hold connections),
  `ReadTimeout` and `WriteTimeout` 15 s, `IdleTimeout` 60 s; its own errors go to the JSON log. On SIGINT or
  SIGTERM it stops accepting connections, lets requests in flight finish within `SHUTDOWN_TIMEOUT` (15 s), and
  then closes the database pool.
- **`run(ctx, args, lookup, stdout)`** holds the whole program; `main` only connects it to the process's signals,
  arguments, environment and output, so tests drive the real program, end to end, against a real database.

## Consequences

- A client follows a notification by the `Location` of its create, and can repeat a create safely.
- A bad migration cannot take the running API down with it.
- The contract can be reviewed without reading Go, and is checked against the routes once the OpenAPI block lands.

### Deliberately not done

| Not done | Why not |
|---|---|
| A `/healthz` endpoint now | It belongs to the health block, which also adds readiness. |
| `HEAD` and `OPTIONS` handling of our own | `ServeMux` answers `HEAD` for `GET` routes; CORS is not needed by server-to-server clients. |
| Authentication | Not in the brief; every client is trusted, and idempotency keys are global as a result (ADR 0015). |
| Rate limiting the API | Admission control is later work (ADR 0012). |
