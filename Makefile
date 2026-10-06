# Everything runs inside Docker (no Go on the host). `make help` lists the targets.
-include .env
export

COMPOSE := docker compose
GO      := $(COMPOSE) --profile tools run --rm tools
# Packages measured by the coverage gate (test helpers are excluded, see README).
COVERPKG = $$(go list ./internal/... | grep -v -e /testdb -e /repotest | paste -sd, -)

.DEFAULT_GOAL := help
.PHONY: help build test test-unit coverage lint up down clean tidy

help: ## Show this help
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-10s %s\n", $$1, $$2}'

build: ## Compile everything
	$(GO) go build ./...

test-unit: ## Unit tests only, with the race detector (no database needed)
	$(GO) sh -c 'REQUIRE_DB= go test -race ./...'

test: ## ALL tests (unit + PostgreSQL integration) with -race; fails if coverage gates are not met
	$(GO) sh -c 'go test -race -count=1 -coverpkg=$(COVERPKG) -coverprofile=coverage.out ./... && sh scripts/coverage-gate.sh coverage.out'

coverage: test ## Same as test, plus per-function report (coverage.html)
	$(GO) sh -c 'go tool cover -func=coverage.out | tail -n 40 && go tool cover -html=coverage.out -o coverage.html'

lint: ## go vet + staticcheck + gofmt check
	$(GO) sh -c 'test -z "$$(gofmt -l .)" || (gofmt -l . && echo "gofmt: files above need formatting" && exit 1); go vet ./... && staticcheck ./...'

tidy: ## go mod tidy
	$(GO) go mod tidy

up: ## Start API + PostgreSQL (http://localhost:$(API_PORT), docs at /docs)
	$(COMPOSE) up --build -d --wait

down: ## Stop the stack (keeps the database volume)
	$(COMPOSE) --profile tools down

clean: ## Stop the stack, DELETE its volumes/images and the coverage files
	$(COMPOSE) --profile tools down -v --rmi local
	rm -f coverage.out coverage.html
