---
phase: 04-auto-update-cli
plan: 01
subsystem: cli
tags: [cobra, systemd, daemon, timer, reboot, go]

# Dependency graph
requires:
  - phase: 03-03
    provides: Manager struct with Install/Remove/Exists, SystemctlRunner interface
provides:
  - daemon command with enable/disable/status subcommands
  - --reboot flag for update command
  - CLI exposure of Phase 3 systemd infrastructure
affects: [05-integration-polish]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Command group with subcommands (daemon enable/disable/status)"
    - "Root check before privileged operations"
    - "JSON output mode for all commands"

key-files:
  created:
    - cmd/commands/daemon.go
  modified:
    - cmd/updex/root.go
    - cmd/commands/update.go

key-decisions:
  - "Fixed daily schedule for auto-update (configurable schedule deferred to v2)"
  - "Service ExecStart uses --no-refresh to stage files only (AUTO-04)"
  - "Reboot only triggers when anyInstalled && err == nil"

patterns-established:
  - "Daemon command pattern: uses Manager for install/remove, Runner for enable/start"
  - "Status doesn't require root (can check file existence and query systemctl)"

# Metrics
duration: 2min
completed: 2026-01-26
---

# Phase 4 Plan 1: Daemon Command and --reboot Flag Summary

**CLI commands for managing auto-update daemon via systemd timer/service, plus --reboot flag for update command**

## Performance

- **Duration:** 2 min
- **Started:** 2026-01-26T19:34:50Z
- **Completed:** 2026-01-26T19:36:30Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments

- Created `daemon` command group with enable/disable/status subcommands
- Wired Phase 3 systemd infrastructure to user-facing CLI
- Service uses `--no-refresh` ensuring auto-updates only stage files (AUTO-04)
- Added `--reboot` flag to update command for immediate activation
- All commands support `--json` output mode

## Task Commits

Each task was committed atomically:

1. **Task 1: Create daemon command with enable/disable/status subcommands** - `67b5bc4` (feat)
2. **Task 2: Register daemon command and add --reboot flag to update** - `b8df9d2` (feat)

## Files Created/Modified

- `cmd/commands/daemon.go` - Daemon command with enable/disable/status (195 lines)
- `cmd/updex/root.go` - Added NewDaemonCmd() registration
- `cmd/commands/update.go` - Added --reboot flag and reboot logic

## Decisions Made

- Used fixed "daily" schedule for timer (configurable schedule deferred per research)
- Service ExecStart is `/usr/bin/updex update --no-refresh` (satisfies AUTO-04)
- Reboot check is `reboot && anyInstalled && err == nil` (safe, no reboot on failure)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 4 complete - all auto-update CLI commands implemented
- Ready for Phase 5: Integration & Polish
- Commands verified working: `daemon enable/disable/status`, `update --reboot`

---
*Phase: 04-auto-update-cli*
*Completed: 2026-01-26*
