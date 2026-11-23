# 0030. Continuous integration and delivery

- Status: Accepted
- Date: 2026-11-23

## Context

The brief's bonus asks for CI/CD on GitHub Actions with automated tests and linting. Two choices were the owner's:
how far delivery goes, and how the workflow's third-party actions are pinned. An action is code that runs on the
runner with the job's token; in March 2025 the tags of `tj-actions/changed-files` were moved to a commit that
printed secrets into logs, and every workflow that named the action by tag ran it.

Worked example: a commit is pushed to `main`, then the tag `v1.0.0`.

| Delivery | On `main` | On `v1.0.0` | What leaves the repository |
|---|---|---|---|
| CI only | lint, test, build the image | the same | nothing |
| **CI, and the image published on a `v*` tag** | lint, test, build the image | the same, then the image is published as `1.0.0` and `1.0` | an image, only when the owner tags a release |
| CI, and the image published on every push to `main` | the same, then the image is published as `main` | as above | an image on every push |

| Pinning | A moved tag | Staying current |
|---|---|---|
| **A full commit SHA, with Dependabot** | cannot change what runs | weekly, grouped update proposals |
| A version tag (`@v7`) | runs whatever the tag now names | automatic |
| A full commit SHA alone | cannot change what runs | by hand, so it ages |

## Decision

- **`.github/workflows/ci.yml`** runs on every push to `main`, every pull request and every `v*` tag:
  - **lint**: `make verify` (modules tidy; the query code current with its SQL, `sqlc diff`; no known
    vulnerability reaching the code, `govulncheck`), then golangci-lint 2.15.0, the version used locally, with the
    repository's configuration (the architecture rules included). sqlc 1.31.1 and govulncheck 1.10.0 run through
    `go run` at pinned versions, so the runner installs nothing; `make check` runs the same locally;
  - **test**: `make test`, the whole suite with the race detector; testcontainers starts PostgreSQL and RabbitMQ
    on the runner's Docker;
  - **image**: builds the image with BuildKit, its layers cached in GitHub's cache, and does not push it;
  - **publish**, on a `v*` tag only and after the three others pass: builds and pushes the image to
    `ghcr.io/<owner>/<repository>` in lower case, tagged with the version (`1.0.0`) and its minor line (`1.0`),
    with OCI labels linking it to the source.
- **Least privilege**: the workflow's token is read-only; only the publish job may write packages. Checkouts do not
  keep the token. A newer push to a pull request cancels its older run.
- **Every action is pinned to a full commit SHA**, resolved from its release tag and checked to exist, with the
  version in a comment beside it.
- **`.github/dependabot.yml`** proposes updates weekly, one grouped pull request per ecosystem: Go modules, the
  workflow's actions, and the Dockerfile's base images (the distroless image's digest included). The images in
  `compose.yaml` are left out: `internal/platform/testenv` names the same tags and a test keeps them equal, so
  they move together by hand.

## Consequences

- A push shows, within minutes, whether the code lints, passes every test with the race detector and builds.
- Nothing is published until the owner pushes a version tag.
- Nothing runs until the repository is pushed; the workflow's first run is the check that it is right. Every
  command it runs was run locally first.
- The first `govulncheck` run found two advisories in indirect dependencies, both fixed upstream: a patch release
  of `klauspost/compress`, 1.20.1 → 1.20.2 (GO-2026-6865, reached through Prometheus's handler), and a minor
  release of OpenTelemetry, 1.44.0 → 1.47.0 (GO-2026-6974, reached through testcontainers, in tests only; it moved
  `go-logr/logr` to 1.4.4 and added `otel/log` with it). Neither is a new dependency. The scan is now clean.

### Deliberately not done

| Not done | Why not |
|---|---|
| Deploying anywhere | There is no environment to deploy to; the image is the deliverable. |
| A matrix of Go versions or operating systems | The service is built with one pinned Go version into one Linux image. |
| A separate workflow for releases | One workflow with a tag-only job keeps the release behind the same checks. |
