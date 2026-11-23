# 33. Status updates over WebSocket

- Date: 2026-11-24
- Commits: this block's commit
- Decisions: [ADR 0033](../adr/0033-status-updates-over-websocket.md)

## Goal

Let a client follow notifications' status changes as they happen, the brief's WebSocket bonus.

## Steps

1. Asked the owner, who approved `github.com/coder/websocket` 1.8.15, subscriptions to up to 100 notifications and a
   batch, and a PostgreSQL trigger as the source.
2. 33a, tests first: `migrations/0009` notifies on every committed status change, not on an update that keeps the
   status; `Store.ListenStatus` reads the events, their time in UTC.
3. 33b, tests first: the hub delivers by notification and by batch, drops a subscriber 64 events behind without
   waiting for it, listens again after losing its listener, and closes every subscription when it stops.
4. 33c, tests first: `GET /v1/status-updates` checks the query, the upgrade and the origin before taking the
   connection over (400, 426, 403 as problems), streams the subscribed events, and closes with 1001 when the server
   stops. The API process runs the hub and waits for it before closing the pool.
5. Two assumptions tested: I expected the server's 15-second timeouts to end a WebSocket and wrote code to lift
   them; with that code removed, the 17-second test still passed, and `net/http`'s source shows it clears the
   deadlines when a connection is taken over, so the code went and the test stayed. Writing the end-to-end test
   showed that subscribing after the handshake could miss a change made right after connecting; the subscription now
   starts first.
6. The contract documents the endpoint, its 101, 400, 403 and 426 answers, `StatusEvent`, and the two new codes.

## Verification

`internal/notification`, `internal/httpapi`, `api/contract` and `cmd/api` with `-race` green; the end-to-end test
cancels a notification through the API and hears `cancelled` over the WebSocket.

## Follow-ups

Block 34, distributed tracing.
