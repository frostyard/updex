.PHONY: all build clean fmt lint lint-version-check test test-cover coverage-check test-coverage-check docs-check tidy check ci install help

# Build variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_TIME) -X main.builtBy=make"

# Go commands
GO := go
GOFMT := gofmt

# Pinned golangci-lint release. This is the single source of truth: the CI
# Lint job reads it from this file (see .github/workflows/test.yml) and
# `make ci` refuses to run with any other version, so the lint signal is
# reproducible and bumped deliberately. Bump here only. Compared against
# `golangci-lint version --short`, which prints the bare "MAJOR.MINOR.PATCH".
GOLANGCI_LINT_VERSION := 2.12.2
GOFILES := $(shell find . -type f -name '*.go' -not -path "./vendor/*")

all: fmt build

## build: Build the updex binary
build:
	$(GO) build $(LDFLAGS) -o build/updex ./cmd/updex-cli

## install: Install updex binary to GOPATH/bin
install:
	$(GO) install $(LDFLAGS) ./cmd/updex-cli

## clean: Remove build artifacts
clean:
	rm -rf build/
	$(GO) clean

## fmt: Format Go source files
fmt:
	$(GOFMT) -w $(GOFILES)

lint: ## Run linter
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		installed="$$(golangci-lint version --short 2>/dev/null)"; \
		if [ "$$installed" != "$(GOLANGCI_LINT_VERSION)" ]; then \
			echo "warning: golangci-lint $$installed installed, CI pins $(GOLANGCI_LINT_VERSION); results may differ (make ci enforces the pin)"; \
		fi; \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

## lint-version-check: Fail unless the installed golangci-lint matches GOLANGCI_LINT_VERSION
lint-version-check:
	@installed="$$(golangci-lint version --short 2>/dev/null)" || { \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required for make ci (not installed)"; exit 1; }; \
	if [ "$$installed" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "expected golangci-lint $(GOLANGCI_LINT_VERSION), found $$installed"; \
		echo "install with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi

## test: Run tests
test:
	$(GO) test -v ./...

## test-cover: Run tests with coverage
test-cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## coverage-check: Enforce the 80.0% total statement-coverage floor on coverage.out
coverage-check:
	./scripts/check-coverage.sh coverage.out

## test-coverage-check: Exercise scripts/check-coverage.sh against fixture profiles
test-coverage-check:
	./scripts/test-coverage-check.sh

## tidy: Tidy go modules
tidy:
	$(GO) mod tidy

## check: Run fmt, lint, and test
check: fmt lint test

## ci: Run the credential-free CI gate
ci:
	@echo "==> verify: go.mod is tidy"
	$(GO) mod tidy
	git diff --exit-code -- go.mod go.sum
	@echo "==> verify: go vet"
	$(GO) vet ./...
	@echo "==> verify: gofmt"
	test -z "$$($(GOFMT) -l $$(git ls-files '*.go'))"
	@echo "==> lint (golangci-lint $(GOLANGCI_LINT_VERSION))"
	$(MAKE) lint-version-check
	golangci-lint run
	@echo "==> unit tests"
	$(GO) test -v $$($(GO) list ./... | grep -v '/tests/e2e$$') -coverprofile=coverage.out -covermode=atomic
	@echo "==> coverage floor"
	$(MAKE) test-coverage-check
	$(MAKE) coverage-check
	@echo "==> race detector"
	$(GO) test -race -short -v $$($(GO) list ./... | grep -v '/tests/e2e$$')
	@echo "==> cross-architecture build"
	GOOS=linux GOARCH=amd64 $(MAKE) build
	GOOS=linux GOARCH=arm64 $(MAKE) build
	@echo "==> docs integrity"
	$(MAKE) docs-check
	@echo "==> CI gate passed"

## docs-check: Run the docs-integrity gate (index, links, aliases) — the CI "Docs integrity" job
docs-check:
	@command -v node >/dev/null 2>&1 || { \
	  echo "docs-check: 'node' is not on PATH; the docs-integrity gate needs Node >= 20 (see AGENTS.md Prerequisites)" >&2; \
	  exit 1; \
	}; \
	major="$$(node -p 'process.versions.node.split(".")[0]')"; \
	if [ "$$major" -lt 20 ]; then \
	  echo "docs-check: node $$major.x is too old; the docs-integrity gate needs Node >= 20 (see AGENTS.md Prerequisites)" >&2; \
	  exit 1; \
	fi
	node scripts/check-docs.mjs

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

bump: ## generate a new version with svu
	@$(MAKE) build
	@$(MAKE) test
	@$(MAKE) fmt
	$(MAKE) lint
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Working directory is not clean. Please commit or stash changes before bumping version."; \
		exit 1; \
	fi
	@echo "Creating new tag..."
	@version=$$(svu next); \
		git tag -a $$version -m "Version $$version"; \
		echo "Tagged version $$version"; \
		echo "Pushing tag $$version to origin..."; \
		git push origin $$version
