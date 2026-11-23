# 0029. Contract tests and the documentation page

- Status: Accepted
- Date: 2026-11-23
- Builds on: [0016](0016-http-api-and-the-api-process.md) (a hand-written OpenAPI 3.1 contract, design first)

## Context

`api/openapi.yaml` is the API's contract, written by hand since block 15. The owner asked for it to be detailed:
every operation, parameter and member described, examples for every request and answer, every error code where it
can occur. The more a hand-written contract says, the more it can drift from the code: a member renamed in a
response, a status code added to a handler, a rule the server does not enforce. CONTRIBUTING also promises a test
that every route the server registers is in the contract. And the brief asks for documentation a reviewer can open.

Worked example: the contract says `GET /v1/notifications/{id}` answers `attempts`; someone renames it
`attemptCount` in the code.

| How the contract is tied to the code | The rename | Spec valid | Every route documented | Cost |
|---|---|---|---|---|
| **A validator library, in tests** | caught: the answer lacks `attempts` | yes | yes | a test dependency |
| Routes only, with a YAML parser | missed | no | yes | none |
| A linter on the spec (Redocly CLI) | missed | yes, with style rules | no | a pinned image |

| How the documentation page is served | Address | In the repository | "Try it out" | Offline |
|---|---|---|---|---|
| **`/docs`, Swagger UI from a CDN, pinned with integrity hashes** | the API's port | one HTML file | same origin | no |
| `/docs`, Swagger UI's files vendored and embedded | the API's port | about 1.5 MB of third-party files | same origin | yes |
| A Swagger UI container | another port | a Compose service | cross-origin: the API needs CORS | yes |

## Decision

- **`github.com/getkin/kin-openapi` v0.153.0, in tests only**, the most used Go OpenAPI library, with OpenAPI 3.1 and
  JSON Schema 2020-12. The package `api/contract` wraps it; `depguard` keeps it and kin-openapi out of
  production code, and the binaries link neither.
  - **The contract is valid**: it parses, is valid OpenAPI 3.1, and every example matches its schema.
  - **Every answer the tests see is documented**: the notification handlers' tests, the health and router tests,
    and the API's end-to-end tests check each answer with `contract.Check`: its status must be documented for its
    operation, and its body must match the schema. A request no operation matches is left to the next check.
  - **The routes and the operations are the same set**: modules mount on `httpx.Mux`, an interface an
    `*http.ServeMux` satisfies, so the router can record each pattern; a test compares them with the contract's
    operations both ways.
- **`api/openapi.yaml` is embedded** (package `api`) and served at `GET /openapi.yaml` as `application/yaml`.
- **`GET /docs`** serves a page that loads Swagger UI 5.36.0 from jsDelivr with Subresource Integrity hashes. The
  hashes are of npm's published files, checked against the CDN's when the version was pinned; a browser refuses a
  file that differs. The contract's first server is `/`, so "Try it out" calls the API serving the page.
- **The contract made detailed**: an introduction with the delivery flow and its guarantee, the conventions, a table
  of every error code and its status, and the limits; tags grouping the operations; every operation, parameter and
  member described; every answer, errors included, with an example taken from the running service; `Problem.code`
  an enumeration of the fourteen codes.
- **Batch items are described as the server reads them**: their members' JSON types only, each item's rules
  checked one by one. The validator found that the item schema reused the single create's rules, so a client
  checking requests against the contract would have refused a batch the server accepts in part.

## Consequences

- A change that makes the code and the contract disagree fails a test: a renamed member, an undocumented status
  or error code, a route added without its operation, an example that no longer matches.
- Reviewers read the contract at `http://localhost:8080/docs` once `make up` has run; the page needs the internet.
- Updating Swagger UI means changing its version and both hashes in `internal/httpapi/docs.html`
  (`docs/development.md` says how).

### Deliberately not done

| Not done | Why not |
|---|---|
| Validating requests against the contract at run time | The handlers validate, with the error codes clients rely on; a second validator in front would answer the same mistakes differently. |
| Generating code from the contract (oapi-codegen) | The handlers and their types are small and hand-written; a generator is one more tool for no rule the tests do not already check. |
| A linter on the spec's style | The validator checks what matters to clients; style rules can join CI later. |
| A Content-Security-Policy on the page | It serves no data of its own; the integrity hashes already pin the only third-party code it runs. |
