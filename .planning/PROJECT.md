# updex

## What This Is

A Debian-compatible alternative to systemd's `updatectl` for managing systemd-sysexts. Provides a CLI tool for discovering, installing, updating, and removing system extensions from multiple registries, with auto-update timer support.

## Core Value

Users can reliably install and update systemd-sysexts from any registry without needing the unavailable `updatectl` package.

## Current State

**Shipped:** v1 (2026-01-26)

The tool is production-ready with:
- Complete CLI for extension management (install, update, remove, list, check)
- Feature-based grouping with enable/disable commands
- Auto-update via systemd timer (`daemon enable/disable/status`)
- Safe enable/disable with `--now` flag and merge state checks
- Comprehensive test suite (177+ tests, all run without root)
- Shell completions for bash, zsh, and fish

## Requirements

### Validated

- ✓ CLI framework with Cobra commands — v1
- ✓ Discover available extensions from registries — v1
- ✓ Install extensions from URL/registry — v1
- ✓ Update installed extensions to latest versions — v1
- ✓ List installed extensions and available versions — v1
- ✓ Check for available updates — v1
- ✓ Multiple registry support — v1
- ✓ GPG signature verification — v1
- ✓ Progress bar for downloads — v1
- ✓ JSON output mode — v1
- ✓ Feature-based extension grouping — v1
- ✓ Transfer config file parsing (.transfer INI format) — v1
- ✓ Version comparison and pattern matching — v1
- ✓ Enable feature with --now (immediate download) — v1
- ✓ Disable feature with --now (immediate removal) — v1
- ✓ Merge state safety checks — v1
- ✓ Auto-update systemd timer/service — v1
- ✓ daemon enable/disable/status commands — v1
- ✓ --reboot flag for update command — v1
- ✓ Unit test coverage for core operations — v1
- ✓ Integration tests for workflows — v1
- ✓ Tests run without root — v1
- ✓ Actionable error messages — v1
- ✓ Comprehensive help text — v1
- ✓ Shell completions (bash, zsh, fish) — v1

### Active

- [ ] Configurable timer schedule (daily, weekly, custom)
- [ ] --offline flag for list command
- [ ] Auto-update failure notifications

### Out of Scope

- Mobile app or GUI — CLI-only tool
- Windows/macOS support — systemd-sysext is Linux-specific
- Package repository hosting — this is a client tool only
- D-Bus daemon — CLI-only tool is sufficient
- Partition operations — focus on file-based transfers
- Auto-update by default — must be opt-in
- Rollback command — use version pinning instead

## Context

The project is part of the Frostyard ecosystem. The codebase follows a clean library + CLI architecture where `updex/` is the public API and `cmd/` contains thin CLI wrappers. Configuration is INI-based (.transfer and .feature files) following systemd conventions.

Current codebase: 10,377 lines of Go with 44.4% coverage on updex package.

## Constraints

- **Platform**: Linux only (systemd-sysext dependency)
- **Architecture**: Primary target is amd64, arm64 for testing
- **Go version**: 1.25+
- **Compatibility**: Must work with existing .transfer file format
- **CI verification**: All GitHub Actions "Tests" workflow jobs must pass before work is complete:
  - lint (golangci-lint and `make lint`)
  - security (govulncheck)
  - verify (go mod tidy, go vet, gofmt)
  - unit-test (go test with coverage)
  - race-test (go test -race)
  - build (cross-compile linux/amd64, linux/arm64)

## Key Decisions

| Decision                   | Rationale                               | Outcome   |
| -------------------------- | --------------------------------------- | --------- |
| Go for implementation      | Fast, single binary, good for CLI tools | ✓ Good    |
| Cobra for CLI framework    | Industry standard, good docs            | ✓ Good    |
| INI format for configs     | Matches systemd conventions             | ✓ Good    |
| Library + CLI architecture | Enables programmatic use                | ✓ Good    |
| Disable = remove files     | Simpler mental model for users          | ✓ Good    |
| Package-level SetRunner    | Simple test injection pattern           | ✓ Good    |
| --now combines unmerge+remove | Complete immediate effect            | ✓ Good    |
| Fixed daily timer schedule | Simpler initial implementation          | ✓ Good    |
| Service uses --no-refresh  | Stage only, no auto-activate            | ✓ Good    |

---

_Last updated: 2026-01-26 after v1 milestone_
