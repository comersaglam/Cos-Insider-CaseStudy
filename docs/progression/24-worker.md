# 24. Worker composition root and an end-to-end test

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0025](../adr/0025-worker-in-compose-and-the-provider-stand-in.md), amending
  [ADR 0019](../adr/0019-container-image-and-the-one-command-stack.md); applying ADR 0009

## Goal

Run the whole delivery side as one process, test it end to end, and make `docker compose up` send notifications.

## Steps

1. Added `golang.org/x/sync` v0.25.0, approved earlier. `testenv.RabbitMQVhostURL` gives a test a RabbitMQ virtual
   host of its own through the management API, so the worker's real queue names can be used in parallel tests.
   `notification.Channels` lists the channels in a stable order.
2. `cmd/worker`, tests first: `run(ctx, args, lookup, stdout)` reads the configuration, connects, declares the
   topology, and runs the relay, the scheduler, a consumer per channel on a channel of its own, and a watcher of the
   RabbitMQ connection under one `errgroup`; resources close in reverse order once every loop has stopped.
3. The end-to-end test creates notifications through `notification.Service`, as the API does, and watches them go
   through the outbox, RabbitMQ and the consumers to a fake provider: two sent, one retried through the scheduler
   until its two attempts were spent. A second test stops the worker while the provider holds a request: the send
   finishes and is recorded. The first run failed on stopping because the test's virtual host was created inside
   the worker's goroutine and deleted before the worker stopped; it is now created first.
4. Asked the owner how Compose's worker reaches a provider, since free webhook.site URLs expire in seven days and
   take 100 requests; the owner chose a stand-in by default with webhook.site through `PROVIDER_URL`.
5. Compose gained `worker` and `provider` (WireMock 3.13.2 with mappings in `deploy/wiremock/mappings`: 202 by
   default, 503 and 422 for two demonstration numbers); `make smoke` now waits for the notification to be sent.
6. Running the stack showed WireMock answering 500: my date format broke its template engine. The circuit breaker
   opened on it, its probes failed while the error lasted, and it closed on the first probe after the fix. The
   mapping now uses WireMock's default ISO 8601 time.
7. Recorded ADR 0025.

## Verification

- `go test -race ./cmd/worker/`: both end-to-end tests pass, 80.7% coverage; every other package green.
  `golangci-lint`: 0 issues.
- `make up` then `make smoke`: created, repeated with its key (202), sent by the worker. The demonstration numbers
  ended `failed` (`provider_rejected`, one attempt) and `retrying`.

## Follow-ups

Phase 3 starts with block 25, metrics: HTTP and sending counters and latencies, queue depth, breaker state, served on
`/metrics` by both processes.
