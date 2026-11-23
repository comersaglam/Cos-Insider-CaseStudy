# 0010. Local environment with Docker Compose

- Status: Accepted; amended by [0019](0019-container-image-and-the-one-command-stack.md) (Compose runs the service)
- Date: 2026-11-23

## Context

The service needs PostgreSQL and RabbitMQ to run. Tests already start their own throwaway containers through
testcontainers ([ADR 0002](0002-testing-strategy.md)), so they need no running stack. What a developer still
needs, from the first database block on, is a stack to run the service against by hand and to inspect: tables in
`psql`, queues in the RabbitMQ management UI. The assessment also asks for a Docker Compose setup.

The remaining risk is drift: if the stack runs one PostgreSQL version and the tests another, a query can pass in
one and fail in the other.

Options considered:

| Option | For | Against |
|---|---|---|
| **Compose for the dependencies now, the application's image later** | A stack to inspect from the first database block; versions fixed before any SQL is written | One small block before the domain work |
| Everything in the one-command-stack block | Nothing done before it is needed | No stack to inspect while the store and API are built; ad-hoc `docker run` in the meantime |
| A dev container (Go itself in a container) | The toolchain is identical on every machine | Go already pins its version in `go.mod` and builds the same everywhere; testcontainers from inside a container needs the Docker socket mounted and extra settings on macOS |

## Decision

- **`compose.yaml` at the repository root** runs PostgreSQL and RabbitMQ (with the management UI). Its name is the
  one Docker looks for, so `docker compose up` works with no flags. `make up` starts it and waits until both
  services report healthy; `make down` stops it and keeps the data.
- **The application's `Dockerfile` also lives at the root**, added with the one-command-stack block, when the
  binaries have something to run. A `deploy/` directory is created only when a service needs a configuration file
  mounted into it (for example a Prometheus configuration).
- **Image tags are pinned to a patch release** (`postgres:18.7-alpine`, `rabbitmq:4.4.1-management-alpine`).
  A new major version is taken once it has had its first patch release: PostgreSQL 19.0 was out, so 18.7 was
  chosen.
- **One set of images for the stack and the tests.** `internal/platform/testenv` names the images the tests start,
  and `TestComposeRunsTheTestImages` fails when `compose.yaml` runs a different tag.
- **Ports are bound to `127.0.0.1`**, so the stack is not reachable from the network, and each port can be moved
  through `.env` when it is taken on the machine.
- **Credentials are local-only defaults** written in `compose.yaml`; `.env.example` lists what can be overridden.
- **Health checks test what a client needs**: `pg_isready` over TCP (the server the image runs while initialising
  listens only on a Unix socket, so a socket check would pass too early), and `rabbitmq-diagnostics
  check_port_connectivity` (the AMQP listener accepts connections, not merely the node process running).
- **RabbitMQ runs as the `rabbitmq` user with a fixed host name.** The health check runs as the container's user;
  as root, its first run could create the Erlang cookie before the server did, readable by root alone, and the
  server failed to start. RabbitMQ names its node, and the data stored under it, after the host name, so a fixed
  one keeps the data when the container is re-created.

## Consequences

- From the database block on, the same tables and queues the tests use can be inspected in a running stack.
- A version bump is one change in two places, and the test makes it impossible to change only one of them.
- PostgreSQL 18 keeps its data in a versioned directory below `/var/lib/postgresql`; the volume is mounted there,
  not at the older `/var/lib/postgresql/data`.

### Deliberately not done

| Not done | Why not |
|---|---|
| Floating tags (`latest`, `18`) | They change underneath the project; two machines could run different versions. |
| Pinning images by digest | The strictest pin, but meant to be kept current by a bot such as Renovate; a patch tag is enough here. |
| Tests starting the stack through the testcontainers Compose module | Slower, a much larger dependency, and tests would share one database; the drift it would prevent is caught by the image test. |
| Everything under `deploy/` | Every command would need `-f deploy/compose.yaml`, and a reviewer typing `docker compose up` at the root would get nothing. |
| A dev container | See the options above. |
