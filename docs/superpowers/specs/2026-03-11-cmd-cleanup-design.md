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
  daemon.go            daemon commands + handlers
  completion_test.go   shell completion tests
  root_test.go         requireRoot() test
```

## Deleted

- `cmd/commands/` — entire directory
- `cmd/common/` — entire directory (including `common_test.go`)

## File Origins

| Target file | Source | Changes required |
|---|---|---|
| `root.go` | Merge of `cmd/updex/root.go` + `cmd/common/common.go` | Remove `cmd/commands` and `cmd/common` imports; inline all references |
| `client.go` | `cmd/commands/components.go` | Rename file; change package to `updex`; remove `cmd/common` import; `common.Definitions` -> `definitions`, `common.Verify` -> `verify` |
| `features.go` | `cmd/commands/features.go` — command builders (`NewFeaturesCmd`, all `newFeatures*Cmd` funcs) + flag vars | Change package to `updex`; remove `cmd/common` import |
| `features_run.go` | `cmd/commands/features.go` — handler functions (`runFeaturesList`, `runFeaturesEnable`, `runFeaturesDisable`, `runFeaturesUpdate`, `runFeaturesCheck`) | Change package to `updex`; `common.RequireRoot()` -> `requireRoot()`; `common.NoRefresh` -> `noRefresh` |
| `daemon.go` | `cmd/commands/daemon.go` | Change package to `updex`; remove `cmd/common` import; `common.RequireRoot()` -> `requireRoot()` |
| `completion_test.go` | `cmd/commands/completion_test.go` | Change package to `updex` |
| `root_test.go` | `cmd/common/common_test.go` | Change package to `updex` |

## Import Removals

All moved files currently import `"github.com/frostyard/updex/cmd/common"` —
this import is removed from every file since all symbols are now package-local.

`root.go` currently imports `"github.com/frostyard/updex/cmd/commands"` — this
import is removed; calls change from `commands.NewFeaturesCmd()` to
`NewFeaturesCmd()`.

## Symbol Visibility Changes

Once cross-package access is eliminated, exported symbols become unexported:

From `cmd/common`:
- `Definitions` -> `definitions`
- `Verify` -> `verify`
- `NoRefresh` -> `noRefresh`
- `RegisterAppFlags()` -> `registerAppFlags()`
- `RequireRoot()` -> `requireRoot()`

From `cmd/commands`:
- `NewFeaturesCmd()` -> `newFeaturesCmd()`
- `NewDaemonCmd()` -> `newDaemonCmd()`
- `DaemonStatus` -> `daemonStatus`

Feature flag vars (`featureDisableRemove`, etc.) and `newClient()` are already
unexported.

## Invariants

- `cmd/updex-cli/main.go` is unchanged (already imports `cmd/updex`)
- No behavioral changes — only package structure and symbol visibility
- All existing tests continue to pass
