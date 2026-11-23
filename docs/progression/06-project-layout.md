# 06. Project layout

- Date: 2026-11-23
- Commits: `ad58ebb`
- Decisions: [ADR 0008](../adr/0008-modular-monolith-layout.md)

## Goal

Lay out the code so each module can be built and tested on its own, with one central router that mounts them, and
make the layout's rules fail the build when broken.

## Steps

1. Compared packaging by layer, by feature, and full hexagonal by what adding the cancel endpoint touches; chose a
   modular monolith (packages by feature, a platform layer, a central router) with lightweight hexagonal inside
   each module.
2. Ran `go mod init github.com/comersaglam/Cos-Insider-CaseStudy`.
3. Created `cmd/api`, `cmd/worker`, `internal/httpapi`, the modules `notification`, `delivery`, `dispatch`,
   `health`, and `internal/platform/{config,postgres,rabbitmq,httpx,observability}`, each with a `doc.go` stating
   what the package owns.
4. Wrote `.golangci.yml` (v2) with `depguard` rules for the import directions and `revive` rules for doc comments.
5. Wrote the `Makefile` (`help`, `fmt`, `lint`, `test`, `build`, `check`) and updated `CONTRIBUTING.md` and
   `docs/development.md` with the layout.

## Verification

- `go build ./...` and `go vet ./...` pass; `make check` reports 0 lint issues (no tests exist yet).
- The rules were seen failing: an import of `notification` from `platform/config` and of `delivery` from
  `notification` each failed `depguard` with the ADR's reason, and an undocumented exported function failed
  `revive`. The violations were removed afterwards.

## Follow-ups

The first implementation block: the `notification` domain (types, status transitions, content validation),
tests first.
