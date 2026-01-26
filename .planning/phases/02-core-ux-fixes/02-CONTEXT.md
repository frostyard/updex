# Phase 2: Core UX Fixes - Context

**Gathered:** 2026-01-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix dangerous remove/disable semantics so users can safely enable/disable features with immediate effect. This includes:
- `features enable --now` to immediately download extensions
- `features disable --now` to immediately remove extension files
- Disabling removes extension files (not just config changes)
- Remove operations check merge state before deleting files

</domain>

<decisions>
## Implementation Decisions

### --now flag behavior
- Without `--now`: only config changes (no file operations)
- With `--now`: config changes + download/remove files immediately
- Success message without `--now` suggests next command: "Feature 'X' enabled. Run 'updex update' to download extensions."
- Support `--dry-run` flag to preview what would happen

### Merge state warnings
- Require `--force` flag to remove/disable merged extensions
- Error message explicitly mentions reboot: "Extension 'X' is active. Removing requires --force and a reboot to take effect."
- When disabling a feature with multiple extensions, list all affected extensions in the warning
- Detect dependencies: warn if removing an extension might break something that depends on it

### Enable/disable feedback
- Show step-by-step progress: config updated, downloading, verifying, done
- **All progress output must use existing `github.com/frostyard/pm/progress` package**

### Error recovery
- `--retry` flag to opt into automatic retry for network failures (no auto-retry by default)
- Error messages: friendly message first, then raw error on new line

### Claude's Discretion
- Fail-fast vs continue-on-failure for multi-extension operations
- Rollback behavior on partial failure
- Exact retry count when `--retry` is used
- Dependency detection depth/scope

</decisions>

<specifics>
## Specific Ideas

- All progress/output formatting handled by `github.com/frostyard/pm/progress` package (existing library)
- Error format example:
  ```
  Couldn't reach registry. Check your network connection.
  Error: connection refused (registry.example.com)
  ```

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 02-core-ux-fixes*
*Context gathered: 2026-01-26*
