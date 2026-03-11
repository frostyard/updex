# cmd/ Directory Cleanup Design

## Problem

The `cmd/` directory has unnecessary package complexity after recent refactors.
Four packages (`cmd/commands/`, `cmd/common/`, `cmd/updex/`, `cmd/updex-cli/`)
exist where two would suffice. `cmd/common/` is vestigial (3 exported vars, a
flag registration function, and `RequireRoot()`). `components.go` is a leftover
name from a prior refactor.

## Solution

Consolidate `cmd/commands/` and `cmd/common/` into `cmd/updex/`. Delete the
two source packages entirely. Split `features.go` (431 lines) into command
definitions and handler functions.

## Target Structure

```
cmd/updex-cli/
  main.go              (unchanged — entry point)

cmd/updex/
  root.go              root command + flags + requireRoot()
  client.go            newClient() helper
  features.go          NewFeaturesCmd() + subcommand builders + flag vars
  features_run.go      run* handlers + output formatting
  daemon.go            daemon commands + handlers (moved as-is)
  completion_test.go   shell completion tests (moved, package renamed)
  root_test.go         requireRoot() test (moved from common_test.go)
```

## Deleted

- `cmd/commands/` — entire directory
- `cmd/common/` — entire directory (including `common_test.go`)

## File Origins

| Target file | Source |
|---|---|
| `root.go` | Merge of `cmd/updex/root.go` + `cmd/common/common.go` |
| `client.go` | `cmd/commands/components.go` (renamed) |
| `features.go` | Top half of `cmd/commands/features.go` (builders + flag vars) |
| `features_run.go` | Bottom half of `cmd/commands/features.go` (handlers) |
| `daemon.go` | `cmd/commands/daemon.go` (moved) |
| `completion_test.go` | `cmd/commands/completion_test.go` (moved) |
| `root_test.go` | `cmd/common/common_test.go` (moved) |

## Symbol Visibility Changes

Once cross-package access is eliminated, exported symbols become unexported:

- `Definitions` -> `definitions`
- `Verify` -> `verify`
- `NoRefresh` -> `noRefresh`
- `RegisterAppFlags()` -> `registerAppFlags()`
- `RequireRoot()` -> `requireRoot()`

Feature flag vars (`featureDisableRemove`, etc.) are already unexported.

## Invariants

- `cmd/updex-cli/main.go` is unchanged (already imports `cmd/updex`)
- No behavioral changes — only package structure and symbol visibility
- All existing tests continue to pass
