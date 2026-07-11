.PHONY: help build build-arbitrage build-migrate build-ubuntu package-ubuntu run run-migrate migrate test tidy clean

MODULE      := github.com/brianliu-sysu/uniswapv3
CONFIG      ?= configs/config.yaml
MIGRATIONS  ?= migrations
BIN_DIR     := bin
DIST_DIR    := dist
ARBITRAGE   := $(BIN_DIR)/arbitrage
MIGRATE_BIN := $(BIN_DIR)/migrate

# Ubuntu / Linux amd64 release package
GOOS_LINUX      := linux
GOARCH_LINUX    := amd64
UBUNTU_BIN_DIR  := $(DIST_DIR)/ubuntu-$(GOARCH_LINUX)
UBUNTU_BIN      := $(UBUNTU_BIN_DIR)/arbitrage
UBUNTU_ZIP      := $(DIST_DIR)/arbitrage-ubuntu-$(GOARCH_LINUX).zip

GO          := go
GOFLAGS     ?=
LDFLAGS     ?= -s -w

help: ## Show available targets
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: build-arbitrage build-migrate ## Build all binaries

build-arbitrage: ## Build arbitrage sync binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(ARBITRAGE) ./cmd/arbitrage/

build-migrate: ## Build database migrate binary
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(MIGRATE_BIN) ./cmd/migrate

build-ubuntu: ## Cross-compile arbitrage for Ubuntu (linux/amd64)
	@mkdir -p $(UBUNTU_BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS_LINUX) GOARCH=$(GOARCH_LINUX) \
		$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(UBUNTU_BIN) ./cmd/arbitrage/
	@echo "built $(UBUNTU_BIN)"

package-ubuntu: build-ubuntu ## Build Ubuntu binary and zip it with configs/config.yaml
	@test -f $(CONFIG) || (echo "missing config: $(CONFIG)" && exit 1)
	@mkdir -p $(UBUNTU_BIN_DIR)/configs
	cp $(CONFIG) $(UBUNTU_BIN_DIR)/configs/config.yaml
	@rm -f $(UBUNTU_ZIP)
	cd $(UBUNTU_BIN_DIR) && zip -r ../$(notdir $(UBUNTU_ZIP)) arbitrage configs/config.yaml
	@echo "package ready: $(UBUNTU_ZIP)"

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
	rm -rf $(BIN_DIR) $(DIST_DIR)
