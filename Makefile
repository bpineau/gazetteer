# gazetteer — convenience targets for building, formatting, linting and
# refreshing the library.
#
# Default goal prints the help (no surprise side-effects when you type
# `make` alone).

BIN_DIR      := bin
CLI_BIN      := $(BIN_DIR)/gazetteer
CLI_PKG      := ./cmd/gazetteer

GO           ?= go
GOFMT        ?= gofmt
GOIMPORTS    ?= goimports
GOLANGCI     ?= golangci-lint

GO_PACKAGES  := ./...
GO_TIMEOUT   ?= 180s

.DEFAULT_GOAL := help

# ----- Help ----------------------------------------------------------

.PHONY: help
help: ## Print this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ----- Build ---------------------------------------------------------

.PHONY: build
build: $(CLI_BIN) ## Build the gazetteer CLI into bin/gazetteer.

$(CLI_BIN): $(shell find . -type f -name '*.go' -not -path './$(BIN_DIR)/*')
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -o $(CLI_BIN) $(CLI_PKG)

.PHONY: install
install: ## Install the CLI into $$GOBIN / $$GOPATH/bin via `go install`.
	$(GO) install $(CLI_PKG)

# ----- Format / lint / vet -------------------------------------------

.PHONY: fmt
fmt: ## gofmt + goimports in-place across the whole tree.
	$(GOFMT) -w .
	$(GOIMPORTS) -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file would change under gofmt or goimports.
	@out="$$( $(GOFMT) -l . )"; \
	if [ -n "$$out" ]; then \
	  echo "gofmt diff:"; echo "$$out"; exit 1; \
	fi
	@out="$$( $(GOIMPORTS) -l . )"; \
	if [ -n "$$out" ]; then \
	  echo "goimports diff:"; echo "$$out"; exit 1; \
	fi

.PHONY: vet
vet: ## go vet across the module.
	$(GO) vet $(GO_PACKAGES)

.PHONY: lint
lint: ## golangci-lint run (uses .golangci.yml).
	$(GOLANGCI) run

.PHONY: tools
tools: ## Install developer tooling (goimports, golangci-lint).
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# ----- Tests ---------------------------------------------------------

.PHONY: test
test: ## Run the test suite with a sane timeout.
	$(GO) test -timeout $(GO_TIMEOUT) $(GO_PACKAGES)

.PHONY: test-race
test-race: ## Run the test suite under the race detector.
	$(GO) test -timeout $(GO_TIMEOUT) -race $(GO_PACKAGES)

# ----- Refresh embedded datasets ------------------------------------

.PHONY: refresh
refresh: build ## Refresh the embedded CSV / JSON datasets shipped by every source.
	$(CLI_BIN) refresh all

# ----- CI grade gate -------------------------------------------------

.PHONY: check
check: fmt-check vet lint test ## All-in CI-grade gate: format + vet + lint + test.

.PHONY: tidy-check
tidy-check: ## Fail if `go mod tidy` would change go.mod / go.sum (no mutation).
	$(GO) mod tidy -diff

.PHONY: precommit
precommit: check tidy-check ## The pre-commit gate: check + tidy-check (fast; ~seconds cached).

# ----- Git hooks -----------------------------------------------------

.PHONY: hooks
hooks: ## Install the repo git hooks (pre-commit runs `make precommit`).
	git config core.hooksPath .githooks
	@echo "installed: .githooks (pre-commit → make precommit). Bypass once with: git commit --no-verify"

# ----- Housekeeping --------------------------------------------------

.PHONY: clean
clean: ## Remove build artefacts.
	rm -rf $(BIN_DIR)

.PHONY: tidy
tidy: ## go mod tidy.
	$(GO) mod tidy
