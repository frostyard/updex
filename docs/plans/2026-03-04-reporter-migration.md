# Reporter Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `github.com/frostyard/pm/progress` with `github.com/frostyard/std/reporter` throughout the codebase.

**Architecture:** The SDK's `Client` struct will hold a `reporter.Reporter` directly (no helper wrapper). Thin private methods on `Client` (`msg`, `warn`) delegate to the reporter. The CLI creates a `TextReporter` for human output or leaves it nil (defaulting to `NoopReporter`) for JSON mode.

**Tech Stack:** Go 1.25, `github.com/frostyard/std/reporter`

---

### Task 1: Swap dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add new dependency and remove old one**

Run:
```bash
go get github.com/frostyard/std@latest
```

Do NOT remove the old dependency yet — it will be removed automatically by `go mod tidy` after all code references are updated.

**Step 2: Verify go.mod has the new dependency**

Run: `grep frostyard/std go.mod`
Expected: A line with `github.com/frostyard/std`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add github.com/frostyard/std dependency"
```

---

### Task 2: Rewrite `updex/updex.go` to use new reporter

**Files:**
- Modify: `updex/updex.go`

**Step 1: Replace the file contents**

Change the import from `github.com/frostyard/pm/progress` to `github.com/frostyard/std/reporter`.

Replace the `Client` struct, `ClientConfig`, and `NewClient` with:

```go
package updex

import (
	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/internal/sysext"
)

// Client provides programmatic access to updex operations.
type Client struct {
	config   ClientConfig
	reporter reporter.Reporter
}

// ClientConfig holds configuration for the Client.
type ClientConfig struct {
	// Definitions is the custom path to directory containing .transfer files.
	// If empty, standard paths are used:
	//   - /etc/sysupdate.d/*.transfer
	//   - /run/sysupdate.d/*.transfer
	//   - /usr/local/lib/sysupdate.d/*.transfer
	//   - /usr/lib/sysupdate.d/*.transfer
	Definitions string

	// Verify enables GPG signature verification on SHA256SUMS files.
	Verify bool

	// Progress is an optional progress reporter for receiving progress updates.
	// If nil, a NoopReporter is used.
	Progress reporter.Reporter

	// SysextRunner is an optional runner for systemd-sysext commands.
	// If nil, uses the default runner that executes real commands.
	// Set this in tests to inject a mock.
	SysextRunner sysext.SysextRunner
}

// NewClient creates a new updex API client with the given configuration.
func NewClient(cfg ClientConfig) *Client {
	if cfg.SysextRunner != nil {
		sysext.SetRunner(cfg.SysextRunner)
	}
	r := cfg.Progress
	if r == nil {
		r = reporter.NoopReporter{}
	}
	return &Client{
		config:   cfg,
		reporter: r,
	}
}

// msg reports an informational message via the reporter.
func (c *Client) msg(format string, a ...any) {
	c.reporter.Message(format, a...)
}

// warn reports a warning via the reporter.
func (c *Client) warn(format string, a ...any) {
	c.reporter.Warning(format, a...)
}
```

**Step 2: Verify it compiles (will fail because features.go/install.go still reference helper)**

Run: `go build ./updex/`
Expected: Compilation errors in `features.go` and `install.go` referencing `c.helper`

---

### Task 3: Migrate `updex/features.go`

**Files:**
- Modify: `updex/features.go`

**Step 1: Apply all call translations**

Throughout `features.go`, apply these replacements:
- `c.helper.BeginAction(...)` → remove entirely (or keep the string as a `c.msg(...)` call if it provides useful context)
- `defer c.helper.EndAction()` → remove entirely
- `c.helper.BeginTask(...)` → `c.msg(...)`
- `c.helper.EndTask()` → remove entirely
- `c.helper.Info(...)` → `c.msg(...)`
- `c.helper.Warning(...)` → `c.warn(...)`

Specific translations for each function:

**`Features()`:**
```go
func (c *Client) Features(ctx context.Context) ([]FeatureInfo, error) {
	c.msg("Loading configurations")
	// ... (unchanged logic) ...
	// Replace c.helper.Info("No features configured") with:
	c.msg("No features configured")
	// Replace c.helper.Info(fmt.Sprintf("Found %d feature(s)", ...)) with:
	c.msg("Found %d feature(s)", len(featureInfos))
	// Remove all c.helper.EndTask() calls
	// Remove c.helper.BeginAction / defer c.helper.EndAction
```

**`EnableFeature()`:**
```go
// Remove: c.helper.BeginAction(actionName) and defer c.helper.EndAction()
// Replace: c.helper.BeginTask(fmt.Sprintf("Enabling %s", name)) → c.msg("Enabling %s", name)
// Replace: c.helper.Warning(result.Error) → c.warn("%s", result.Error)
// Replace: c.helper.Info(fmt.Sprintf(...)) → c.msg(...)
// Remove: all c.helper.EndTask() calls
// Replace: c.helper.BeginTask("Downloading extensions") → c.msg("Downloading extensions")
// Replace: c.helper.BeginTask("Refreshing sysext") → c.msg("Refreshing sysext")
```

**`DisableFeature()`:**
Same pattern as `EnableFeature`. Additionally:
```go
// Replace: c.helper.BeginTask("Unmerging extensions") → c.msg("Unmerging extensions")
// Replace: c.helper.BeginTask("Removing files") → c.msg("Removing files")
// Replace: c.helper.BeginTask("Refreshing sysext") → c.msg("Refreshing sysext")
```

**`UpdateFeatures()`:**
```go
// Remove: c.helper.BeginAction("Update features") and defer c.helper.EndAction()
// Replace: c.helper.BeginTask(fmt.Sprintf("Processing %s/%s", ...)) → c.msg("Processing %s/%s", f.Name, transfer.Component)
// Replace: c.helper.Warning(result.Error) → c.warn("%s", result.Error)
// Replace: c.helper.Info(fmt.Sprintf(...)) → c.msg(...)
// Remove: all c.helper.EndTask() calls
```

**`CheckFeatures()`:**
```go
// Remove: c.helper.BeginAction("Check features for updates") and defer c.helper.EndAction()
// Replace: c.helper.BeginTask(fmt.Sprintf("Checking %s/%s", ...)) → c.msg("Checking %s/%s", f.Name, transfer.Component)
// Replace: c.helper.Warning(fmt.Sprintf(...)) → c.warn(...)
// Replace: c.helper.Info(fmt.Sprintf(...)) → c.msg(...)
// Remove: all c.helper.EndTask() calls
```

**Important:** When replacing `c.helper.Info(fmt.Sprintf("format %s", arg))`, simplify to `c.msg("format %s", arg)` — the `msg` method already handles `Sprintf` internally.

Similarly, `c.helper.Warning(fmt.Sprintf("format %s", arg))` becomes `c.warn("format %s", arg)`.

**Step 2: Verify compilation**

Run: `go build ./updex/`
Expected: May still fail if `install.go` has errors, but `features.go` errors should be resolved.

---

### Task 4: Migrate `updex/install.go`

**Files:**
- Modify: `updex/install.go`

**Step 1: Apply all call translations**

The `installTransfer()` function has these calls to replace:
```go
// Replace: c.helper.Warning(fmt.Sprintf("failed to update symlink: %v", err))
// With:    c.warn("failed to update symlink: %v", err)

// Replace: c.helper.Warning(fmt.Sprintf("failed to link to sysext: %v", err))
// With:    c.warn("failed to link to sysext: %v", err)

// Replace: c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
// With:    c.warn("sysext refresh failed: %v", err)

// Replace: c.helper.Info("Skipping sysext refresh (--no-refresh)")
// With:    c.msg("Skipping sysext refresh (--no-refresh)")

// Replace: c.helper.Warning(fmt.Sprintf("vacuum failed: %v", err))
// With:    c.warn("vacuum failed: %v", err)
```

**Step 2: Verify SDK compiles**

Run: `go build ./updex/`
Expected: Success — no more references to `c.helper` or `progress` package.

**Step 3: Commit SDK migration**

```bash
git add updex/updex.go updex/features.go updex/install.go
git commit -m "refactor: replace pm/progress with std/reporter in SDK"
```

---

### Task 5: Update CLI layer

**Files:**
- Delete: `cmd/common/reporter.go`
- Modify: `cmd/commands/components.go`

**Step 1: Delete the local TextReporter**

```bash
rm cmd/common/reporter.go
```

**Step 2: Update `cmd/commands/components.go`**

Replace its contents with:

```go
package commands

import (
	"os"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	var r reporter.Reporter
	if !common.JSONOutput {
		r = reporter.NewTextReporter(os.Stderr)
	}

	return updex.NewClient(updex.ClientConfig{
		Definitions: common.Definitions,
		Verify:      common.Verify,
		Progress:    r,
	})
}
```

**Step 3: Verify CLI compiles**

Run: `go build ./...`
Expected: Success

**Step 4: Commit CLI changes**

```bash
git add cmd/common/reporter.go cmd/commands/components.go
git commit -m "refactor: use std/reporter in CLI, remove local TextReporter"
```

---

### Task 6: Clean up dependencies and verify

**Files:**
- Modify: `go.mod`, `go.sum`

**Step 1: Run go mod tidy to remove old dependency**

Run: `go mod tidy`

**Step 2: Verify old dependency is gone**

Run: `grep frostyard/pm go.mod`
Expected: No output (the old `github.com/frostyard/pm/progress` dependency is removed)

**Step 3: Run tests**

Run: `make check`
Expected: All tests pass, lint is clean, formatting is correct.

**Step 4: Commit cleanup**

```bash
git add go.mod go.sum
git commit -m "chore: remove github.com/frostyard/pm/progress dependency"
```

---

### Task 7: Update planning docs

**Files:**
- Modify: `.planning/codebase/ARCHITECTURE.md`
- Modify: `.planning/codebase/STRUCTURE.md`

**Step 1: Update ARCHITECTURE.md**

Find the logging/progress reporting section (around line 143) and update references from `github.com/frostyard/pm/progress` to `github.com/frostyard/std/reporter`. Change "TextReporter" references to note it now comes from the external package.

**Step 2: Update STRUCTURE.md**

Find references to `cmd/common/reporter.go` (around lines 51, 54) and remove them since the file no longer exists.

**Step 3: Commit**

```bash
git add .planning/codebase/ARCHITECTURE.md .planning/codebase/STRUCTURE.md
git commit -m "docs: update planning docs for reporter migration"
```
