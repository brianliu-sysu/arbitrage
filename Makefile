.PHONY: help build build-arbitrage build-migrate run run-migrate migrate test tidy clean

MODULE      := github.com/brianliu-sysu/uniswapv3
CONFIG      ?= configs/config.yaml
MIGRATIONS  ?= migrations
BIN_DIR     := bin
ARBITRAGE   := $(BIN_DIR)/arbitrage
MIGRATE_BIN := $(BIN_DIR)/migrate

GO          := go
GOFLAGS     ?=

help: ## Show available targets
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: build-arbitrage build-migrate ## Build all binaries

build-arbitrage: ## Build arbitrage sync binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(ARBITRAGE) ./cmd

build-migrate: ## Build database migrate binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(MIGRATE_BIN) ./cmd/migrate

run: build-arbitrage ## Run arbitrage sync service
	$(ARBITRAGE) -config $(CONFIG)

run-migrate: build-migrate ## Run database migrations once
	$(MIGRATE_BIN) -config $(CONFIG) -migrations $(MIGRATIONS)

migrate: run-migrate ## Alias for run-migrate

test: ## Run all tests
	$(GO) test $(GOFLAGS) ./...

tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
