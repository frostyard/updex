---
phase: 05-integration-polish
plan: 02
subsystem: cli-ux
tags: [cobra, help-text, error-messages, cli]

# Dependency graph
requires:
  - phase: 04-auto-update-cli
    provides: All commands implemented
provides:
  - Actionable error messages
  - Comprehensive help text with examples
  - POLISH-01 and POLISH-02 requirements fulfilled
affects: [documentation, user-experience]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - Help text includes REQUIREMENTS/WORKFLOW sections
    - Error messages include suggestions (e.g., --component docker)

key-files:
  created: []
  modified:
    - cmd/commands/install.go
    - cmd/commands/update.go
    - cmd/commands/remove.go
    - cmd/commands/list.go
    - cmd/commands/check.go
    - cmd/commands/daemon.go
    - cmd/commands/features.go
    - cmd/commands/vacuum.go
    - cmd/commands/pending.go
    - cmd/commands/discover.go
    - cmd/commands/components.go

key-decisions:
  - "Error messages follow pattern: what happened + actionable suggestion"
  - "Help text structure: Long with sections (REQUIREMENTS, WORKFLOW, etc.) + Example with 2-3 examples"

patterns-established:
  - "Error message pattern: 'missing --component flag; specify which extension to X (e.g., --component docker)'"
  - "Help text pattern: Long description with structured sections, Example with real usage"

# Metrics
duration: 3min
completed: 2026-01-26
---

# Phase 5 Plan 2: Error Messages and Help Text Summary

**Polished all 11 command files with actionable error messages and comprehensive help text including Examples**

## Performance

- **Duration:** 3 min
- **Started:** 2026-01-26T20:06:49Z
- **Completed:** 2026-01-26T20:09:43Z
- **Tasks:** 3
- **Files modified:** 11

## Accomplishments
- Improved error messages for missing --component flag with actionable suggestions
- Added comprehensive help text to all 11 commands with REQUIREMENTS/WORKFLOW sections
- Added Example sections with 2-3 real usage examples to every command
- Verified all help output renders correctly

## Task Commits

Each task was committed atomically:

1. **Task 1: Audit and improve error messages** - `011c4ae` (fix)
2. **Task 2: Add comprehensive help text to all commands** - `481be18` (docs)
3. **Task 3: Verify help text and error improvements** - (verification only, no commit)

## Files Created/Modified
- `cmd/commands/install.go` - Improved error message, added REQUIREMENTS/WORKFLOW sections and examples
- `cmd/commands/update.go` - Added REQUIREMENTS section and more examples
- `cmd/commands/remove.go` - Improved error message, added REQUIREMENTS/BEHAVIOR sections and examples
- `cmd/commands/list.go` - Added OUTPUT COLUMNS section and examples
- `cmd/commands/check.go` - Clarified EXIT CODES section and added examples
- `cmd/commands/daemon.go` - Added SUBCOMMANDS section and examples for all subcommands
- `cmd/commands/features.go` - Added CONFIGURATION FILES/SUBCOMMANDS sections and examples
- `cmd/commands/vacuum.go` - Added WHAT IS KEPT section and examples
- `cmd/commands/pending.go` - Clarified EXIT CODES section and added examples
- `cmd/commands/discover.go` - Added WORKFLOW section and examples
- `cmd/commands/components.go` - Added OUTPUT COLUMNS section and examples

## Decisions Made
- Error messages follow pattern: "missing --X flag; specify which extension to Y (e.g., --X Z)"
- Help text includes structured sections appropriate to command type
- All commands have at least 2-3 examples in Example section

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## Next Phase Readiness
- POLISH-01 (clear error messages) fulfilled
- POLISH-02 (comprehensive help text) fulfilled
- Ready for 05-03-PLAN.md (shell completions verification)

---
*Phase: 05-integration-polish*
*Completed: 2026-01-26*
