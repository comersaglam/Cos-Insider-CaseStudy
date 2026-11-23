.DEFAULT_GOAL := help

.PHONY: help fmt lint test build check up down

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

up: ## Start PostgreSQL and RabbitMQ and wait until they are healthy
	docker compose up --detach --wait

down: ## Stop the local stack, keeping its data (docker compose down --volumes deletes it)
	docker compose down
