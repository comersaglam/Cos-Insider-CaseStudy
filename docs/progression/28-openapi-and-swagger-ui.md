# 28. OpenAPI and Swagger UI

- Date: 2026-11-23
- Commits: `9fe660f`
- Decisions: [ADR 0029](../adr/0029-contract-tests-and-documentation-page.md)

## Goal

A detailed contract that cannot drift from the code, and a documentation page a reviewer can open.

## Steps

1. Researched the options and asked the owner, who chose contract tests with `getkin/kin-openapi` v0.153.0 (approved
   as a test dependency) and Swagger UI at `/docs` from a CDN with integrity hashes.
2. Pinned Swagger UI 5.36.0: checked npm's tarball against its published integrity, checked that jsDelivr serves
   the same bytes, computed both SHA-384 hashes.
3. Tests first: `GET /openapi.yaml` serves the embedded contract, `GET /docs` the pinned page. Package `api`
   embeds the contract.
4. `api/contract`, tests first: the contract is valid with its examples; `Validate` accepts a documented answer and
   refuses an undocumented status and a body against its schema. Its first run found the batch request's items
   reusing the single create's rules, which a client checking requests would have refused; the items are now
   described as the server reads them.
5. Modules mount on `httpx.Mux`; the router records its routes, and a test compares them with the contract's
   operations both ways. It failed first on `/openapi.yaml` and `/docs`, then they were documented.
6. `contract.Check` added to the notification handlers' tests, the health and router tests and the API's
   end-to-end tests. They passed; requiring a member the server never sends made 9 answers fail, and one in
   every problem made 36 error answers fail, across 400, 404, 409, 413, 422 and 500, so the check bites.
7. `depguard` keeps kin-openapi and `api/contract` out of production code; the binaries link neither.
8. Collected 33 real answers from the running stack, every operation's success and errors, and rewrote the
   contract with them: an introduction (the delivery flow and its guarantee, conventions, the error codes table,
   limits), tags, every operation, parameter and member described, an example for every answer, `Problem.code` an
   enumeration. Breaking one example made the validity test fail. Reading the provider client corrected the
   overview: 408 and 429 are retried, as 5xx are.
9. Rendered `/docs` in headless Chrome from the running stack: Swagger UI loaded through its integrity hashes and
   shows the introduction, the tables and the operations by tag.

## Verification

`api/contract`, `internal/health`, `internal/httpapi`, `internal/notification` and `cmd/api` with `-race` green,
every answer checked against the contract; `golangci-lint` 0 issues.

## Follow-ups

Block 29, the README: setup, run and test through the make targets first, then the architecture diagram in D2.
