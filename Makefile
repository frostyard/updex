.PHONY: all build clean fmt lint lint-version-check test test-cover coverage-check test-coverage-check tidy check verify-static verify ci install help

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
GOLANGCI_LINT_VERSION := 2.13.1
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
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required for make lint (not installed)"; \
		echo "install with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
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

## coverage-check: Enforce the absolute floor and committed coverage ratchet on coverage.out
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

## verify-static: The non-mutating static checks shared by verify and ci (tidy diff, vet, gofmt -l, exact-pin lint)
verify-static:
	@echo "==> verify: go.mod is tidy"
	$(GO) mod tidy -diff
	@echo "==> verify: go vet"
	$(GO) vet ./...
	@echo "==> verify: gofmt"
	test -z "$$($(GOFMT) -l $$(git ls-files '*.go'))"
	@echo "==> lint (golangci-lint $(GOLANGCI_LINT_VERSION))"
	$(MAKE) lint-version-check
	golangci-lint run

## verify: Credential-free, non-mutating gate (what a read-only reviewer runs): verify-static plus the non-E2E tests
verify: verify-static
	@echo "==> unit tests"
	$(GO) test $$($(GO) list ./... | grep -v '/tests/e2e$$')

## ci: Run the credential-free CI gate (verify-static, then coverage, race, and cross-build)
ci: verify-static
	@echo "==> unit tests with coverage"
	$(GO) test -v $$($(GO) list ./... | grep -v '/tests/e2e$$') -coverprofile=coverage.out -covermode=atomic
	@echo "==> coverage floor"
	$(MAKE) test-coverage-check
	$(MAKE) coverage-check
	@echo "==> end-to-end tests"
	$(GO) test -v ./tests/e2e/...
	@echo "==> race detector"
	$(GO) test -race -short -v $$($(GO) list ./... | grep -v '/tests/e2e$$')
	@echo "==> cross-architecture build"
	GOOS=linux GOARCH=amd64 $(MAKE) build
	GOOS=linux GOARCH=arm64 $(MAKE) build
	@echo "==> CI gate passed"

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
