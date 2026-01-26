.PHONY: all build clean fmt lint test install help

# Build variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go commands
GO := go
GOFMT := gofmt
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
	rm -f updex
	$(GO) clean

## fmt: Format Go source files
fmt:
	$(GOFMT) -w $(GOFILES)

lint: ## Run linter
	@echo "Running linter..."
	@golangci-lint run || echo "golangci-lint not installed, skipping"

## test: Run tests
test:
	$(GO) test -v ./...

## test-cover: Run tests with coverage
test-cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## tidy: Tidy go modules
tidy:
	$(GO) mod tidy

## check: Run fmt, lint, and test
check: fmt lint test

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
