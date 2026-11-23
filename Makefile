.DEFAULT_GOAL := help

.PHONY: help fmt lint test build check

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

fmt: ## Format the code (gofmt, goimports)
	golangci-lint fmt ./...

lint: ## Run the linters, including the architecture rules (depguard)
	golangci-lint run ./...

test: ## Run every test with the race detector
	go test -race ./...

build: ## Build both binaries into bin/
	go build -o bin/ ./cmd/...

check: lint test ## Everything CI runs
