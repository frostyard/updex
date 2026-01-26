---
phase: 03-systemd-unit-infrastructure
plan: 01
subsystem: infra
tags: [systemd, timer, service, unit-files, go]

# Dependency graph
requires:
  - phase: 02-core-ux-fixes
    provides: "Safe operations to call from timer"
provides:
  - TimerConfig and ServiceConfig types for unit file configuration
  - GenerateTimer and GenerateService functions for unit content generation
  - Comprehensive test coverage for unit generation
affects: [03-02, 03-03, 04-01]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "strings.Builder for efficient string concatenation"
    - "Table-driven tests with contains/excludes verification"

key-files:
  created:
    - internal/systemd/unit.go
    - internal/systemd/unit_test.go
  modified: []

key-decisions:
  - "Use strings.Builder over text/template for simpler unit file generation"
  - "No [Install] section for services - timer handles activation"
  - "Table-driven tests with flexible string matching (strings.Contains)"

patterns-established:
  - "Systemd unit generation: struct config → GenerateX function → string output"
  - "Section order verification in tests"

# Metrics
duration: 1min
completed: 2026-01-26
---

# Phase 3 Plan 1: Unit Types and Generation Summary

**TimerConfig/ServiceConfig types and GenerateTimer/GenerateService functions for programmatic systemd unit file generation**

## Performance

- **Duration:** 1 min
- **Started:** 2026-01-26T19:07:22Z
- **Completed:** 2026-01-26T19:08:46Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- Created internal/systemd package with unit file generation capability
- TimerConfig and ServiceConfig structs with documented fields
- GenerateTimer produces valid [Unit], [Timer], [Install] sections
- GenerateService produces valid [Unit], [Service] sections (no [Install] - timer handles activation)
- 8 comprehensive test cases covering all generation paths

## Task Commits

Each task was committed atomically:

1. **Task 1: Create unit types and generation functions** - `f9ae207` (feat)
2. **Task 2: Create comprehensive unit generation tests** - `7a04ed4` (test)

## Files Created/Modified
- `internal/systemd/unit.go` - TimerConfig, ServiceConfig types and GenerateTimer, GenerateService functions (83 lines)
- `internal/systemd/unit_test.go` - Comprehensive table-driven tests with 8 test cases (227 lines)

## Decisions Made
- Used strings.Builder over text/template for simpler unit file generation (matches research recommendation)
- Services don't include [Install] section since timers handle activation
- Tests use strings.Contains for flexible matching, allowing formatting flexibility

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None

## Next Phase Readiness
- Unit generation foundation complete
- Ready for 03-02-PLAN.md (SystemctlRunner interface and mock)
- GenerateTimer and GenerateService available for Manager to use

---
*Phase: 03-systemd-unit-infrastructure*
*Completed: 2026-01-26*
