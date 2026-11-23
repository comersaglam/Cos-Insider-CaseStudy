# 18. One-command stack

- Date: 2026-11-23
- Commits: `b569c52`
- Decisions: [ADR 0019](../adr/0019-container-image-and-the-one-command-stack.md), amending
  [ADR 0010](../adr/0010-local-environment-with-docker-compose.md)

## Goal

Make `docker compose up` run the service itself: build its image, apply the migrations, then serve the API.

## Steps

1. No question needed the owner. Pulled `golang:1.27.4-alpine` (the version in `go.mod`) and
   `gcr.io/distroless/static-debian13:nonroot`, which has no version tags and is pinned by digest.
2. Wrote the multi-stage `Dockerfile`: modules downloaded before the code is copied, BuildKit cache mounts,
   static binaries without local paths or symbols, a final image with both binaries running as `nonroot`.
   `.dockerignore` keeps history, documents and local settings out of the build context.
3. Added `migrate` (runs `api migrate` once PostgreSQL is healthy) and `api` (starts once `migrate` has completed,
   on `127.0.0.1:8080`) to `compose.yaml`, sharing one anchor. The worker waits for block 24, since its binary
   runs nothing yet and would restart in a loop; the API's health check waits for block 26.
4. `make up` now builds and starts everything; `make deps` starts only the dependencies for working from source;
   `make smoke` runs `scripts/smoke.sh`. `.env.example` gained `API_PORT` and `LOG_LEVEL`.

## Verification

- `make up` built the image and started the stack in 30 s; `migrate` applied the migrations and exited 0; `api`
  started after it.
- `make smoke`: a notification created (202), read back as `queued`, repeated with its key (202).
- The image is 23.5 MB and runs as `nonroot:nonroot`; `docker compose stop api` stopped it in 0.5 s with exit
  code 0 and the log line `stopped`.

## Follow-ups

Phase 2 starts with block 19, the RabbitMQ platform layer: connection, the topology of ADR 0012 (the direct
exchange, a queue per channel with priorities and single active consumer, the dead-letter queue) and publishing
with confirms.
