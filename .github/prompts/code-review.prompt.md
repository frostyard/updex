# Code Review Prompt

Use this prompt when reviewing a pull request against this repository's conventions.

## Instructions

Review the diff and flag violations of these project rules:

1. **SDK-first design** — all business logic must live in the `updex/` package.
   `cmd/` command handlers should only parse flags, call SDK methods, and format
   output. SDK code (`updex/`, `config/`, `catalog/`, `download/`, `manifest/`,
   `version/`, `sysext/`, `systemd/`) must never import Cobra, pflag, or any
   other CLI-specific package.
2. **Context-first, structured returns** — SDK functions take a
   `context.Context` as the first parameter and return typed result structs
   plus an `error`, never formatted strings.
3. **Error style** — error messages are lowercase, have no trailing
   punctuation, and wrap underlying errors with `fmt.Errorf("context: %w", err)`.
4. **Go 1.26 idioms** — prefer `any` over `interface{}`, `slices`/`maps`/`cmp`
   helpers, `t.Context()` in tests, `strings.SplitSeq`, `wg.Go()`, and
   `omitzero` on JSON struct tags for slices/maps/structs.
5. **Formatting** — `make fmt` must have been run; check for `gofmt` drift.
6. **Tests** — new behavior should include table-driven tests using
   `t.TempDir()` for filesystem state and mock `Runner` implementations for
   systemd/sysext commands, consistent with existing `_test.go` files.
7. **Documentation** — check whether `CLAUDE.md`, `README.md`, or files under
   `yeti/` need updating to reflect the change, per this repo's convention of
   keeping AI-facing documentation current.

Report findings as a checklist grouped by severity (must-fix vs. suggestion),
citing file paths and line numbers.
