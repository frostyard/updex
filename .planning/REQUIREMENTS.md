# Requirements: updex

**Defined:** 2026-01-26
**Core Value:** Users can reliably install and update systemd-sysexts from any registry without needing the unavailable updatectl package.

## v1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Core UX

- [ ] **UX-01**: User can enable a feature with `--now` flag to immediately download extensions
- [ ] **UX-02**: User can disable a feature with `--now` flag to immediately remove extension files
- [ ] **UX-03**: User disabling a feature sees extension files removed (not just update config changed)
- [ ] **UX-04**: User can pass `--reboot` to update command to reboot after update completes

### Auto-Update

- [ ] **AUTO-01**: User can generate systemd timer and service files for scheduled updates
- [ ] **AUTO-02**: User can install generated timer/service to system with `install-timer` command
- [ ] **AUTO-03**: User can check auto-update timer status with status command
- [ ] **AUTO-04**: Auto-update only stages files, does not auto-activate merged extensions

### Testing

- [ ] **TEST-01**: Core operations have unit test coverage (list, check, update, install, remove)
- [ ] **TEST-02**: Config parsing has unit test coverage (transfer, feature files)
- [ ] **TEST-03**: Integration tests validate end-to-end workflows
- [ ] **TEST-04**: Tests can run without root (mock filesystem/systemd where needed)

### Polish

- [ ] **POLISH-01**: Error messages are clear and actionable
- [ ] **POLISH-02**: Help text is comprehensive and follows conventions
- [ ] **POLISH-03**: Shell completions work for bash, zsh, and fish

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
| UX-01 | — | Pending |
| UX-02 | — | Pending |
| UX-03 | — | Pending |
| UX-04 | — | Pending |
| AUTO-01 | — | Pending |
| AUTO-02 | — | Pending |
| AUTO-03 | — | Pending |
| AUTO-04 | — | Pending |
| TEST-01 | — | Pending |
| TEST-02 | — | Pending |
| TEST-03 | — | Pending |
| TEST-04 | — | Pending |
| POLISH-01 | — | Pending |
| POLISH-02 | — | Pending |
| POLISH-03 | — | Pending |

**Coverage:**
- v1 requirements: 15 total
- Mapped to phases: 0
- Unmapped: 15 ⚠️

---
*Requirements defined: 2026-01-26*
*Last updated: 2026-01-26 after initial definition*
