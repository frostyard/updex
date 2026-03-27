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
- `cmd/updex/` — Cobra command handlers calling SDK methods (flags, output formatting, progress bars)
- `config/` — Parses `.transfer` and `.feature` INI files from systemd-style search paths
- `download/` — HTTP downloads with SHA256 verification and decompression (xz, gz, zstd)
- `manifest/` — Fetches/parses SHA256SUMS manifests with optional GPG verification
- `version/` — Pattern matching (`@v` placeholder) and semantic version comparison
- `sysext/` — systemd-sysext integration with mockable `Runner` interface
- `systemd/` — Generates/installs systemd timer+service units, mockable `Runner` interface

Entry point: `cmd/updex-cli/main.go` → `cmd/updex/root.go`

## Code Patterns

- Error messages: lowercase, no trailing punctuation, wrap with `fmt.Errorf("context: %w", err)`
- SDK functions accept a `context.Context` and an options struct, return result structs + error
- CLI output: `common.OutputJSON()` for `--json` flag, text tables otherwise
- Tests use `t.TempDir()` for filesystem operations and mock runners for systemd commands
- Configuration uses INI format with systemd-style priority paths: `/etc/sysupdate.d/`, `/run/sysupdate.d/`, `/usr/local/lib/sysupdate.d/`, `/usr/lib/sysupdate.d/`

## Go Version

Go 1.26. Use modern idioms: `any`, `slices`/`maps`/`cmp` packages, `t.Context()`, `slices.SortFunc`, `strings.SplitSeq`, `omitzero` for slice/map/struct JSON tags, `wg.Go()`.
## Documentation

**update documentation** After any change to source code, update relevant documentation in CLAUDE.md, README.md and the yeti/ folder. A task is not complete without reviewing and updating relevant documentation.

**yeti/ directory** The `yeti/` directory contains documentation written for AI consumption and context enhancement, not primarily for humans. Jobs like `doc-maintainer` and `issue-worker` instruct the AI to read `yeti/OVERVIEW.md` and related files for codebase context before performing tasks. Write content in this directory to be maximally useful to an AI agent understanding the codebase — detailed architecture, patterns, and decision rationale rather than user-facing guides.
