# 0027. Health probes: liveness, readiness, and a health check without a shell

- Status: Accepted
- Date: 2026-11-23

## Context

An orchestrator asks a process two different questions. **Liveness**: is the process stuck, so that restarting it
would help? **Readiness**: can it do its work now, so that traffic or dependants may rely on it? Kubernetes probes
each separately; Compose has one health check per container, which `depends_on: service_healthy` and
`docker compose up --wait` read. ADR 0019 left the API's health check for this block.

The image is distroless (ADR 0019): it has no shell and no `curl`, so a health check cannot be
`curl -f http://localhost:8080/readyz`.

| Option for the container's check | For | Against |
|---|---|---|
| **The binary asks itself: `api healthcheck`** | No new file in the image; the standard library's HTTP client; the same address setting as the server | A subcommand to maintain |
| Copy a static `curl` or `wget` into the image | The familiar command | A second binary to keep patched, in an image chosen for having none |
| An image with a shell (`alpine`) | Anything can be scripted | Reverses ADR 0019 |

## Decision

- **`GET /healthz`** answers 200 `{"status":"ok"}` while the process runs and checks nothing: a database outage is
  not cured by restarting the API, and a liveness probe that checks dependencies restarts every replica at once.
- **`GET /readyz`** runs every dependency's check at once, each within two seconds, and answers 200 with each check
  `ok`, or a 503 problem (`not_ready`, the one error envelope) whose detail names the failed dependencies. Why a check
  failed is logged at warn level, never answered: the error can hold a host or a user name.
  - The API checks PostgreSQL (`pool.Ping`); it never talks to RabbitMQ.
  - The worker checks PostgreSQL and its RabbitMQ connection, on its operations server (`OPS_ADDR`, beside
    `/metrics`).
- **The probes live in `internal/health`**, mounted by `httpapi` for the API and by the worker's composition root.
- **`api healthcheck` and `worker healthcheck`** ask the process in the same container for `/readyz` on
  `127.0.0.1`, at the port of `HTTP_ADDR` or `OPS_ADDR`, and exit 0 or 1 (`httpx.Probe`). Compose runs them every
  10 seconds, every second while the container starts, and marks the container unhealthy after three failures.
  `make up` (`--wait`) now returns once the API and the worker are ready.
- **Probes and scrapes stay out of the access log while they succeed**: `httpx.AccessLog` takes the quiet paths
  (`/healthz`, `/readyz`, `/metrics`) and logs their successes at debug level; a failure is logged like any request.

## Consequences

- `docker compose up --wait` means the service works, not only that its containers started.
- With PostgreSQL stopped, the API answers `/readyz` with 503 and stays alive; the worker stops (ADR 0009, crash-only)
  and Compose restarts it until the database is back. Both were ready again two seconds after it returned.
- Stopping can take up to five seconds longer when a client holds a connection it has opened but not used: Go's
  `http.Server.Shutdown` counts such a connection idle only after five seconds. The default `SHUTDOWN_TIMEOUT` of 15
  seconds covers it.

### Deliberately not done

| Not done | Why not |
|---|---|
| Dependencies in the liveness probe | A shared outage would restart every replica, which cures nothing and adds load as they come back. |
| The error's text in the readiness answer | It can name hosts and users; the log has it, with the correlation ID. |
| A check of the provider | The provider is outside the service; the circuit breaker (ADR 0007) and the metrics already watch it. A down provider makes notifications wait, not the worker unready. |
| Restarting an unhealthy container from Compose | Compose does not; an orchestrator does, from the same probes. The worker already exits on a lost dependency. |
