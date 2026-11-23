.DEFAULT_GOAL := help

.PHONY: help fmt lint test build check generate up deps down smoke migrate api

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

fmt: ## Format the code (gofmt, goimports)
	golangci-lint fmt ./...

lint: ## Run the linters, including the architecture rules (depguard)
	golangci-lint run ./...

# A hung test fails after TEST_TIMEOUT instead of Go's default of ten minutes.
PKG ?= ./...
TEST_TIMEOUT ?= 3m

test: ## Run the tests with the race detector; one package with PKG=./internal/notification/...
	go test -race -timeout $(TEST_TIMEOUT) $(PKG)

build: ## Build both binaries into bin/
	go build -o bin/ ./cmd/...

check: lint test ## Everything CI runs

generate: ## Regenerate the typed query code from the SQL (sqlc)
	sqlc generate

up: ## Build and start the whole service and wait until it is healthy (API on :8080, worker's metrics on :9090)
	docker compose up --detach --build --wait

deps: ## Start only PostgreSQL and RabbitMQ, for running the API from source (make api)
	docker compose up --detach --wait postgres rabbitmq

down: ## Stop the local stack, keeping its data (docker compose down --volumes deletes it)
	docker compose down

smoke: ## Create and read a notification through the running API
	scripts/smoke.sh

# The local stack's database, as compose.yaml starts it; set DATABASE_URL to use another.
DATABASE_URL ?= postgres://notifications:notifications@localhost:5432/notifications?sslmode=disable

migrate: ## Apply the database migrations (to the local stack by default)
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/api migrate

api: ## Run the API from source against the local stack (make deps first)
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/api
