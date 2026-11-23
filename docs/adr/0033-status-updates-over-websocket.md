# 0033. Status updates over WebSocket

- Status: Accepted
- Date: 2026-11-24

## Context

The brief's bonus asks for real-time status updates over WebSocket. Three choices were the owner's; the worked
example runs through them: a support panel follows a flash-sale batch of 1000 notifications and should see each
one turn `sent` within a second.

| Library | Context | Concurrent writes | Maintenance |
|---|---|---|---|
| **`github.com/coder/websocket` 1.8.15** | every call takes one, as the service's shutdown does | safe | released 2026-06 |
| `github.com/gorilla/websocket` 1.5.3 | none; deadlines by hand | one writer, a lock by hand | released 2024-06 |
| `golang.org/x/net/websocket` | none | — | its own documentation points to the other two |

| What a client follows | In the example | At millions of notifications a day |
|---|---|---|
| **Up to 100 notifications and a batch, by ID** | `?batchId=` brings the batch's 1000 events | only what was asked for flows |
| Everything, filtered by channel or status | the panel filters for itself | hundreds of events a second into a browser |

| Where events come from | Who produces them | Consistency | New infrastructure |
|---|---|---|---|
| **A trigger, `LISTEN/NOTIFY`** | every status change, whichever code makes it | sent on commit, never for a rollback | none: the outbox already uses it |
| A RabbitMQ fanout from the worker | only code that remembers to publish | not in the database's transaction | an exchange and queues |
| Polling the database | — | consistent | none, but a query per subscriber per interval |

Server-Sent Events would need no library, but the brief asks for WebSocket.

## Decision

- **`migrations/0009`**: an `AFTER UPDATE OF status` trigger, for rows whose status really changed, calls
  `pg_notify('notification_status', …)` with the notification's ID, its batch, its status and `updated_at`. The
  payload is about 150 bytes, well under NOTIFY's 8000.
- **The API process listens** on a connection of its own (`Store.ListenStatus`), and **a hub** passes each event to
  the subscriptions whose filter matches it. The hub never waits: a subscriber 64 events behind is dropped. A lost
  listener is reopened after a second; when the process stops, every subscription ends. Each API replica listens,
  so a client may connect to any.
- **`GET /v1/status-updates`** takes up to 100 `notificationId` and one `batchId`. The query, the upgrade headers
  and the origin are checked before the connection is taken over, so a refusal is the usual problem:
  `400 invalid_query`, `426 upgrade_required`, `403 origin_not_allowed`. The subscription starts before the
  handshake completes, so a change the client causes right after connecting is not missed.
- **On the connection**: one JSON text message per change; the client sends nothing (a message closes the
  connection, as `CloseRead` does); a ping every 30 seconds; each write bounded by 5 seconds. The server closes with
  1001 when it stops and 1008 when the client fell behind.
- **A browser may connect only from the API's own origin**: with no authentication, a page elsewhere could
  otherwise follow notifications through a visitor's browser. Programs send no `Origin` and are not affected.

## Consequences

- One ID follows a notification from acceptance to `sent` without polling, whichever process moves it.
- Events are not replayed: a client reads the current status first, then follows the changes.
- A burst, such as a batch going to `sending` at once, fits a subscriber's buffer; a client that stops reading is
  disconnected rather than slowing the others.
- Two assumptions were checked rather than kept: `net/http` clears a connection's deadlines when it is taken over
  (so the server's 15-second timeouts do not end a WebSocket; a test holds one for 17 seconds), and the
  subscription had to start before the handshake (an end-to-end test cancels right after connecting).

### Deliberately not done

| Not done | Why not |
|---|---|
| Replaying missed events | It needs a stored event log; the status is always readable. |
| Following everything | At the brief's volume it floods a browser; dashboards read `/v1/metrics` and Prometheus. |
| Messages from the client, such as changing the filter | A new connection with another query does it. |
| Cross-origin browser access | Needs authentication first; nothing here authenticates a browser. |
