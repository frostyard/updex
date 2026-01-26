# Roadmap: updex

## Overview

This milestone hardens updex from functional to reliable. We fix dangerous UX issues (disable should remove files), establish testing infrastructure, build systemd unit management for auto-update, expose it via CLI, and polish the user experience. Research recommends fix-first: address remove/disable semantics before automating them.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Test Foundation** - Establish testing infrastructure and patterns
- [ ] **Phase 2: Core UX Fixes** - Fix dangerous remove/disable semantics
- [ ] **Phase 3: Systemd Unit Infrastructure** - Build internal package for timer/service management
- [ ] **Phase 4: Auto-Update CLI** - Expose auto-update via daemon commands
- [ ] **Phase 5: Integration & Polish** - End-to-end validation and UX polish

## Phase Details

### Phase 1: Test Foundation
**Goal**: Developers can write and run tests without root privileges
**Depends on**: Nothing (first phase)
**Requirements**: TEST-01, TEST-02, TEST-04
**Success Criteria** (what must be TRUE):
  1. Unit tests exist for core operations (list, check, update, install, remove)
  2. Unit tests exist for config parsing (transfer files, feature files)
  3. Tests run without root using mocked filesystem/systemd
  4. Test helper utilities are available for HTTP server mocking
**Plans**: TBD

Plans:
- [ ] 01-01: TBD
- [ ] 01-02: TBD

### Phase 2: Core UX Fixes
**Goal**: Users can safely enable/disable features with immediate effect
**Depends on**: Phase 1 (tests verify safety)
**Requirements**: UX-01, UX-02, UX-03
**Success Criteria** (what must be TRUE):
  1. User can `features enable --now` to immediately download extensions
  2. User can `features disable --now` to immediately remove extension files
  3. Disabling a feature removes extension files (not just config changes)
  4. Remove operations check merge state before deleting files
**Plans**: TBD

Plans:
- [ ] 02-01: TBD
- [ ] 02-02: TBD

### Phase 3: Systemd Unit Infrastructure
**Goal**: Internal package can generate, install, and manage systemd timer/service files
**Depends on**: Phase 2 (safe operations to call from timer)
**Requirements**: AUTO-01
**Success Criteria** (what must be TRUE):
  1. Timer and service unit files can be generated with correct systemd syntax
  2. Unit files can be installed to /etc/systemd/system (or configurable path)
  3. Unit files can be removed cleanly
  4. Package is fully testable with temp directories (no root required)
**Plans**: TBD

Plans:
- [ ] 03-01: TBD

### Phase 4: Auto-Update CLI
**Goal**: Users can manage auto-update timer via CLI commands
**Depends on**: Phase 3 (internal package exists)
**Requirements**: AUTO-02, AUTO-03, AUTO-04, UX-04
**Success Criteria** (what must be TRUE):
  1. User can run `updex daemon enable` to install timer/service
  2. User can run `updex daemon disable` to remove timer/service
  3. User can run `updex daemon status` to check timer state
  4. Auto-update only stages files, does not auto-activate merged extensions
  5. User can pass `--reboot` to update command to reboot after update
**Plans**: TBD

Plans:
- [ ] 04-01: TBD
- [ ] 04-02: TBD

### Phase 5: Integration & Polish
**Goal**: End-to-end workflows are validated and user experience is polished
**Depends on**: Phase 4 (all features complete)
**Requirements**: TEST-03, POLISH-01, POLISH-02, POLISH-03
**Success Criteria** (what must be TRUE):
  1. Integration tests validate complete workflows (install → update → remove)
  2. Error messages are clear and actionable (no cryptic stack traces)
  3. Help text is comprehensive and follows conventions
  4. Shell completions work for bash, zsh, and fish
**Plans**: TBD

Plans:
- [ ] 05-01: TBD
- [ ] 05-02: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Test Foundation | 0/TBD | Not started | - |
| 2. Core UX Fixes | 0/TBD | Not started | - |
| 3. Systemd Unit Infrastructure | 0/TBD | Not started | - |
| 4. Auto-Update CLI | 0/TBD | Not started | - |
| 5. Integration & Polish | 0/TBD | Not started | - |
