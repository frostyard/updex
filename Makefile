.PHONY: all build clean fmt lint lint-version-check test test-cover coverage-check test-coverage-check test-docs-check tidy check verify-static verify ci install help

# Build variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_TIME) -X main.builtBy=make"

# Go commands
GO := go
GOFMT := gofmt

# Pinned golangci-lint release, read from mise.toml — the single source of
# every tool pin (core ADR-0043): `mise install` provisions it locally, in CI
# (jdx/mise-action), and on Snowcat workers, verified against mise.lock.
# Bump it there in a dedicated commit; never edit this line. Compared against
# `golangci-lint version --short`, which prints the bare "MAJOR.MINOR.PATCH".
GOLANGCI_LINT_VERSION := $(strip $(shell sed -n 's/^golangci-lint = "\(.*\)"/\1/p' mise.toml))
# The Go release this module is built with, from go.mod's toolchain line —
# the only Go pin (mise reads the same line). golangci-lint must be built
# with a Go at least this new, or its embedded gofmt and typechecker disagree
# with the toolchain.
GO_TOOLCHAIN := $(strip $(shell sed -n 's/^toolchain go\(.*\)/\1/p' go.mod))
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

## lint: Run linter (requires golangci-lint; fails if the mise.toml pin is missing or the installed release differs)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		$(MAKE) --no-print-directory lint-version-check && \
		golangci-lint run; \
	else \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required for make lint (not installed)"; \
		echo "install with: mise install"; \
		exit 1; \
	fi

## lint-version-check: Fail unless the installed golangci-lint is the mise.toml pin and was built with a Go no older than go.mod's toolchain
lint-version-check:
	@test -n "$(GOLANGCI_LINT_VERSION)" || { echo "mise.toml pins no golangci-lint"; exit 1; }
	@installed="$$(golangci-lint version --short 2>/dev/null)" || { \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required (not installed; run: mise install)"; exit 1; }; \
	if [ "$$installed" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "expected golangci-lint $(GOLANGCI_LINT_VERSION), found $$installed (run: mise install)"; \
		exit 1; \
	fi; \
	built="$$(golangci-lint version 2>/dev/null | sed -n 's/.*built with go\([0-9.]*\).*/\1/p')"; \
	if [ -n "$$built" ] && [ "$$(printf '%s\n%s\n' "$(GO_TOOLCHAIN)" "$$built" | sort -V | head -1)" != "$(GO_TOOLCHAIN)" ]; then \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) was built with go$$built, older than go.mod's toolchain go$(GO_TOOLCHAIN): bump golangci-lint first (core ADR-0043)"; \
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

## test-docs-check: Exercise scripts/check-docs.mjs against fixture repositories
test-docs-check:
	node scripts/test-check-docs.mjs

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
