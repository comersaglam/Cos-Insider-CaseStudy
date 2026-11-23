# 0008. Modular monolith: packages by feature, a platform layer and one central router

- Status: Accepted
- Date: 2026-11-23

## Context

The code must let each part of the system be built, read and tested on its own, with one visible place where the
parts are connected. Three layouts were considered, judged by what adding the cancel endpoint touches:

| Layout | Cancel touches | Why it was or was not chosen |
|---|---|---|
| By layer (`handler/`, `service/`, `repository/`) | three folders | Familiar from Django and Spring, but a feature is spread across the tree; Go practice calls this packaging by layer and advises against it. |
| **By feature, with a platform layer and a central router** | `internal/notification/` | Chosen: each module holds its handlers, use cases and storage; the Go blog's "Organizing a Go module" and Ben Johnson's standard package layout point this way. |
| Full hexagonal (`core/domain`, `core/ports`, `adapters/...`) | four or five folders | The strictest separation, but heavy for a service this size and often criticised in Go as a transplanted Java structure. |

## Decision

```
cmd/api, cmd/worker      composition roots: build config, platform, modules; start servers and loops
internal/httpapi         the central router: middleware chain and the one place modules are mounted
internal/notification    module: notification lifecycle (create, batch, get, list, cancel, retry)
internal/delivery        module: send one notification (consumer, rate limit, provider, retry, breaker)
internal/dispatch        module: outbox relay and scheduler
internal/health          liveness and readiness
internal/platform/...    infrastructure without business meaning: config, postgres, rabbitmq, httpx, observability
migrations, api/openapi.yaml, docs
```

- **Lightweight hexagonal inside each module**: a module declares the interfaces it needs (its ports) next to the
  code that uses them; the PostgreSQL, RabbitMQ and HTTP implementations satisfy them.
- **Each module mounts its own routes** through `RegisterRoutes(mux, ...)`; `httpapi.NewRouter` calls them and
  wraps the mux in the middleware chain. Adding or removing a module is one line there.
- **Dependency rules**, enforced by `depguard` in `golangci-lint`:
  - `platform` imports no module;
  - `notification` imports no other module;
  - `delivery` and `dispatch` may import `notification`, not each other;
  - only `cmd/*` and `httpapi` assemble several modules.

## Consequences

- A module can be understood and tested from its own folder.
- A forbidden import fails the lint step in CI, so the architecture cannot drift silently.
- Modules share one database; `notification` owns its tables, and other modules change them only through
  functions `notification` exports.
