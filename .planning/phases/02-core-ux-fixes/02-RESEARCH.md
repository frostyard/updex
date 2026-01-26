# Phase 2: Core UX Fixes - Research

**Researched:** 2026-01-26
**Domain:** Go CLI UX, progress reporting, feature enable/disable semantics
**Confidence:** HIGH

## Summary

This phase implements core UX improvements for the updex CLI's feature enable/disable commands. The primary focus is adding `--now` flag behavior to immediately download extensions when enabling and remove files when disabling, along with merge state safety checks. The existing codebase already has partial `--now` flag support for disable (unmerge only), but needs extension to include the file download/remove functionality.

The existing progress reporting system uses `github.com/frostyard/pm/progress` package, which provides a structured action/task/step/message hierarchy. The CLI uses a `TextReporter` that prints formatted output. All progress reporting in Phase 2 MUST use this existing system - no alternative progress output mechanisms should be introduced.

Key technical challenges:
1. Extending `EnableFeature` to support `--now` (trigger download)
2. Extending `DisableFeature` to remove files properly (not just unmerge)
3. Adding merge state detection before dangerous operations
4. Implementing `--force` flag for merged extension removal
5. Adding `--dry-run` preview capability

**Primary recommendation:** Extend existing `EnableFeature` and `DisableFeature` methods with new options, reusing the existing `Update` download logic and `RemoveAllVersions` removal logic, while adding merge state checks via `sysext.GetActiveVersion()`.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/frostyard/pm/progress` | v0.2.1 | Progress reporting | Already used, provides action/task/step hierarchy |
| `github.com/spf13/cobra` | v1.10.2 | CLI command framework | Already used for all commands |
| Go stdlib `os` | Go 1.25 | File operations | Remove, symlink management |
| Go stdlib `path/filepath` | Go 1.25 | Path manipulation | Target path construction |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/frostyard/updex/internal/sysext` | local | Extension management | All sysext operations (merge state, remove, refresh) |
| `github.com/frostyard/updex/internal/manifest` | local | Manifest fetching | Download operations when `--now` is used |
| `github.com/frostyard/updex/internal/download` | local | File download | Downloading extensions for `--now` enable |
| `github.com/frostyard/updex/internal/config` | local | Transfer/feature config | Loading feature transfers |

### Not Needed
| Instead of | Avoid | Reason |
|------------|-------|--------|
| Custom progress bars | - | Use `progress.ProgressHelper` already in codebase |
| Alternative retry libs | - | Simple loop is sufficient for `--retry` flag |
| Custom error formatting | - | Use existing error return patterns |

**No additional dependencies required.** All functionality can be built with existing packages.

## Architecture Patterns

### Recommended Structure

Build on existing patterns in `updex/features.go`:

```
updex/
├── features.go          # Extend EnableFeature, DisableFeature
├── options.go           # Add EnableFeatureOptions, extend DisableFeatureOptions
├── results.go           # Extend FeatureActionResult
├── features_test.go     # NEW: Tests for feature enable/disable with --now
cmd/commands/
├── features.go          # Add --now, --force, --dry-run, --retry flags
```

### Pattern 1: Extended Options Pattern
**What:** Add new options structs following existing conventions
**When to use:** For all new flag behaviors
**Example:**
```go
// Source: Existing pattern in updex/options.go

// EnableFeatureOptions configures the EnableFeature operation.
type EnableFeatureOptions struct {
    // Now immediately downloads extensions after enabling
    Now bool

    // DryRun previews what would happen without making changes
    DryRun bool

    // Retry enables automatic retry on network failures
    Retry bool

    // RetryCount is the number of retries when Retry is true (default: 3)
    RetryCount int

    // NoRefresh skips running systemd-sysext refresh after enable
    NoRefresh bool
}

// DisableFeatureOptions (extend existing)
type DisableFeatureOptions struct {
    Remove    bool // existing
    Now       bool // existing
    NoRefresh bool // existing

    // NEW fields:
    Force  bool // Allow removing merged extensions
    DryRun bool // Preview what would happen
}
```

### Pattern 2: Progress Reporting via ProgressHelper
**What:** Use existing `c.helper` methods for all output
**When to use:** All status updates, warnings, progress indication
**Example:**
```go
// Source: Existing pattern in updex/features.go, updex/update.go

func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
    c.helper.BeginAction("Enable feature")
    defer c.helper.EndAction()

    c.helper.BeginTask(fmt.Sprintf("Enabling %s", name))
    // ... enable logic ...
    c.helper.Info("Config updated")
    c.helper.EndTask()

    if opts.Now {
        c.helper.BeginTask("Downloading extensions")
        for _, transfer := range featureTransfers {
            c.helper.Info(fmt.Sprintf("Downloading %s", transfer.Component))
            // ... download logic ...
        }
        c.helper.EndTask()
    }

    return result, nil
}
```

### Pattern 3: Merge State Detection
**What:** Check if extension is merged before allowing removal
**When to use:** Before any file removal operation
**Example:**
```go
// Source: Existing sysext.GetActiveVersion in internal/sysext/manager.go

func (c *Client) checkMergeState(transfer *config.Transfer) (bool, string, error) {
    activeVersion, err := sysext.GetActiveVersion(transfer)
    if err != nil {
        return false, "", err
    }
    return activeVersion != "", activeVersion, nil
}

// In DisableFeature:
if isMerged && !opts.Force {
    result.Error = fmt.Sprintf("Extension '%s' is active. Removing requires --force and a reboot to take effect.", name)
    return result, fmt.Errorf("%s", result.Error)
}
```

### Pattern 4: Dry Run Support
**What:** Preview operations without executing them
**When to use:** For --dry-run flag support
**Example:**
```go
// Source: Standard pattern for dry-run in CLI tools

func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
    if opts.DryRun {
        c.helper.BeginAction("Enable feature (dry run)")
    } else {
        c.helper.BeginAction("Enable feature")
    }
    defer c.helper.EndAction()

    // ... validation logic (always runs) ...

    if opts.DryRun {
        result.WouldDownload = transferNames
        result.NextActionMessage = "Dry run - no changes made"
        return result, nil
    }

    // ... actual execution ...
}
```

### Anti-Patterns to Avoid
- **Direct fmt.Print in client code:** Use `c.helper.Info()` etc. instead
- **Custom progress formatting:** Use existing TextReporter patterns
- **Inline download logic:** Reuse `installTransfer()` or create shared helper
- **Ignoring existing test patterns:** Follow Phase 1 established test infrastructure

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Download with hash verification | Custom HTTP + hash code | `download.Download()` | Handles decompression, progress bar, atomic writes |
| Merge state detection | Parse `/run/extensions/` manually | `sysext.GetActiveVersion()` | Already handles symlinks, multiple patterns |
| Remove all versions | Manual directory walk | `sysext.RemoveAllVersions()` | Handles patterns, symlinks, error recovery |
| Sysext refresh | `exec.Command("systemd-sysext", ...)` | `sysext.Refresh()` | Uses mockable runner interface for tests |
| Progress output | Custom print statements | `c.helper.Info()`, `c.helper.Warning()` | Consistent formatting, JSON mode support |
| Transfer loading | Direct INI parsing | `config.LoadTransfers()`, `config.GetTransfersForFeature()` | Handles drop-ins, feature filtering |

**Key insight:** The codebase already has well-tested utilities for every file operation needed. The main work is wiring these together with proper options and progress reporting.

## Common Pitfalls

### Pitfall 1: Not Using Existing Progress Reporter
**What goes wrong:** Inconsistent output formatting, breaks JSON mode
**Why it happens:** Tempting to use fmt.Printf for "simple" messages
**How to avoid:** Always use `c.helper.Info()`, `c.helper.Warning()`, `c.helper.BeginTask()`, etc.
**Warning signs:** Direct `fmt.` calls in client methods, tests checking stdout

### Pitfall 2: Forgetting to Check Merge State
**What goes wrong:** Removing files for merged extension leaves system in broken state until reboot
**Why it happens:** Happy path testing, not considering edge case
**How to avoid:** 
1. Always call `sysext.GetActiveVersion()` before removal
2. Require `--force` for merged extensions
3. Error message must mention reboot requirement
**Warning signs:** Remove succeeds without warning, extension stops working immediately

### Pitfall 3: Partial Failure Without Rollback
**What goes wrong:** Multi-extension operation fails partway through, leaves partial state
**Why it happens:** Not handling errors in loops, no rollback strategy
**How to avoid:**
1. Decide on fail-fast vs continue-on-failure (Claude's discretion)
2. If continue-on-failure: collect all errors, report all at end
3. If fail-fast: stop on first error, document what was completed
4. Consider rollback for partial config changes
**Warning signs:** Error during 3rd extension, first 2 still modified

### Pitfall 4: Inconsistent --now Semantics Between Enable and Disable
**What goes wrong:** `enable --now` does something, `disable --now` does something different
**Why it happens:** Different developers, different interpretation
**How to avoid:** 
- `enable --now`: config + download + refresh
- `disable --now`: config + remove files + unmerge (if force) + refresh
- Document symmetry explicitly
**Warning signs:** User confusion, different result structures

### Pitfall 5: Not Mocking in Tests
**What goes wrong:** Tests fail without root, tests skip on CI
**Why it happens:** Forgetting to inject MockRunner, using real paths
**How to avoid:**
1. Every test must call `sysext.SetRunner(mockRunner)` with cleanup
2. Use `t.TempDir()` for all paths
3. Use `testutil.NewTestServer()` for HTTP mocking
**Warning signs:** Tests require sudo, tests modify `/var/lib/extensions`

### Pitfall 6: Forgetting to Update Results Struct
**What goes wrong:** New fields not serialized, JSON output missing data
**Why it happens:** Adding functionality without updating result types
**How to avoid:**
1. Add fields to `FeatureActionResult` for all new data
2. Include JSON tags with proper names
3. Test JSON output explicitly
**Warning signs:** CLI shows info that JSON mode doesn't

## Code Examples

Verified patterns from existing codebase:

### Enable Feature with --now (extending existing method)
```go
// Source: Pattern from updex/install.go installTransfer method

func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
    c.helper.BeginAction("Enable feature")
    defer c.helper.EndAction()

    result := &FeatureActionResult{
        Feature: name,
        Action:  "enable",
    }

    // ... existing validation and config update logic ...

    if opts.Now {
        c.helper.BeginTask("Downloading extensions")

        transfers, err := config.LoadTransfers(c.config.Definitions)
        if err != nil {
            result.Error = fmt.Sprintf("failed to load transfers: %v", err)
            c.helper.Warning(result.Error)
            c.helper.EndTask()
            return result, fmt.Errorf("%s", result.Error)
        }

        featureTransfers := config.GetTransfersForFeature(transfers, name)

        for _, transfer := range featureTransfers {
            c.helper.Info(fmt.Sprintf("Downloading %s", transfer.Component))

            // Reuse existing download logic
            version, err := c.installTransfer(transfer, opts.NoRefresh)
            if err != nil {
                c.helper.Warning(fmt.Sprintf("failed to download %s: %v", transfer.Component, err))
                if !opts.ContinueOnError {
                    result.Error = fmt.Sprintf("failed to download %s: %v", transfer.Component, err)
                    c.helper.EndTask()
                    return result, err
                }
                // continue to next transfer
            }
            result.DownloadedVersions = append(result.DownloadedVersions, 
                fmt.Sprintf("%s@%s", transfer.Component, version))
        }

        c.helper.EndTask()
    }

    result.Success = true
    if opts.Now {
        result.NextActionMessage = "Feature enabled and extensions downloaded"
    } else {
        result.NextActionMessage = "Feature 'X' enabled. Run 'updex update' to download extensions."
    }

    return result, nil
}
```

### Disable Feature with Merge State Check
```go
// Source: Pattern from updex/remove.go, extended with merge check

func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error) {
    c.helper.BeginAction("Disable feature")
    defer c.helper.EndAction()

    result := &FeatureActionResult{
        Feature: name,
        Action:  "disable",
    }

    // ... validation logic ...

    // Get transfers for this feature
    transfers, _ := config.LoadTransfers(c.config.Definitions)
    featureTransfers := config.GetTransfersForFeature(transfers, name)

    // Check merge state for all transfers
    if opts.Now || opts.Remove {
        var mergedExtensions []string
        for _, t := range featureTransfers {
            activeVersion, _ := sysext.GetActiveVersion(t)
            if activeVersion != "" {
                mergedExtensions = append(mergedExtensions, t.Component)
            }
        }

        if len(mergedExtensions) > 0 && !opts.Force {
            if len(mergedExtensions) == 1 {
                result.Error = fmt.Sprintf("Extension '%s' is active. Removing requires --force and a reboot to take effect.", mergedExtensions[0])
            } else {
                result.Error = fmt.Sprintf("Extensions %v are active. Removing requires --force and a reboot to take effect.", mergedExtensions)
            }
            c.helper.Warning(result.Error)
            return result, fmt.Errorf("%s", result.Error)
        }
    }

    // ... existing config update logic ...

    // Remove files if requested
    if opts.Remove {
        c.helper.BeginTask("Removing files")
        var allRemoved []string
        for _, t := range featureTransfers {
            if err := sysext.UnlinkFromSysext(t); err != nil {
                c.helper.Warning(fmt.Sprintf("failed to unlink %s: %v", t.Component, err))
            }
            removed, err := sysext.RemoveAllVersions(t)
            if err != nil {
                c.helper.Warning(fmt.Sprintf("failed to remove files for %s: %v", t.Component, err))
            }
            allRemoved = append(allRemoved, removed...)
        }
        result.RemovedFiles = allRemoved
        c.helper.Info(fmt.Sprintf("Removed %d file(s)", len(allRemoved)))
        c.helper.EndTask()
    }

    // ... existing unmerge/refresh logic ...

    return result, nil
}
```

### Dry Run Implementation
```go
// Source: Standard CLI pattern

func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
    actionName := "Enable feature"
    if opts.DryRun {
        actionName = "Enable feature (dry run)"
    }
    c.helper.BeginAction(actionName)
    defer c.helper.EndAction()

    // ... validation (always runs) ...

    // Collect what would happen
    transfers, _ := config.LoadTransfers(c.config.Definitions)
    featureTransfers := config.GetTransfersForFeature(transfers, name)

    if opts.DryRun {
        var wouldDownload []string
        for _, t := range featureTransfers {
            wouldDownload = append(wouldDownload, t.Component)
        }
        result.WouldDownload = wouldDownload
        result.NextActionMessage = fmt.Sprintf("Would enable feature '%s' and download %d extension(s)", name, len(wouldDownload))
        c.helper.Info(result.NextActionMessage)
        return result, nil
    }

    // ... actual execution ...
}
```

### Error Format Pattern
```go
// Source: CONTEXT.md decision

// User-friendly error with raw error on new line
func formatError(friendly string, err error) string {
    return fmt.Sprintf("%s\nError: %v", friendly, err)
}

// Usage in feature operations
if err := download.Download(url, path, hash, mode); err != nil {
    friendlyMsg := "Couldn't reach registry. Check your network connection."
    result.Error = formatError(friendlyMsg, err)
    c.helper.Warning(result.Error)
    return result, fmt.Errorf("%s", result.Error)
}
```

### Test Pattern for Feature Operations
```go
// Source: Pattern from updex/remove_test.go, updex/update_test.go

func TestEnableFeatureNow(t *testing.T) {
    tests := []struct {
        name            string
        featureName     string
        setupConfig     func(*testing.T, string)          // configDir
        setupServer     func(*testing.T) *testutil.TestServerFiles
        setupTarget     func(*testing.T, string)          // targetDir
        opts            EnableFeatureOptions
        wantSuccess     bool
        wantDownloads   int
        wantErr         bool
        wantErrContains string
    }{
        {
            name:        "enable with --now downloads extensions",
            featureName: "devel",
            setupConfig: func(t *testing.T, configDir string) {
                // Create feature file
                createFeatureFile(t, configDir, "devel", true)
                // Create transfer for the feature
                createTransferFileWithFeature(t, configDir, "myext", "http://example.com", "devel")
            },
            setupServer: func(t *testing.T) *testutil.TestServerFiles {
                return &testutil.TestServerFiles{
                    Files: map[string]string{
                        "myext_1.0.0.raw": "8653bf0e654b5eef4044b95b5c491dc1b29349f46a4b572737b9d6f92aaf4c82",
                    },
                    Content: map[string][]byte{
                        "myext_1.0.0.raw": []byte("fake extension content"),
                    },
                }
            },
            setupTarget: func(t *testing.T, targetDir string) {},
            opts:        EnableFeatureOptions{Now: true, NoRefresh: true},
            wantSuccess: true,
            wantDownloads: 1,
            wantErr:     false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            configDir := t.TempDir()
            targetDir := t.TempDir()

            // Set up mock runner
            mockRunner := &sysext.MockRunner{}
            cleanup := sysext.SetRunner(mockRunner)
            defer cleanup()

            // Set up HTTP server
            var serverURL string
            if tt.setupServer != nil {
                files := tt.setupServer(t)
                server := testutil.NewTestServer(t, *files)
                defer server.Close()
                serverURL = server.URL
            }

            tt.setupConfig(t, configDir)
            tt.setupTarget(t, targetDir)
            updateTransferTargetPath(t, configDir, targetDir)
            // Update source URL in transfer files
            updateTransferSourceURL(t, configDir, serverURL)

            client := NewClient(ClientConfig{Definitions: configDir})
            result, err := client.EnableFeature(context.Background(), tt.featureName, tt.opts)

            if tt.wantErr {
                if err == nil {
                    t.Error("expected error, got nil")
                }
            } else if err != nil {
                t.Errorf("unexpected error: %v", err)
            }

            if result != nil && result.Success != tt.wantSuccess {
                t.Errorf("Success = %v, want %v", result.Success, tt.wantSuccess)
            }
        })
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `--now` only unmerges | `--now` also removes files | This phase | Full disable semantics |
| No merge state check | Check and require `--force` | This phase | Safer operations |
| Enable doesn't download | `enable --now` triggers download | This phase | Immediate effect |

**Deprecated/outdated:**
- Direct systemd-sysext calls: Use `sysext.Runner` interface for testability

## Open Questions

Things that couldn't be fully resolved (Claude's discretion from CONTEXT.md):

1. **Fail-fast vs continue-on-failure for multi-extension operations**
   - What we know: Some users want all extensions attempted, others want immediate stop
   - What's unclear: No explicit user preference documented
   - Recommendation: **Fail-fast by default** - stop on first error, report what was completed. Safer, easier to reason about. Future: could add `--continue-on-error` flag if needed.

2. **Rollback behavior on partial failure**
   - What we know: If 2 of 3 extensions succeed before failure, state is inconsistent
   - What's unclear: Should we undo successful operations?
   - Recommendation: **No automatic rollback** - complex to implement correctly, and explicit user action to retry is safer. Document what completed in result.

3. **Exact retry count when `--retry` is used**
   - What we know: User opted into retries for network failures
   - What's unclear: How many attempts is reasonable?
   - Recommendation: **3 retries** with exponential backoff (1s, 2s, 4s). Standard practice, not excessive.

4. **Dependency detection depth/scope**
   - What we know: Should warn if removing extension X might break extension Y
   - What's unclear: How deep to analyze, what constitutes a dependency
   - Recommendation: **Phase 2 scope: no dependency detection** - complex feature, defer to future phase. Current check is just merge state (is it active?). Add TODO comment for future enhancement.

## Sources

### Primary (HIGH confidence)
- Existing `updex/features.go` - current implementation patterns
- Existing `updex/update.go` - download logic via `installTransfer()`
- Existing `internal/sysext/manager.go` - `GetActiveVersion()`, `RemoveAllVersions()`, `UnlinkFromSysext()`
- Existing `updex/results.go` - result struct patterns
- Existing `updex/options.go` - options struct patterns
- Existing Phase 1 test patterns - `MockRunner`, `testutil.NewTestServer`
- Phase 2 CONTEXT.md - user decisions and constraints

### Secondary (MEDIUM confidence)
- `github.com/frostyard/pm/progress` package (not queried via Context7, but verified in codebase)

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - all packages already in use, no new dependencies
- Architecture: HIGH - extends existing patterns, well-documented in codebase
- Pitfalls: HIGH - based on codebase analysis and common CLI patterns
- Code examples: HIGH - derived directly from existing codebase

**Research date:** 2026-01-26
**Valid until:** 2026-02-26 (stable patterns, no external dependencies changing)
