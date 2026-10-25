GO ?= go
BIN ?= dist/multiversectl
SERVER_BIN ?= dist/multiversectl-server
PREFIX ?= $(HOME)/.local/bin
SCHEMA = schema/project.v1alpha1.yaml
TYPES = internal/adapters/manifest/v1alpha1/types.go
CATALOG_SCHEMA = schema/catalog.v1alpha1.yaml
ROOT_SCHEMA = schema/catalogroot.v1alpha1.yaml
CATALOG_TYPES = internal/adapters/catalog/v1alpha1/types.go

.PHONY: help build install test lint schema schema-check docs docs-check tidy clean
.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Build both binaries into dist/
	$(GO) build -o $(BIN) ./cmd/multiversectl
	$(GO) build -o $(SERVER_BIN) ./cmd/multiversectl-server

install: ## Build and install into $(PREFIX) (default ~/.local/bin)
	$(GO) build -o $(PREFIX)/multiversectl ./cmd/multiversectl
	$(GO) build -o $(PREFIX)/multiversectl-server ./cmd/multiversectl-server
	@echo "installed $(PREFIX)/multiversectl and $(PREFIX)/multiversectl-server"

test: ## Run the tests
	$(GO) test ./...

lint: ## gofmt and go vet
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || { echo "gofmt needed"; exit 1; }
	$(GO) vet ./...

schema: ## Generate $(SCHEMA) and $(CATALOG_SCHEMA) from the Go types
	$(GO) run ./internal/tools/schemagen $(TYPES) > $(SCHEMA)
	$(GO) run ./internal/tools/schemagen -kind Catalog $(CATALOG_TYPES) > $(CATALOG_SCHEMA)
	$(GO) run ./internal/tools/schemagen -kind CatalogRoot $(CATALOG_TYPES) > $(ROOT_SCHEMA)
	@echo "wrote $(SCHEMA) $(CATALOG_SCHEMA) $(ROOT_SCHEMA)"

docs: ## Render docs/ from docsrc/
	$(GO) run ./internal/tools/docgen docsrc docs

docs-check: docs ## Fail if the committed docs are stale
	@git diff --exit-code -- docs \
		|| { echo "docs/ is stale — commit the regenerated pages"; exit 1; }

schema-check: schema ## Fail if the committed schema is stale
	@git diff --exit-code -- $(SCHEMA) $(CATALOG_SCHEMA) $(ROOT_SCHEMA) \
		|| { echo "schema/ is stale — commit the regenerated files"; exit 1; }

tidy: ## Tidy go.mod
	$(GO) mod tidy

clean: ## Remove build output
	rm -rf dist
