# updex

## What This Is

A Debian-compatible alternative to systemd's `updatectl` for managing systemd-sysexts. Provides a CLI tool for discovering, installing, updating, and removing system extensions from multiple registries, targeting both sysadmins and desktop enthusiasts.

## Core Value

Users can reliably install and update systemd-sysexts from any registry without needing the unavailable `updatectl` package.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. Inferred from existing codebase. -->

- ✓ CLI framework with Cobra commands — existing
- ✓ Discover available extensions from registries — existing
- ✓ Install extensions from URL/registry — existing
- ✓ Update installed extensions to latest versions — existing
- ✓ List installed extensions and available versions — existing
- ✓ Check for available updates — existing
- ✓ Multiple registry support — existing
- ✓ GPG signature verification — existing
- ✓ Progress bar for downloads — existing
- ✓ JSON output mode — existing
- ✓ Feature-based extension grouping — existing
- ✓ Transfer config file parsing (.transfer INI format) — existing
- ✓ Version comparison and pattern matching — existing

### Active

<!-- Current scope. Building toward these. -->

- [ ] Auto-update mechanism with systemd timer/service
- [ ] Optional command to install auto-update timer/service files
- [ ] Disable feature removes extension files (not just stops updates)
- [ ] Improved unit test coverage
- [ ] Integration tests with real sysexts
- [ ] Better error messages and help text

### Out of Scope

- Mobile app or GUI — CLI-only tool
- Windows/macOS support — systemd-sysext is Linux-specific
- Package repository hosting — this is a client tool only

## Context

The project is part of the Frostyard ecosystem. The codebase follows a clean library + CLI architecture where `updex/` is the public API and `cmd/` contains thin CLI wrappers. Configuration is INI-based (.transfer and .feature files) following systemd conventions.

Existing test coverage is limited. The tool is functional for core operations but needs polish before broader release.

## Constraints

- **Platform**: Linux only (systemd-sysext dependency)
- **Architecture**: Primary target is amd64, arm64 for testing
- **Go version**: 1.25+
- **Compatibility**: Must work with existing .transfer file format
- **CI verification**: All GitHub Actions "Tests" workflow jobs must pass before work is complete:
  - lint (golangci-lint)
  - security (govulncheck)
  - verify (go mod tidy, go vet, gofmt)
  - unit-test (go test with coverage)
  - race-test (go test -race)
  - build (cross-compile linux/amd64, linux/arm64)

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go for implementation | Fast, single binary, good for CLI tools | ✓ Good |
| Cobra for CLI framework | Industry standard, good docs | ✓ Good |
| INI format for configs | Matches systemd conventions | ✓ Good |
| Library + CLI architecture | Enables programmatic use | ✓ Good |
| Disable = remove files | Simpler mental model for users | — Pending |

---
*Last updated: 2026-01-26 after adding CI constraint*
