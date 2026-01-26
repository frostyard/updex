---
phase: 03-systemd-unit-infrastructure
plan: 03
subsystem: infra
tags: [systemd, manager, unit-files, install, remove, go]

# Dependency graph
requires:
  - phase: 03-01
    provides: TimerConfig, ServiceConfig, GenerateTimer, GenerateService
  - phase: 03-02
    provides: SystemctlRunner interface, MockSystemctlRunner
provides:
  - Manager struct for unit file operations
  - NewManager() with production defaults
  - NewTestManager() for testing with injected dependencies
  - Install() for atomic timer/service file creation
  - Remove() for cleanup with stop/disable/daemon-reload
  - Exists() for checking unit file presence
affects: [04-01, 04-auto-update-cli]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Atomic file operations with rollback on partial failure"
    - "Idempotent removal (ignores non-existent files)"
    - "Configurable UnitPath for testability"

key-files:
  created:
    - internal/systemd/manager.go
    - internal/systemd/manager_test.go
  modified: []

key-decisions:
  - "Install fails if files already exist - require explicit Remove first"
  - "Remove ignores stop/disable errors (may not be running/enabled)"
  - "Cleanup on partial failure - timer removed if service write fails"

patterns-established:
  - "Manager pattern: configurable path + injected runner for testability"
  - "Atomic install: check existence → write timer → write service → daemon-reload"
  - "Idempotent remove: stop → disable → remove files → daemon-reload"

# Metrics
duration: 1min
completed: 2026-01-26
---

# Phase 3 Plan 3: Manager with Install/Remove Summary

**Manager struct with atomic Install/Remove operations for systemd timer/service unit files, fully testable with temp directories**

## Performance

- **Duration:** 1 min
- **Started:** 2026-01-26T19:10:53Z
- **Completed:** 2026-01-26T19:12:16Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- Created Manager struct with configurable UnitPath and SystemctlRunner
- Install() writes both timer and service files atomically with rollback on failure
- Remove() stops, disables, and removes unit files with daemon-reload
- Exists() checks for presence of either timer or service file
- Comprehensive test suite with 16 test cases, all using t.TempDir()

## Task Commits

Each task was committed atomically:

1. **Task 1: Create Manager with Install and Remove operations** - `6e635cd` (feat)
2. **Task 2: Create comprehensive Manager tests** - `171b8d4` (test)

## Files Created/Modified

- `internal/systemd/manager.go` - Manager struct with Install, Remove, Exists operations (120 lines)
- `internal/systemd/manager_test.go` - Comprehensive table-driven tests (387 lines)

## Decisions Made

- Install fails if files already exist (require explicit Remove first) - safer than silent overwrite
- Remove ignores Stop/Disable errors - unit may not be running or enabled
- Cleanup on partial failure - if service file write fails, timer file is removed

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 3 complete - all systemd unit infrastructure in place
- Ready for Phase 4: Auto-Update CLI (daemon enable/disable commands)
- Manager can be used directly by CLI commands to install/remove timer units

---
*Phase: 03-systemd-unit-infrastructure*
*Completed: 2026-01-26*
