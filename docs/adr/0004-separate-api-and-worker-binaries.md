# 0004. Separate API and worker binaries from one image

- Status: Accepted
- Date: 2026-11-23

## Context

The API takes requests; the worker consumes queues, calls the provider and runs the outbox relay and the
scheduler. Their load differs: a flash sale stresses the workers, not the API.

| Option | Why it was or was not chosen |
|---|---|
| **Two binaries, `cmd/api` and `cmd/worker`, built into one image** | Chosen: each scales and fails on its own; this is Go's `cmd/` convention. |
| One process running both | Simplest, but scaling and failures are coupled. |
| One binary with subcommands (`notifier api`, `notifier worker`) | Equivalent at run time; adds flag parsing for no gain over the `cmd/` convention. |

## Decision

Two `package main` programs, `cmd/api/main.go` and `cmd/worker/main.go`, each wiring its own dependencies. One
multi-stage Dockerfile builds both; Docker Compose runs them as two services from that image.

## Consequences

- Workers scale with `docker compose up --scale worker=N` without touching the API.
- The wiring in the two `main` files repeats a few lines (configuration, logger, database pool); shared setup
  lives in small packages, not in a framework.
