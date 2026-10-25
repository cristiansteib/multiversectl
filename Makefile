GO ?= go
BIN ?= dist/multiversectl
PREFIX ?= $(HOME)/.local/bin
SCHEMA = schema/workspace.v1alpha1.yaml
TYPES = internal/adapters/manifest/v1alpha1/types.go

.PHONY: help build install test lint schema schema-check docs docs-check tidy clean
.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary into dist/
	$(GO) build -o $(BIN) ./cmd/multiversectl

install: ## Build and install into $(PREFIX) (default ~/.local/bin)
	$(GO) build -o $(PREFIX)/multiversectl ./cmd/multiversectl
	@echo "installed $(PREFIX)/multiversectl"

test: ## Run the tests
	$(GO) test ./...

lint: ## gofmt and go vet
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || { echo "gofmt needed"; exit 1; }
	$(GO) vet ./...

schema: ## Generate $(SCHEMA) from the Go types
	$(GO) run ./internal/tools/schemagen $(TYPES) > $(SCHEMA)
	@echo "wrote $(SCHEMA)"

docs: ## Render docs/ from docsrc/
	$(GO) run ./internal/tools/docgen docsrc docs

docs-check: docs ## Fail if the committed docs are stale
	@git diff --exit-code -- docs \
		|| { echo "docs/ is stale — commit the regenerated pages"; exit 1; }

schema-check: schema ## Fail if the committed schema is stale
	@git diff --exit-code -- $(SCHEMA) \
		|| { echo "$(SCHEMA) is stale — commit the regenerated file"; exit 1; }

tidy: ## Tidy go.mod
	$(GO) mod tidy

clean: ## Remove build output
	rm -rf dist
