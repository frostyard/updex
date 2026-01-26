# Requirements: updex

**Defined:** 2026-01-26
**Core Value:** Users can reliably install and update systemd-sysexts from any registry without needing the unavailable updatectl package.

## v1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Core UX

- [x] **UX-01**: User can enable a feature with `--now` flag to immediately download extensions
- [x] **UX-02**: User can disable a feature with `--now` flag to immediately remove extension files
- [x] **UX-03**: User disabling a feature sees extension files removed (not just update config changed)
- [x] **UX-04**: User can pass `--reboot` to update command to reboot after update completes

### Auto-Update

- [x] **AUTO-01**: User can generate systemd timer and service files for scheduled updates
- [x] **AUTO-02**: User can install generated timer/service to system with `install-timer` command
- [x] **AUTO-03**: User can check auto-update timer status with status command
- [x] **AUTO-04**: Auto-update only stages files, does not auto-activate merged extensions

### Testing

- [x] **TEST-01**: Core operations have unit test coverage (list, check, update, install, remove)
- [x] **TEST-02**: Config parsing has unit test coverage (transfer, feature files)
- [x] **TEST-03**: Integration tests validate end-to-end workflows
- [x] **TEST-04**: Tests can run without root (mock filesystem/systemd where needed)

### Polish

- [x] **POLISH-01**: Error messages are clear and actionable
- [x] **POLISH-02**: Help text is comprehensive and follows conventions
- [x] **POLISH-03**: Shell completions work for bash, zsh, and fish

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Convenience Flags

- **CONV-01**: User can pass `--offline` to list command for local-only listing
- **CONV-02**: User can pass `--dry-run` to destructive commands to preview changes

### Advanced Features

- **ADV-01**: User receives notification when auto-update fails
- **ADV-02**: User can configure timer schedule (daily, weekly, custom)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| D-Bus daemon | Massive complexity, CLI-only tool is sufficient |
| Partition operations | Dangerous, complex, focus on file-based transfers |
| Auto-update by default | Security concern, must be opt-in |
| Rollback command | Complex state management, use version pinning instead |
| Disk image mode | Out of scope for Debian-focused tool |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| TEST-01 | Phase 1: Test Foundation | Complete |
| TEST-02 | Phase 1: Test Foundation | Complete |
| TEST-04 | Phase 1: Test Foundation | Complete |
| UX-01 | Phase 2: Core UX Fixes | Complete |
| UX-02 | Phase 2: Core UX Fixes | Complete |
| UX-03 | Phase 2: Core UX Fixes | Complete |
| AUTO-01 | Phase 3: Systemd Unit Infrastructure | Complete |
| AUTO-02 | Phase 4: Auto-Update CLI | Complete |
| AUTO-03 | Phase 4: Auto-Update CLI | Complete |
| AUTO-04 | Phase 4: Auto-Update CLI | Complete |
| UX-04 | Phase 4: Auto-Update CLI | Complete |
| TEST-03 | Phase 5: Integration & Polish | Complete |
| POLISH-01 | Phase 5: Integration & Polish | Complete |
| POLISH-02 | Phase 5: Integration & Polish | Complete |
| POLISH-03 | Phase 5: Integration & Polish | Complete |

**Coverage:**
- v1 requirements: 15 total
- Mapped to phases: 15 ✓
- Unmapped: 0

---
*Requirements defined: 2026-01-26*
*Last updated: 2026-01-26 after Phase 5 completion (milestone complete)*
