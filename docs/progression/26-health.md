# 26. Health

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0027](../adr/0027-health-probes.md)

## Goal

Liveness and readiness probes on both processes, and Compose health checks that use them, so that
`docker compose up --wait` returns once the service can work.

## Steps

1. `internal/health`, tests first: `/healthz` checks nothing; `/readyz` runs the checks at once, each within a
   timeout, answers 503 `not_ready` in the problem envelope naming what failed, and logs why without answering it.
2. `httpx.Probe`, tests first: asks a listen address's port on `127.0.0.1` for a path and fails on anything but 200.
3. `httpx.AccessLog` takes quiet paths, tests first: successful probes and scrapes are logged at debug level, failing
   ones as any request.
4. The router mounts the probes (`NewRouter` takes them); the API checks PostgreSQL; `api healthcheck` probes the
   running API without needing the database setting.
5. The worker's operations server serves the probes, checking PostgreSQL and the RabbitMQ connection;
   `worker healthcheck`.
6. A surprise: the worker's end-to-end test failed to stop within its 5 s shutdown timeout. Go's HTTP client had
   dialled a spare connection it never used, and `http.Server.Shutdown` counts such a connection idle only after five
   seconds. Reproduced on its own; the end-to-end helpers now close the client's idle connections before stopping,
   which also brought the API's end-to-end run back from 9.8 s to 4.5 s. Recorded in ADR 0027's consequences.
7. Compose health checks (`api healthcheck`, `worker healthcheck`, every 10 s, every second while starting).
8. `api/openapi.yaml` describes `/healthz`, `/readyz` and `/metrics`, the last missing since block 25.

## Verification

- Packages with `-race`: `health`, `httpx`, `httpapi`, `cmd/api`, `cmd/worker` green; `golangci-lint` 0 issues.
- Live: `make up` returned in 27 s with every service healthy. With PostgreSQL stopped, the API's `/readyz` answered
  503 "postgres did not answer", `/healthz` 200, and `api healthcheck` exited 1; the worker stopped and was restarted
  by Compose. Two seconds after PostgreSQL returned both were ready. `make smoke` passed; no successful probe appeared
  in the API's info log.

## Follow-ups

Block 27 carries the correlation ID across the queue, from the API's request to the worker's log lines.
