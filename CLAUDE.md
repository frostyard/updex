# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make fmt          # Format code (run after every change)
make build        # Build binary to build/updex
make test         # Run all tests
make lint         # Run golangci-lint
make check        # fmt + lint + test
make test-cover   # Tests with HTML coverage report
make tidy         # go mod tidy
```

Run a single test: `go test -v -run TestName ./updex/`

## Architecture

updex is a Go SDK and CLI for managing systemd-sysext images. It replicates `systemd-sysupdate` functionality for `url-file` transfers.

**SDK-first design**: All logic lives in the `updex/` package as a public Go API. CLI commands in `cmd/` are thin Cobra wrappers that parse flags, call SDK functions, and format output. SDK code must never import CLI packages.

Key packages:
- `updex/` — Public SDK: `Client` struct with `Features()`, `EnableFeature()`, `DisableFeature()`, `UpdateFeatures()`, `CheckFeatures()`
- `cmd/commands/` — Cobra command handlers calling SDK methods
- `cmd/common/` — Shared CLI utilities (flags, JSON output, progress reporting)
- `internal/config/` — Parses `.transfer` and `.feature` INI files from systemd-style search paths
- `internal/download/` — HTTP downloads with SHA256 verification and decompression (xz, gz, zstd)
- `internal/manifest/` — Fetches/parses SHA256SUMS manifests with optional GPG verification
- `internal/version/` — Pattern matching (`@v` placeholder) and semantic version comparison
- `internal/sysext/` — systemd-sysext integration with mockable `Runner` interface
- `internal/systemd/` — Generates/installs systemd timer+service units, mockable `Runner` interface

Entry point: `cmd/updex-cli/main.go` → `cmd/updex/root.go`

## Code Patterns

- Error messages: lowercase, no trailing punctuation, wrap with `fmt.Errorf("context: %w", err)`
- SDK functions accept a `context.Context` and an options struct, return result structs + error
- CLI output: `common.OutputJSON()` for `--json` flag, text tables otherwise
- Tests use `t.TempDir()` for filesystem operations and mock runners for systemd commands
- Configuration uses INI format with systemd-style priority paths: `/etc/sysupdate.d/`, `/run/sysupdate.d/`, `/usr/local/lib/sysupdate.d/`, `/usr/lib/sysupdate.d/`

## Go Version

Go 1.25. Use modern idioms: `any`, `slices`/`maps`/`cmp` packages, `t.Context()`, `slices.SortFunc`, `strings.SplitSeq`, `omitzero` for slice/map/struct JSON tags, `wg.Go()`.
