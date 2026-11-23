# 09. Local environment

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0010](../adr/0010-local-environment-with-docker-compose.md)

## Goal

Have PostgreSQL and RabbitMQ running locally before the first database block, on exactly the versions the tests
will use, so nothing behaves differently between the stack and the test suite.

## Steps

1. Inserted this block before the notification domain; the roadmap's later blocks moved up by one, and the
   one-command-stack block keeps only the application's image and services.
2. Chose the newest releases that have had a patch release: `postgres:18.7-alpine` (19.0 had none yet) and
   `rabbitmq:4.4.1-management-alpine`; confirmed from the images that PostgreSQL 18 keeps its data below
   `/var/lib/postgresql`.
3. Wrote `TestComposeRunsTheTestImages` with the image constants in `internal/platform/testenv`, and saw it fail
   because `compose.yaml` did not exist.
4. Wrote `compose.yaml` at the root (pinned images, health checks, named volumes, ports on `127.0.0.1` and movable
   through `.env`), `.env.example`, and the `make up` and `make down` targets. The test passed; changing one tag in
   `compose.yaml` made it fail with the drifted image named.
5. The first `make up` failed: RabbitMQ could not read `/var/lib/rabbitmq/.erlang.cookie`. The health check, run as
   root, had created the cookie before the server, readable by root alone. Running the service as the `rabbitmq`
   user fixed it.
6. Recorded the decisions as ADR 0010; documented the stack in `docs/development.md`.

## Verification

- `go test ./internal/platform/testenv/` passes, and fails when a tag in `compose.yaml` is changed.
- `make up` reports both services healthy; `psql` answers with PostgreSQL 18.7; the management API answers with
  RabbitMQ 4.4.1 on node `rabbit@rabbitmq`.
- A table created before `make down` is still there after the next `make up`.
- `make check` passes.

## Follow-ups

Block 10, the notification domain, starts with its open questions: the SMS limit, the e-mail subject, the push
recipient format and phone number format.
