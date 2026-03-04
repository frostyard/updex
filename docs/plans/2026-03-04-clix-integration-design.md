# Integrate clix for unified CLI functionality

**Date:** 2026-03-04
**Status:** Approved

## Problem

updex hand-rolls version injection, common flags, JSON output helpers, and reporter factory logic that is duplicated across frostyard CLIs. The `clix` library (`github.com/frostyard/clix`) standardizes this.

## Approach: Full clix adoption

Replace all overlapping CLI infrastructure with clix equivalents. Keep only updex-specific flags in `cmd/common/`.

## Changes

### main.go

Replace manual `SetVersion/SetCommit/SetDate/SetBuiltBy` calls with a `clix.App{}` struct. Call `app.Run(updex.NewRootCmd())` instead of `updex.Execute()`.

### root.go

- Export `NewRootCmd() *cobra.Command` instead of `Execute() error`
- Remove `SetVersion`, `SetCommit`, `SetDate`, `SetBuiltBy`, `makeVersionString()`
- Remove package-level `commit`, `date`, `builtBy` variables
- Remove direct `fang.Execute()` call (clix handles this)
- Call `common.RegisterAppFlags(rootCmd)` for updex-specific flags

### cmd/common/

**Remove:** `JSONOutput` flag, `RegisterCommonFlags()`, `OutputJSON()`, `OutputJSONLines()`

**Keep:** `Definitions`, `Verify`, `NoRefresh` flags, `RequireRoot()`

**Rename:** `RegisterCommonFlags()` → `RegisterAppFlags()` (registers only the 3 app-specific persistent flags)

### commands/features.go and daemon.go

- `common.JSONOutput` → `clix.JSONOutput`
- `common.OutputJSON(x)` → `clix.OutputJSON(x)`
- Remove per-command `--dry-run` flags, use `clix.DryRun` globally
- Pass `clix.DryRun` into SDK option structs

### commands/components.go

Replace manual reporter creation with `clix.NewReporter()`. This automatically handles silent/JSON/text modes based on flags.

### Tests

- Remove tests for deleted `common` functions
- Keep `RequireRoot` tests
- Verify completion tests still pass
- Verify flag behavior with clix globals

## Decisions

- **Global --dry-run:** Use clix's global `--dry-run` flag instead of per-command flags
- **App-specific flags stay in cmd/common/:** `--definitions`, `--verify`, `--no-refresh` remain in a slimmed-down `cmd/common/` package
- **New flags for free:** `--verbose` and `--silent` become available via clix
