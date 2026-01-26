# Milestone v1: updex hardening

**Status:** SHIPPED 2026-01-26
**Phases:** 1-5
**Total Plans:** 11

## Overview

This milestone hardened updex from functional to reliable. We fixed dangerous UX issues (disable should remove files), established testing infrastructure, built systemd unit management for auto-update, exposed it via CLI, and polished the user experience. Research recommended fix-first: address remove/disable semantics before automating them.

## Phases

### Phase 1: Test Foundation

**Goal**: Developers can write and run tests without root privileges
**Depends on**: Nothing (first phase)
**Requirements**: TEST-01, TEST-02, TEST-04
**Plans**: 2 plans

Plans:
- [x] 01-01-PLAN.md — Create test infrastructure (SysextRunner interface, HTTP test helpers)
- [x] 01-02-PLAN.md — Add unit tests for core operations (list, check, update, install, remove)

**Details:**
- Created SysextRunner interface for mocking systemd-sysext commands
- HTTP test server helper for registry mocking
- Package-level SetRunner with cleanup function pattern
- 21 table-driven unit tests covering core operations
- 32.6% initial coverage for updex package

### Phase 2: Core UX Fixes

**Goal**: Users can safely enable/disable features with immediate effect
**Depends on**: Phase 1 (tests verify safety)
**Requirements**: UX-01, UX-02, UX-03
**Plans**: 2 plans

Plans:
- [x] 02-01-PLAN.md — Implement --now for enable (downloads) and fix disable semantics (file removal + merge state)
- [x] 02-02-PLAN.md — Add unit tests for enable/disable with --now, --force, --dry-run

**Details:**
- EnableFeatureOptions and DisableFeatureOptions structs
- --now on disable combines unmerge AND file removal
- Merge state check requires --force for active extensions
- 11 additional unit tests for feature operations
- Coverage increased to 44.4%

### Phase 3: Systemd Unit Infrastructure

**Goal**: Internal package can generate, install, and manage systemd timer/service files
**Depends on**: Phase 2 (safe operations to call from timer)
**Requirements**: AUTO-01
**Plans**: 3 plans

Plans:
- [x] 03-01-PLAN.md — Create unit types and generation functions with tests
- [x] 03-02-PLAN.md — Create SystemctlRunner interface and mock
- [x] 03-03-PLAN.md — Create Manager with Install/Remove operations and tests

**Details:**
- TimerConfig and ServiceConfig types for unit file configuration
- GenerateTimer and GenerateService functions
- SystemctlRunner interface mirroring SysextRunner pattern
- Manager with atomic Install/Remove/Exists operations
- 16 comprehensive test cases for Manager

### Phase 4: Auto-Update CLI

**Goal**: Users can manage auto-update timer via CLI commands
**Depends on**: Phase 3 (internal package exists)
**Requirements**: AUTO-02, AUTO-03, AUTO-04, UX-04
**Plans**: 1 plan

Plans:
- [x] 04-01-PLAN.md — Create daemon command (enable/disable/status) and add --reboot to update

**Details:**
- daemon command group with enable/disable/status subcommands
- Service uses --no-refresh to stage files only (AUTO-04)
- --reboot flag for update command
- Fixed daily schedule (configurable deferred to v2)

### Phase 5: Integration & Polish

**Goal**: End-to-end workflows are validated and user experience is polished
**Depends on**: Phase 4 (all features complete)
**Requirements**: TEST-03, POLISH-01, POLISH-02, POLISH-03
**Plans**: 3 plans

Plans:
- [x] 05-01-PLAN.md — Create integration tests for end-to-end workflows
- [x] 05-02-PLAN.md — Polish error messages and help text across all commands
- [x] 05-03-PLAN.md — Verify shell completions for bash, zsh, and fish

**Details:**
- IntegrationTestEnv helper for complete test environment setup
- 3 workflow integration tests (update, remove, multi-version)
- All 11 commands polished with actionable error messages
- Comprehensive help text with REQUIREMENTS/WORKFLOW sections
- Shell completions verified for bash, zsh, and fish

---

## Milestone Summary

**Key Decisions:**
- Fix-first approach: address remove/disable semantics before auto-update
- Package-level SetRunner with cleanup function for test injection
- --now combines unmerge AND file removal (breaking from old behavior)
- Merge state check requires --force for active extensions
- Service uses --no-refresh to stage files only
- Fixed daily schedule (configurable deferred to v2)

**Issues Resolved:**
- Dangerous disable semantics (files now removed with --now)
- Missing test infrastructure (177+ tests now run without root)
- No auto-update capability (systemd timer now available)
- Poor error messages (all commands now have actionable suggestions)

**Issues Deferred:**
- Configurable timer schedule (v2)
- --offline flag for list command (v2)
- Auto-update failure notifications (v2)

**Technical Debt Incurred:**
None. All phases completed without deferred items.

---

*For current project status, see .planning/ROADMAP.md*
*Archived: 2026-01-26*
