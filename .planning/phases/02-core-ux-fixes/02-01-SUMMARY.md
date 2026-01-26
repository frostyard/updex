---
phase: 02-core-ux-fixes
plan: 01
subsystem: cli
tags: [features, sysext, enable, disable, flags]

# Dependency graph
requires:
  - phase: 01-test-foundation
    provides: Testing infrastructure and patterns
provides:
  - EnableFeatureOptions struct with Now, DryRun, Retry fields
  - DisableFeatureOptions with Force, DryRun fields
  - Merge state check before file removal
  - Combined --now behavior (unmerge AND remove)
affects: [02-02, 03-systemd-unit, 04-auto-update]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - Options structs for complex CLI operations
    - Merge state check before destructive operations

key-files:
  created: []
  modified:
    - updex/options.go
    - updex/features.go
    - updex/results.go
    - cmd/commands/features.go

key-decisions:
  - "--now on disable now combines unmerge AND file removal (breaking from old separate behavior)"
  - "Merge state check requires --force for active extensions with reboot warning"
  - "Keep --remove flag for backward compatibility (alias to --now)"

patterns-established:
  - "Options struct pattern: Complex commands get dedicated *Options struct"
  - "Merge state safety: Always check GetActiveVersion before removing extension files"

# Metrics
duration: 5min
completed: 2026-01-26
---

# Phase 2 Plan 1: Enable/Disable --now Flags Summary

**Immediate enable/disable with --now flag, merge state safety checks, and --dry-run preview for both commands**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-01-26T18:13:05Z
- **Completed:** 2026-01-26T18:17:36Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- EnableFeature accepts options and downloads extensions immediately with --now
- DisableFeature with --now both unmerges AND removes files (combined behavior)
- Merge state check before file removal - requires --force for active extensions
- --dry-run support for previewing enable/disable actions
- Improved CLI output showing downloaded/removed files

## Task Commits

Each task was committed atomically:

1. **Task 1: Add EnableFeatureOptions and --now flag for enable** - `7af8340` (feat)
2. **Task 2: Fix disable --now semantics and add merge state check** - `1ed65c8` (feat)

## Files Created/Modified

- `updex/options.go` - Added EnableFeatureOptions struct, extended DisableFeatureOptions with Force/DryRun
- `updex/features.go` - EnableFeature with --now download logic, DisableFeature with merge state check
- `updex/results.go` - Added DownloadedFiles and DryRun fields to FeatureActionResult
- `cmd/commands/features.go` - Added --now, --dry-run, --retry, --force flags to CLI

## Decisions Made

1. **--now combines unmerge AND removal** - Changed from separate --now (unmerge only) and --remove (files only) to --now doing both. More intuitive user experience - "now" means complete immediate effect.

2. **Merge state check with --force** - Active extensions can't be removed without --force. This prevents accidental data loss and makes reboot requirement explicit.

3. **Backward compatibility for --remove** - Kept --remove flag as alias to --now for scripts that might use it.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Enable/disable semantics are now safe and complete
- Ready for 02-02: Unit tests for enable/disable with --now, --force, --dry-run
- Merge state checking pattern established for use in other remove operations

---
*Phase: 02-core-ux-fixes*
*Completed: 2026-01-26*
