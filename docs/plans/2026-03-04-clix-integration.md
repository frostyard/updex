# clix Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hand-rolled CLI infrastructure with `github.com/frostyard/clix` for standardized version injection, flags, JSON output, and reporter factory.

**Architecture:** clix wraps cobra+fang with standardized flags (`--json`, `--verbose`, `--dry-run`, `--silent`), JSON output helpers, and a reporter factory. updex keeps app-specific flags (`--definitions`, `--verify`, `--no-refresh`) in a slimmed-down `cmd/common/` package. The `cmd/updex/` package exports `NewRootCmd()` instead of `Execute()`, and `main.go` creates a `clix.App{}` to run it.

**Tech Stack:** Go 1.26, github.com/frostyard/clix, github.com/spf13/cobra

---

### Task 1: Add clix dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run: `cd /home/bjk/projects/frostyard/updex && go get github.com/frostyard/clix@latest`

**Step 2: Tidy modules**

Run: `go mod tidy`

**Step 3: Verify**

Run: `grep frostyard/clix go.mod`
Expected: `github.com/frostyard/clix v0.x.x` appears in require block

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add github.com/frostyard/clix"
```

---

### Task 2: Slim down cmd/common/ — remove overlapping functionality

**Files:**
- Modify: `cmd/common/common.go`
- Modify: `cmd/common/common_test.go`

**Step 1: Rewrite common.go**

Remove: `JSONOutput` var, `RegisterCommonFlags()`, `OutputJSON()`, `OutputJSONLines()`, `encoding/json` import.
Keep: `Definitions`, `Verify`, `NoRefresh` vars, `RequireRoot()`.
Add: `RegisterAppFlags(cmd *cobra.Command)` that registers only the 3 app-specific persistent flags.

```go
package common

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// App-specific flags (not provided by clix)
var (
	Definitions string
	Verify      bool
	NoRefresh   bool
)

// RegisterAppFlags adds updex-specific persistent flags to the root command.
func RegisterAppFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&Definitions, "definitions", "C", "", "Path to directory containing .transfer and .feature files")
	cmd.PersistentFlags().BoolVar(&Verify, "verify", false, "Verify GPG signatures on SHA256SUMS")
	cmd.PersistentFlags().BoolVar(&NoRefresh, "no-refresh", false, "Skip running systemd-sysext refresh after install/update")
}

// RequireRoot checks if the current process has root privileges
// Returns an error if the process is not running as root (EUID != 0)
func RequireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this operation requires root privileges")
	}
	return nil
}
```

**Step 2: Verify common_test.go still compiles**

The existing `common_test.go` only tests `RequireRoot()` — no changes needed.

Run: `go test -v ./cmd/common/`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/common/common.go
git commit -m "refactor: slim cmd/common to app-specific flags only"
```

---

### Task 3: Rewrite root.go — export NewRootCmd(), remove version plumbing

**Files:**
- Modify: `cmd/updex/root.go`

**Step 1: Rewrite root.go**

Remove: `commit`/`date`/`builtBy` vars, `SetVersion()`, `SetCommit()`, `SetDate()`, `SetBuiltBy()`, `makeVersionString()`, `Execute()`, `init()`, imports of `context`, `fmt`, `os`, `fang`.
Add: `NewRootCmd() *cobra.Command` that builds and returns the configured command.

```go
package updex

import (
	"github.com/frostyard/updex/cmd/commands"
	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root cobra command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "updex",
		Short: "Manage systemd-sysext extensions through features",
		Long: `updex manages systemd-sysext extensions through a feature-based interface.

Features group related sysext transfers that can be enabled, disabled,
updated, and checked together. Use 'updex features' to manage them.

Configuration is read from .feature and .transfer files in:
  - /etc/sysupdate.d/
  - /run/sysupdate.d/
  - /usr/local/lib/sysupdate.d/
  - /usr/lib/sysupdate.d/`,
	}

	common.RegisterAppFlags(cmd)
	cmd.AddCommand(commands.NewFeaturesCmd())
	cmd.AddCommand(commands.NewDaemonCmd())

	return cmd
}
```

**Step 2: Verify it compiles (won't pass tests yet — main.go still references old API)**

Run: `go build ./cmd/updex/`
Expected: Build errors about `SetVersion`, `Execute` being called from main.go — that's expected, fixed in Task 4.

**Step 3: Commit**

```bash
git add cmd/updex/root.go
git commit -m "refactor: export NewRootCmd, remove version plumbing from root"
```

---

### Task 4: Rewrite main.go — use clix.App

**Files:**
- Modify: `cmd/updex-cli/main.go`

**Step 1: Rewrite main.go**

```go
package main

import (
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/cmd/updex"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "local"
)

func main() {
	app := clix.App{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	}
	if err := app.Run(updex.NewRootCmd()); err != nil {
		os.Exit(1)
	}
}
```

**Step 2: Verify it builds**

Run: `go build -o /dev/null ./cmd/updex-cli/`
Expected: Build succeeds

**Step 3: Verify version output**

Run: `go run ./cmd/updex-cli/ --version`
Expected: `dev (Commit: none) (Date: unknown) (Built by: local)` (or similar)

**Step 4: Commit**

```bash
git add cmd/updex-cli/main.go
git commit -m "refactor: use clix.App for version injection and execution"
```

---

### Task 5: Update commands — replace common.JSONOutput/OutputJSON with clix equivalents

**Files:**
- Modify: `cmd/commands/features.go`
- Modify: `cmd/commands/daemon.go`
- Modify: `cmd/commands/components.go`

**Step 1: Update components.go — use clix.NewReporter()**

```go
package commands

import (
	"github.com/frostyard/clix"
	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	return updex.NewClient(updex.ClientConfig{
		Definitions: common.Definitions,
		Verify:      common.Verify,
		Progress:    clix.NewReporter(),
	})
}
```

**Step 2: Update features.go**

Replace all occurrences:
- `common.JSONOutput` → `clix.JSONOutput`
- `common.OutputJSON(...)` → `clix.OutputJSON(...)`
- Remove `featureEnableDryRun` and `featureDisableDryRun` vars
- Remove `--dry-run` flag registration from `newFeaturesEnableCmd()` and `newFeaturesDisableCmd()`
- Replace `featureEnableDryRun` → `clix.DryRun` in `runFeaturesEnable()`
- Replace `featureDisableDryRun` → `clix.DryRun` in `runFeaturesDisable()`

Update imports: add `"github.com/frostyard/clix"`, remove `"github.com/frostyard/updex/cmd/common"` (only if no remaining references — check `common.NoRefresh` and `common.RequireRoot()` are still used).

Note: `common` import stays because `common.RequireRoot()` and `common.NoRefresh` are still used.

**Step 3: Update daemon.go**

Replace all occurrences:
- `common.JSONOutput` → `clix.JSONOutput`
- `common.OutputJSON(...)` → `clix.OutputJSON(...)`

Update imports: add `"github.com/frostyard/clix"`, keep `"github.com/frostyard/updex/cmd/common"` (still needs `common.RequireRoot()`).

**Step 4: Verify build**

Run: `go build -o /dev/null ./cmd/updex-cli/`
Expected: Build succeeds

**Step 5: Run tests**

Run: `go test -v ./cmd/...`
Expected: All tests pass

**Step 6: Commit**

```bash
git add cmd/commands/components.go cmd/commands/features.go cmd/commands/daemon.go
git commit -m "refactor: replace common.OutputJSON/JSONOutput with clix equivalents"
```

---

### Task 6: Update Makefile ldflags

**Files:**
- Modify: `Makefile`

**Step 1: Update LDFLAGS to match clix expected vars**

Current Makefile injects `main.version` and `main.buildTime`. Update to inject `main.version`, `main.commit`, `main.date`, `main.builtBy` to match the goreleaser config and the new main.go vars.

Change line 6 from:
```makefile
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"
```

To:
```makefile
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_TIME) -X main.builtBy=make"
```

Note: `COMMIT` var is added, and `builtBy` is set to `"make"` to distinguish from goreleaser builds.

**Step 2: Verify build with injected version**

Run: `make build && ./build/updex --version`
Expected: Version string with commit hash and date

**Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: update Makefile ldflags for clix vars"
```

---

### Task 7: Clean up unused dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

**Step 1: Check if fang is still a direct dependency**

After the migration, `fang` is only used transitively through clix. Check:

Run: `grep -r "charmbracelet/fang" --include="*.go" cmd/ updex/`
Expected: No direct imports remain (clix imports it internally)

**Step 2: Remove direct fang dependency if unused**

If no direct imports remain, `go mod tidy` will move it to indirect.

Run: `go mod tidy`

**Step 3: Check if progressbar is still needed**

Run: `grep -r "progressbar" --include="*.go" .`
Expected: Check if still used. If not, `go mod tidy` already handled it.

**Step 4: Run full test suite**

Run: `make check`
Expected: fmt, lint, test all pass

**Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: tidy module dependencies after clix migration"
```

---

### Task 8: Verify end-to-end

**Files:** None (verification only)

**Step 1: Build and test version**

Run: `make build && ./build/updex --version`
Expected: Proper version string

**Step 2: Verify --help shows new global flags**

Run: `./build/updex --help`
Expected: `--json`, `--verbose`, `--dry-run`, `--silent`, `--definitions`, `--verify`, `--no-refresh` all visible

**Step 3: Verify --json flag works on a subcommand**

Run: `./build/updex features list --json`
Expected: JSON output (or error about missing configs, but in JSON format)

**Step 4: Verify --dry-run is global**

Run: `./build/updex features enable --help`
Expected: No local `--dry-run` flag (it's inherited from root)

**Step 5: Run full check**

Run: `make check`
Expected: All pass
