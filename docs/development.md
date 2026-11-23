# Development

How to work on this service day to day. Decisions behind the tools are in [docs/adr](adr/README.md).

## Tools

Go (version pinned in `go.mod`), Docker, `golangci-lint`, `sqlc`, `govulncheck`, and VS Code with the official Go
extension (`gopls`, format on save).

## Inner loop

1. Write or change a test, run it alone and see it fail: `go test ./internal/<package>/... -run TestName`.
2. Write the code until it passes.
3. Before committing, run the whole suite with the race detector: `make test`.
4. Run `make lint`; the linter's findings are fixed, not silenced.

## Make targets

| Target | What it does |
|---|---|
| `make fmt` | `gofmt` and `goimports` over the module |
| `make lint` | `golangci-lint run` |
| `make generate` | `sqlc generate` |
| `make test` | every test, with `-race`; starts its own PostgreSQL through testcontainers |
| `make up` / `make down` | the full stack with Docker Compose |
| `make migrate` | applies the migrations to the Compose database |

## Go practices this codebase relies on

- **Errors are values.** Every error is returned, wrapped with context (`fmt.Errorf("claim batch: %w", err)`)
  and checked upstream with `errors.Is` or `errors.As`; none is ignored.
- **`context.Context` is the first parameter** of every function that does I/O, and carries cancellation and
  deadlines; every outbound call and query has a timeout.
- **Every goroutine has an owner that stops it** (context cancellation and a `sync.WaitGroup`); shutdown
  finishes in-flight work before the process exits.
- **Interfaces are small and declared where they are used**; constructors return concrete types.
- **Dependencies are wired by hand** in each `cmd/*/main.go`.
