# 0019. The container image and the one-command stack

- Status: Accepted
- Date: 2026-11-23
- Amends: [0010](0010-local-environment-with-docker-compose.md) (Compose now runs the service, not only its
  dependencies)
- Amended by: [0025](0025-worker-in-compose-and-the-provider-stand-in.md) (the worker and a stand-in provider)

## Context

The brief asks for a one-command setup with Docker Compose. Until now Compose ran PostgreSQL and RabbitMQ, and
the API ran from source. The service needs an image, and Compose needs to start it in the right order: the
database healthy, the migrations applied, then the API.

### The final image

| Option | For | Against |
|---|---|---|
| **distroless `static`, `nonroot`** | Only CA certificates, time zone data and a non-root user; no shell or package manager for an attacker to use; about 2 MB | No shell to debug inside the container; `docker exec` gets nothing |
| `alpine` | A shell and `apk` for debugging; small | A package manager and a shell are an attack surface the service does not use |
| `scratch` | Nothing at all | CA certificates (for HTTPS to the provider) and a user have to be added by hand |

Go builds a static binary, so it needs no runtime in the image at all.

## Decision

- **A multi-stage `Dockerfile` at the root.** `golang:1.27.4-alpine`, the version in `go.mod`, builds both
  binaries with `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`; module and build caches are BuildKit cache mounts,
  and `go.mod` and `go.sum` are copied before the code so a code change does not download the modules again.
  The final stage is `gcr.io/distroless/static-debian13:nonroot`, pinned by digest because distroless has no
  version tags. The image holds `api` and `worker`, runs as `nonroot` (uid 65532) and starts `api` by default.
  It is 23.5 MB.
- **`docker compose up` runs the service.** Next to PostgreSQL and RabbitMQ, a `migrate` service runs
  `api migrate` once the database is healthy and exits; `api` starts once `migrate` has completed successfully
  and serves on `127.0.0.1:8080` (`API_PORT` moves it). Both share one YAML anchor for their image and settings.
  The worker joins when it runs something (the worker composition root block); the API's health check joins with
  the health block.
- **Make targets**: `make up` builds and starts everything and waits; `make deps` starts only PostgreSQL and
  RabbitMQ, for running the API from source with `make api`; `make smoke` runs `scripts/smoke.sh`, which creates a
  notification, reads it back and repeats it with its idempotency key.
- **`.dockerignore`** keeps the repository's history, documents and local settings out of the build context.

## Consequences

- One command gives a reviewer the running API, migrated, as it would run in production.
- `docker compose stop` sends SIGTERM; the API finishes its requests and exits 0 in under a second.
- A production image with no shell is debugged through its logs and metrics, not from inside.

### Deliberately not done

| Not done | Why not |
|---|---|
| A separate image per binary | The binaries share code and dependencies; one image is built once and runs either (ADR 0004). |
| Running as root | Nothing in the service needs it. |
| Building outside Docker and copying binaries in | The image would depend on the build machine's toolchain; the build stage fixes it. |
| `docker compose watch` or live reload | Development runs from source (`make deps`, `make api`); the image is what ships. |
