.DEFAULT_GOAL := help

.PHONY: help fmt lint verify test build check generate diagram up deps down smoke load migrate api

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

fmt: ## Format the code (gofmt, goimports)
	golangci-lint fmt ./...

lint: ## Run the linters, including the architecture rules (depguard)
	golangci-lint run ./...

# CI has no sqlc installed and runs it through go run at the same version: SQLC="go run ...sqlc@v1.31.1".
SQLC ?= sqlc
GOVULNCHECK_VERSION := v1.10.0

verify: ## Check that modules are tidy, the query code is generated, and no known vulnerability reaches the code
	go mod tidy -diff
	$(SQLC) diff
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# A hung test fails after TEST_TIMEOUT instead of Go's default of ten minutes.
PKG ?= ./...
TEST_TIMEOUT ?= 3m

test: ## Run the tests with the race detector; one package with PKG=./internal/notification/...
	go test -race -timeout $(TEST_TIMEOUT) $(PKG)

build: ## Build both binaries into bin/
	go build -o bin/ ./cmd/...

check: verify lint test ## Everything CI runs

generate: ## Regenerate the typed query code from the SQL (sqlc)
	sqlc generate

# D2 runs through go run at a pinned version, so nothing is installed.
D2_VERSION := v0.7.2

diagram: ## Render the architecture diagram, docs/architecture.d2, to docs/images/architecture.svg (D2)
	go run oss.terrastruct.com/d2@$(D2_VERSION) docs/architecture.d2 docs/images/architecture.svg

up: ## Build and start the whole service and wait until it is healthy (API on :8080, worker's metrics on :9090)
	docker compose up --detach --build --wait

deps: ## Start only PostgreSQL and RabbitMQ, for running the API from source (make api)
	docker compose up --detach --wait postgres rabbitmq

down: ## Stop the local stack, keeping its data (docker compose down --volumes deletes it)
	docker compose down

smoke: ## Create and read a notification through the running API
	scripts/smoke.sh

# The load test's workload: how many, its seed, and the shares of SMS the stand-in answers 503 and 422.
N ?= 3000
SEED ?= 1
UNAVAILABLE ?= 0
REJECTED ?= 0

load: ## Send a seeded burst to the running stack and measure how it drains (N, SEED, UNAVAILABLE, REJECTED)
	go run ./scripts/loadtest -n $(N) -seed $(SEED) -unavailable $(UNAVAILABLE) -rejected $(REJECTED)

# The local stack's database, as compose.yaml starts it; set DATABASE_URL to use another.
DATABASE_URL ?= postgres://notifications:notifications@localhost:5432/notifications?sslmode=disable

migrate: ## Apply the database migrations (to the local stack by default)
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/api migrate

api: ## Run the API from source against the local stack (make deps first)
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/api
