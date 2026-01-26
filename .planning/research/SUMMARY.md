# Project Research Summary

**Project:** updex
**Domain:** systemd-sysext management CLI (Debian-compatible alternative to updatectl)
**Researched:** 2026-01-26
**Confidence:** HIGH

## Executive Summary

updex is a mature systemd-sysext management CLI that already ships core functionality (install, update, list, remove, features). The current milestone focuses on hardening: adding auto-update via systemd timer/service, fixing UX issues (disable should remove files), and establishing testing infrastructure. Research confirms the project uses an excellent foundation stack (Cobra, Fang, go-version) and only needs targeted additions for new features.

The recommended approach is **fix-first, then extend**: address the "disable removes files" UX issue and establish testing patterns before implementing auto-update. Auto-update introduces safety-critical concerns around merge state management — updating sysext files while extensions are merged can destabilize systems. By fixing core remove/disable semantics first and adding tests, the auto-update implementation can build on verified safe operations.

Key risks center on sysext merge state management. Deleting or updating files while extensions are merged into the filesystem is dangerous. The mitigation strategy is explicit: auto-update downloads and stages new versions but does NOT activate them until reboot or explicit refresh. The existing codebase doesn't consistently check merge state before operations — this is the primary technical debt to address.

## Key Findings

### Recommended Stack

The existing stack is solid and should be retained. Two targeted additions are recommended:

**Core technologies (keep):**
- `github.com/spf13/cobra` v1.10.2: CLI framework — industry standard
- `github.com/charmbracelet/fang` v0.4.4: Config unmarshaling — modern, well-designed
- `github.com/hashicorp/go-version` v1.8.0: Version comparison — de facto standard

**Add for this milestone:**
- `github.com/stretchr/testify` v1.11.1: Test assertions — 25.7k stars, de facto Go testing standard. Use `require` for error checks, `assert` for comparisons.
- `github.com/coreos/go-systemd/v22/unit` v22.6.0: Systemd unit file serialization — CoreOS-maintained, provides safe escaping for unit files. Only use the `unit` subpackage (no cgo).

**Avoid:**
- `go-systemd/dbus` — heavy, not needed for file generation
- `testify/suite` — doesn't support parallel tests
- `text/template` for units — error-prone escaping

### Expected Features

**Already shipped (table stakes):**
- list, check, update, vacuum, pending — core update lifecycle
- features list/enable/disable — feature management
- discover, install, remove — unique differentiators vs updatectl
- JSON output, GPG verification, progress bars

**Add this milestone (P1):**
- `--now` for enable/disable — immediately apply changes
- Auto-update timer/service — `updex daemon enable`
- Disable removes files — complete the "disable = uninstall" mental model

**Defer to future (P2/P3):**
- `--offline` flag — local-only listing
- `--reboot` flag — reboot after update
- Shell completions

### Architecture Approach

The architecture follows a clean Library + CLI separation that should be extended for auto-update. New functionality goes in `internal/systemd/` for unit file management, with `updex.Client` methods (`EnableAutoUpdate`, `DisableAutoUpdate`, `AutoUpdateStatus`) exposing the operations. Embedded templates via `embed.FS` bundle unit files into the binary for single-file distribution.

**Major components:**
1. `cmd/commands/daemon.go` — CLI wrapper for daemon enable/disable/status
2. `updex/daemon.go` — Public API methods with typed options/results
3. `internal/systemd/manager.go` — Unit file rendering and installation (configurable paths for testing)

**Key pattern:** Filesystem abstraction allows testing without root — `Manager.SystemdDir` is `/etc/systemd/system` by default but `t.TempDir()` in tests.

### Critical Pitfalls

1. **Removing active extensions without unmerge** — Check `/run/extensions` before any file deletion. Require `--now` to unmerge first, or error if extension is merged.

2. **Auto-update during active merge state** — Never directly modify merged extensions. Auto-update stages to download new versions; activation requires reboot or explicit refresh.

3. **Feature disable without file cleanup** — Current `features disable` only writes config drop-in. Must remove files by default or make `--remove` the default.

4. **Symlink race conditions** — Update symlinks only when extensions are unmerged. Pattern: unmerge → update symlink → merge.

5. **Breaking running system** — Keep InstancesMax >= 2 for rollback. Don't auto-activate updates; let user/admin choose when to apply.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: Core UX Fixes and Test Foundation
**Rationale:** Establishes safety guarantees and testing patterns needed by all subsequent work. Fixing disable/remove semantics before auto-update prevents automating dangerous operations.
**Delivers:** 
- `features disable` removes files (UX issue fix)
- Merge state checks before remove operations
- testify added with test helpers
- HTTP test server for download tests
**Addresses:** Feature disable cleanup, remove safety
**Avoids:** Pitfall #1 (removing active extensions), Pitfall #4 (disable without cleanup)

### Phase 2: Systemd Unit Management
**Rationale:** Internal package for auto-update before exposing via CLI. Testable in isolation with temp directories.
**Delivers:**
- `internal/systemd/` package
- Unit file templates (service + timer)
- Install/remove/status operations
- Comprehensive unit tests
**Uses:** go-systemd/v22/unit for template rendering
**Implements:** Timer/Service architecture component

### Phase 3: Auto-Update CLI Integration
**Rationale:** Public API and CLI commands after internal is solid. Thin wrappers over tested infrastructure.
**Delivers:**
- `updex daemon enable [--schedule daily|weekly|custom]`
- `updex daemon disable`
- `updex daemon status`
- JSON output for all daemon commands
**Addresses:** Auto-update timer/service feature (P1)
**Avoids:** Pitfall #2 (auto-update during merge), Pitfall #5 (breaking running system)

### Phase 4: Integration Testing and Polish
**Rationale:** End-to-end validation after components complete. Manual testing documentation for root-required paths.
**Delivers:**
- Root-required integration tests (skipped in CI)
- Updated Makefile with test targets
- Documentation for auto-update behavior
- `--dry-run` for destructive operations

### Phase Ordering Rationale

- **Safety first:** Phase 1 fixes dangerous remove/disable semantics. Auto-update (Phase 3) can then safely "update" which calls these operations.
- **Layered testing:** Each phase adds testable units. Phase 1 establishes patterns; Phase 2 tests internal package; Phase 4 does integration.
- **Dependencies:** Phase 2 (`internal/systemd/`) must exist before Phase 3 (CLI exposes it). Phase 1 fixes are independent and should come first.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2 (Systemd Unit Management):** Timer configuration options (OnCalendar syntax, RandomizedDelaySec) may need systemd.timer(5) reference during implementation.

Phases with standard patterns (skip research-phase):
- **Phase 1:** Standard Go testing patterns, testify is well-documented
- **Phase 3:** Follows existing CLI patterns in codebase
- **Phase 4:** Standard integration testing approaches

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Official pkg.go.dev docs verified, versions confirmed |
| Features | HIGH | Official systemd XML man pages parsed |
| Architecture | HIGH | Based on existing codebase patterns, established Go conventions |
| Pitfalls | MEDIUM | Codebase analysis + domain knowledge; systemd-sysext edge cases should be verified against actual behavior |

**Overall confidence:** HIGH

### Gaps to Address

- **Merge state detection accuracy:** Need to verify `/run/extensions` check reliably identifies active merges across systemd versions.
- **Timer catchup behavior:** Confirm `Persistent=true` behaves as expected on laptops (missed timer runs on wake).
- **User vs system install:** Research whether to support `--user` for systemd user units in addition to system units.

## Sources

### Primary (HIGH confidence)
- https://pkg.go.dev/github.com/stretchr/testify@v1.11.1 — Testing library documentation
- https://pkg.go.dev/github.com/coreos/go-systemd/v22/unit — Unit file serialization API
- systemd-sysupdate.xml (GitHub raw) — Feature parity analysis
- updatectl.xml (GitHub raw) — Feature parity analysis

### Secondary (MEDIUM confidence)
- Existing codebase analysis — Architecture patterns, test patterns
- Flatcar sysext-bakery — Sysext management patterns

### Tertiary (LOW confidence)
- systemd.timer(5) concepts — Timer configuration (not directly fetched)
- Sysext merge semantics — Overlay behavior (from training data, verify against current systemd)

---
*Research completed: 2026-01-26*
*Ready for roadmap: yes*
