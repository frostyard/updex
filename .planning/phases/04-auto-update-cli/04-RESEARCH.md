# Phase 4: Auto-Update CLI - Research

**Researched:** 2026-01-26
**Domain:** CLI command implementation for systemd timer management, Go cobra subcommands
**Confidence:** HIGH

## Summary

This phase exposes the Phase 3 systemd infrastructure via CLI commands (`daemon enable`, `daemon disable`, `daemon status`) and adds a `--reboot` flag to the update command. The work is primarily wiring—connecting existing infrastructure (internal/systemd package) to new CLI commands following established project patterns.

The project already has a clear pattern for nested cobra commands (see `features` command with `list`, `enable`, `disable` subcommands). The new `daemon` command will follow this identical pattern. The systemd Manager from Phase 3 provides Install/Remove/Exists operations, and SystemctlRunner provides IsActive/IsEnabled for status checking.

For AUTO-04 (staging only, no auto-activation), the existing update logic already stages files via symlinks without calling `sysext refresh` when `--no-refresh` is passed. The daemon service file should invoke `updex update --no-refresh` to ensure auto-updates only stage files for next reboot.

**Primary recommendation:** Create a `daemon` command group with enable/disable/status subcommands, following the exact patterns from `features.go`, using the Phase 3 systemd Manager.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/spf13/cobra` | existing dep | CLI framework | Already used by project |
| Go stdlib `os` | Go stdlib | File operations, EUID check | Standard for permissions |
| Go stdlib `os/exec` | Go stdlib | Execute systemctl/reboot | Standard for commands |
| `internal/systemd` | Phase 3 | Timer/service management | Just completed |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `text/tabwriter` | Go stdlib | Status output formatting | For daemon status table |
| `encoding/json` | Go stdlib | JSON output mode | When --json flag present |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| exec.Command for reboot | syscall.Reboot | syscall more complex, exec is project pattern |
| New reboot interface | Direct exec call | Reboot is rare, testable via interface overkill |

**Installation:** None required - uses existing dependencies only.

## Architecture Patterns

### Recommended Project Structure
```
cmd/
├── commands/
│   ├── daemon.go       # NEW: daemon command group with enable/disable/status
│   ├── features.go     # EXISTING: pattern to follow
│   └── update.go       # MODIFY: add --reboot flag
├── common/
│   └── common.go       # May need reboot helper
internal/
├── systemd/
│   ├── manager.go      # EXISTING: Use Install/Remove/Exists
│   ├── runner.go       # EXISTING: Use IsActive/IsEnabled
│   └── mock_runner.go  # EXISTING: For testing
```

### Pattern 1: Command Group with Subcommands (from features.go)
**What:** Parent command with child subcommands for related operations
**When to use:** When a concept has multiple related operations (daemon enable/disable/status)
**Example:**
```go
// Source: cmd/commands/features.go - project pattern

func NewDaemonCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "daemon",
        Short: "Manage auto-update daemon",
        Long: `Manage the automatic update timer and service.

The daemon periodically checks for and downloads new extension versions.
Updates are staged but not activated until next reboot.`,
    }

    cmd.AddCommand(newDaemonEnableCmd())
    cmd.AddCommand(newDaemonDisableCmd())
    cmd.AddCommand(newDaemonStatusCmd())

    return cmd
}

func newDaemonEnableCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "enable",
        Short: "Enable automatic updates",
        Long:  `Install and enable the systemd timer for automatic updates.`,
        Args:  cobra.NoArgs,
        RunE:  runDaemonEnable,
    }
}
```

### Pattern 2: Root Command Check and Manager Usage
**What:** Check root, create manager, call operations
**When to use:** Any privileged systemd operations
**Example:**
```go
// Source: cmd/commands/features.go + internal/systemd/manager.go patterns

func runDaemonEnable(cmd *cobra.Command, args []string) error {
    // Check for root privileges
    if err := common.RequireRoot(); err != nil {
        return err
    }

    mgr := systemd.NewManager()

    // Check if already installed
    if mgr.Exists("updex-update") {
        return fmt.Errorf("daemon already installed; run 'updex daemon disable' first")
    }

    timer := &systemd.TimerConfig{
        Name:           "updex-update",
        Description:    "Automatic sysext updates",
        OnCalendar:     "daily",
        Persistent:     true,
        RandomDelaySec: 3600, // 1 hour
    }
    service := &systemd.ServiceConfig{
        Name:        "updex-update",
        Description: "Automatic sysext update service",
        ExecStart:   "/usr/bin/updex update --no-refresh",
        Type:        "oneshot",
    }

    if err := mgr.Install(timer, service); err != nil {
        return fmt.Errorf("failed to install timer: %w", err)
    }

    // Enable and start the timer
    runner := &systemd.DefaultSystemctlRunner{}
    if err := runner.Enable("updex-update.timer"); err != nil {
        return fmt.Errorf("failed to enable timer: %w", err)
    }
    if err := runner.Start("updex-update.timer"); err != nil {
        return fmt.Errorf("failed to start timer: %w", err)
    }

    fmt.Println("Auto-update daemon enabled. Updates will run daily.")
    return nil
}
```

### Pattern 3: Status Output with JSON Support
**What:** Check timer state and output in text or JSON
**When to use:** For status command
**Example:**
```go
// Source: cmd/commands/list.go + internal/systemd/runner.go patterns

type DaemonStatus struct {
    Installed bool   `json:"installed"`
    Enabled   bool   `json:"enabled"`
    Active    bool   `json:"active"`
    Schedule  string `json:"schedule,omitempty"`
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
    mgr := systemd.NewManager()
    runner := &systemd.DefaultSystemctlRunner{}

    status := DaemonStatus{
        Installed: mgr.Exists("updex-update"),
    }

    if status.Installed {
        status.Enabled, _ = runner.IsEnabled("updex-update.timer")
        status.Active, _ = runner.IsActive("updex-update.timer")
        status.Schedule = "daily" // Fixed schedule for now
    }

    if common.JSONOutput {
        common.OutputJSON(status)
        return nil
    }

    // Text output
    if !status.Installed {
        fmt.Println("Auto-update daemon: not installed")
        fmt.Println("Run 'updex daemon enable' to enable automatic updates.")
        return nil
    }

    fmt.Println("Auto-update daemon: installed")
    fmt.Printf("  Enabled: %v\n", status.Enabled)
    fmt.Printf("  Active: %v\n", status.Active)
    fmt.Printf("  Schedule: %s\n", status.Schedule)
    return nil
}
```

### Pattern 4: Reboot Flag on Update Command
**What:** Add --reboot flag that triggers system reboot after successful update
**When to use:** For UX-04 requirement
**Example:**
```go
// Source: cmd/commands/update.go - add flag and reboot logic

var (
    noVacuum bool
    reboot   bool  // NEW
)

func NewUpdateCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "update [VERSION]",
        Short: "Download and install a new version",
        // ... existing Long text ...
        Args: cobra.MaximumNArgs(1),
        RunE: runUpdate,
    }
    cmd.Flags().BoolVar(&noVacuum, "no-vacuum", false, "Do not remove old versions after update")
    cmd.Flags().BoolVar(&reboot, "reboot", false, "Reboot system after successful update")
    return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
    // ... existing update logic ...

    // After successful update with downloads
    if reboot && anyInstalled {
        fmt.Println("Rebooting system to activate changes...")
        return exec.Command("systemctl", "reboot").Run()
    }

    return err
}
```

### Anti-Patterns to Avoid
- **Hardcoding paths:** Use Manager's UnitPath, not literal "/etc/systemd/system"
- **Forgetting root check:** All daemon operations require root
- **Ignoring --json:** All commands should respect common.JSONOutput
- **Complex reboot logic:** Keep reboot simple - just call systemctl reboot
- **Coupling to specific timer names:** Use constants for "updex-update"

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Root permission check | Custom EUID check | common.RequireRoot() | Already exists in project |
| JSON output | Manual json.Marshal | common.OutputJSON() | Project pattern, handles errors |
| Timer/service generation | String templates | systemd.GenerateTimer/Service | Phase 3 infrastructure |
| Timer install/remove | File operations | systemd.Manager.Install/Remove | Phase 3 infrastructure |
| Active/enabled checks | Raw systemctl | systemd.IsActive/IsEnabled | Phase 3 infrastructure |

**Key insight:** This phase is pure wiring. All heavy lifting was done in Phase 3. Focus on following existing patterns exactly.

## Common Pitfalls

### Pitfall 1: Forgetting to Enable After Install
**What goes wrong:** Timer installed but never runs
**Why it happens:** Manager.Install writes files but doesn't enable/start
**How to avoid:** After Install(), call runner.Enable() and runner.Start()
**Warning signs:** Timer shows as installed but disabled in status

### Pitfall 2: Missing --no-refresh in Service ExecStart
**What goes wrong:** Auto-update activates extensions immediately
**Why it happens:** Update without --no-refresh calls sysext refresh
**How to avoid:** Service ExecStart must be "/usr/bin/updex update --no-refresh"
**Warning signs:** Extensions appear in /run/extensions after auto-update

### Pitfall 3: Reboot Before Error Handling
**What goes wrong:** System reboots even on partial failure
**Why it happens:** Checking reboot flag before checking if updates succeeded
**How to avoid:** Only reboot if anyInstalled && err == nil
**Warning signs:** Reboot on update with no actual updates

### Pitfall 4: Not Registering Command in Root
**What goes wrong:** "unknown command: daemon"
**Why it happens:** New command not added to rootCmd in cmd/updex/root.go
**How to avoid:** Add `rootCmd.AddCommand(commands.NewDaemonCmd())` in init()
**Warning signs:** Command not in help output

### Pitfall 5: Testing Without Manager Mock
**What goes wrong:** Tests require root or modify real system
**Why it happens:** Using systemd.NewManager() instead of NewTestManager()
**How to avoid:** Create systemd.Manager with configurable runner/unitPath for testing
**Warning signs:** Tests fail without root, or succeed but leave real timers

### Pitfall 6: Inconsistent Unit Names
**What goes wrong:** Enable/disable don't find the right files
**Why it happens:** Using different names in Install vs Enable/Disable calls
**How to avoid:** Define const unitName = "updex-update" and use consistently
**Warning signs:** "Unit not found" errors, orphaned files

## Code Examples

Verified patterns from project sources:

### Complete daemon.go Structure
```go
// Source: Pattern from cmd/commands/features.go

package commands

import (
    "fmt"
    "os/exec"

    "github.com/frostyard/updex/cmd/common"
    "github.com/frostyard/updex/internal/systemd"
    "github.com/spf13/cobra"
)

const unitName = "updex-update"

// NewDaemonCmd creates the daemon command with subcommands
func NewDaemonCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "daemon",
        Short: "Manage auto-update daemon",
        Long: `Manage the automatic update timer and service.

The daemon periodically checks for and downloads new extension versions.
Updates are staged but not activated until next reboot.

Use 'daemon enable' to install the timer, 'daemon disable' to remove it,
and 'daemon status' to check the current state.`,
    }

    cmd.AddCommand(newDaemonEnableCmd())
    cmd.AddCommand(newDaemonDisableCmd())
    cmd.AddCommand(newDaemonStatusCmd())

    return cmd
}
```

### Daemon Enable Implementation
```go
// Source: Pattern from cmd/commands/features.go + internal/systemd patterns

func newDaemonEnableCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "enable",
        Short: "Enable automatic updates",
        Long: `Install and enable the systemd timer for automatic updates.

This creates timer and service unit files in /etc/systemd/system/ and
enables the timer to run daily. Updates will download new versions but
not activate them until the next reboot.

Requires root privileges.`,
        Args: cobra.NoArgs,
        RunE: runDaemonEnable,
    }
}

func runDaemonEnable(cmd *cobra.Command, args []string) error {
    if err := common.RequireRoot(); err != nil {
        return err
    }

    mgr := systemd.NewManager()

    if mgr.Exists(unitName) {
        return fmt.Errorf("timer already installed; run 'updex daemon disable' first to reinstall")
    }

    timer := &systemd.TimerConfig{
        Name:           unitName,
        Description:    "Automatic sysext updates",
        OnCalendar:     "daily",
        Persistent:     true,
        RandomDelaySec: 3600,
    }
    service := &systemd.ServiceConfig{
        Name:        unitName,
        Description: "Automatic sysext update service",
        ExecStart:   "/usr/bin/updex update --no-refresh",
        Type:        "oneshot",
    }

    if err := mgr.Install(timer, service); err != nil {
        return fmt.Errorf("failed to install timer: %w", err)
    }

    runner := &systemd.DefaultSystemctlRunner{}
    if err := runner.Enable(unitName + ".timer"); err != nil {
        return fmt.Errorf("failed to enable timer: %w", err)
    }
    if err := runner.Start(unitName + ".timer"); err != nil {
        return fmt.Errorf("failed to start timer: %w", err)
    }

    if common.JSONOutput {
        common.OutputJSON(map[string]interface{}{
            "success": true,
            "message": "Auto-update daemon enabled",
        })
        return nil
    }

    fmt.Println("Auto-update daemon enabled.")
    fmt.Println("Updates will run daily and download new versions.")
    fmt.Println("Reboot required to activate downloaded extensions.")
    return nil
}
```

### Update Command with --reboot Flag
```go
// Source: cmd/commands/update.go - modifications

var (
    noVacuum bool
    reboot   bool
)

func NewUpdateCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "update [VERSION]",
        Short: "Download and install a new version",
        Long: `Download and install the newest available version, or a specific version if specified.

After installation, old versions are automatically removed according to InstancesMax
unless --no-vacuum is specified.

With --reboot flag, the system will reboot after a successful update to activate
the new extensions.`,
        Args: cobra.MaximumNArgs(1),
        RunE: runUpdate,
    }
    cmd.Flags().BoolVar(&noVacuum, "no-vacuum", false, "Do not remove old versions after update")
    cmd.Flags().BoolVar(&reboot, "reboot", false, "Reboot system after successful update")
    return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
    // ... existing root check and update logic ...

    // At end of function, before return:
    if reboot && anyInstalled && err == nil {
        if !common.JSONOutput {
            fmt.Println("\nRebooting system to activate changes...")
        }
        return exec.Command("systemctl", "reboot").Run()
    }

    return err
}
```

### Testable Daemon Commands Pattern
```go
// Source: Testing patterns from internal/systemd/manager_test.go

// For testing, the daemon commands can accept a custom manager
// However, simpler approach: test the systemd package directly,
// and integration test the CLI separately

func TestDaemonEnable_AlreadyExists(t *testing.T) {
    // This would require modifying daemon commands to accept manager
    // Alternative: test via subprocess or only test systemd package
    
    // For unit testing, focus on testing:
    // 1. systemd.Manager operations (already done in Phase 3)
    // 2. CLI integration via subprocess (Phase 5)
    
    // Minimal unit test pattern for CLI:
    tmpDir := t.TempDir()
    mockRunner := &systemd.MockSystemctlRunner{}
    mgr := systemd.NewTestManager(tmpDir, mockRunner)
    
    // Pre-create files
    os.WriteFile(filepath.Join(tmpDir, "updex-update.timer"), []byte("exists"), 0644)
    
    // Verify Exists() returns true
    if !mgr.Exists(unitName) {
        t.Error("Exists() should return true for existing timer")
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Cron jobs | systemd timers | 2012+ | Better logging, dependencies |
| Manual timer install | Manager pattern | Phase 3 | Testable, atomic operations |
| Update with immediate activation | --no-refresh staging | Phase 2 | Safer auto-updates |

**Deprecated/outdated:**
- N/A - all patterns are current

## Open Questions

Things that couldn't be fully resolved:

1. **Should daemon schedule be configurable?**
   - What we know: ADV-02 (v2) mentions configurable schedule
   - What's unclear: Add --schedule flag now or defer?
   - Recommendation: Defer to v2. Use fixed "daily" schedule for now.

2. **Should daemon use a dedicated quiet flag?**
   - What we know: Service runs in background, stdout goes to journal
   - What's unclear: Should we add --quiet flag or is journald sufficient?
   - Recommendation: No --quiet flag needed. Journald captures output.

3. **What if updex binary is not at /usr/bin/updex?**
   - What we know: ExecStart uses absolute path
   - What's unclear: Should we detect binary location?
   - Recommendation: Use fixed /usr/bin/updex. Document in install guide.

## Sources

### Primary (HIGH confidence)
- Existing project code: cmd/commands/features.go, cmd/commands/update.go
- Existing project code: internal/systemd/manager.go, runner.go
- Existing project code: cmd/common/common.go
- Existing project patterns from .planning/codebase/CONVENTIONS.md, TESTING.md

### Secondary (MEDIUM confidence)
- Systemd timer/service patterns from Phase 3 research

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Uses existing project dependencies only
- Architecture: HIGH - Exact patterns from existing features.go command
- Pitfalls: HIGH - Based on project experience and Phase 3 lessons
- Code examples: HIGH - Derived directly from existing project code

**Research date:** 2026-01-26
**Valid until:** 2026-02-26 (CLI patterns are stable; systemd infrastructure just completed)
