# cmd/ Directory Cleanup Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate `cmd/commands/` and `cmd/common/` into `cmd/updex/` to reduce unnecessary package complexity.

**Architecture:** Move all CLI code into the `cmd/updex/` package. Split the large `features.go` into command definitions and handler functions. Unexport symbols that no longer need cross-package visibility.

**Tech Stack:** Go 1.26, Cobra, clix

**Spec:** `docs/superpowers/specs/2026-03-11-cmd-cleanup-design.md`

---

## File Structure

After this plan, `cmd/` will contain exactly two packages:

```
cmd/updex-cli/
  main.go              (unchanged)

cmd/updex/
  root.go              root command + flags + requireRoot()
  root_test.go         requireRoot() test
  client.go            newClient() helper
  features.go          command builders + flag vars
  features_run.go      run* handler functions
  daemon.go            daemon commands + handlers
  completion_test.go   shell completion tests
```

Deleted entirely: `cmd/commands/`, `cmd/common/`

---

## Chunk 1: Consolidate packages

### Task 1: Create `client.go` from `components.go`

**Files:**
- Create: `cmd/updex/client.go`
- Source: `cmd/commands/components.go`

- [ ] **Step 1: Create `cmd/updex/client.go`**

```go
package updex

import (
	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	return updex.NewClient(updex.ClientConfig{
		Definitions: definitions,
		Verify:      verify,
		Progress:    clix.NewReporter(),
	})
}
```

- [ ] **Step 2: Verify it compiles in isolation**

Run: `go vet ./cmd/updex/`
Expected: Errors about undefined `definitions` and `verify` — expected since `root.go` hasn't been updated yet. Confirms the file itself is syntactically valid.

---

### Task 2: Merge `common.go` into `root.go`

**Files:**
- Modify: `cmd/updex/root.go`
- Source: `cmd/common/common.go`

- [ ] **Step 1: Rewrite `root.go` to merge in common functionality**

Replace the entire contents of `cmd/updex/root.go` with:

```go
package updex

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	definitions string
	verify      bool
	noRefresh   bool
)

func registerAppFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&definitions, "definitions", "C", "", "Path to directory containing .transfer and .feature files")
	cmd.PersistentFlags().BoolVar(&verify, "verify", false, "Verify GPG signatures on SHA256SUMS")
	cmd.PersistentFlags().BoolVar(&noRefresh, "no-refresh", false, "Skip running systemd-sysext refresh after install/update")
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this operation requires root privileges")
	}
	return nil
}

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

	registerAppFlags(cmd)
	cmd.AddCommand(newFeaturesCmd())
	cmd.AddCommand(newDaemonCmd())

	return cmd
}
```

- [ ] **Step 2: Verify it compiles in isolation**

Run: `go vet ./cmd/updex/`
Expected: Errors about undefined `newFeaturesCmd` and `newDaemonCmd` — expected since those files haven't been moved yet.

---

### Task 3: Create `root_test.go` from `common_test.go`

**Files:**
- Create: `cmd/updex/root_test.go`
- Source: `cmd/common/common_test.go`

- [ ] **Step 1: Create `cmd/updex/root_test.go`**

```go
package updex

import (
	"os"
	"testing"
)

func TestRequireRoot(t *testing.T) {
	err := requireRoot()
	if os.Geteuid() == 0 {
		if err != nil {
			t.Errorf("requireRoot() returned error when running as root: %v", err)
		}
	} else {
		if err == nil {
			t.Error("requireRoot() should return error when not running as root")
		}
		expectedMsg := "this operation requires root privileges"
		if err.Error() != expectedMsg {
			t.Errorf("requireRoot() error = %v, want %v", err.Error(), expectedMsg)
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test -v -run TestRequireRoot ./cmd/updex/`
Expected: PASS (will fail until features/daemon files are moved, but the test itself is correct)

---

### Task 4: Create `features.go` (command builders)

**Files:**
- Create: `cmd/updex/features.go`
- Source: `cmd/commands/features.go` (command builder functions + flag vars)

- [ ] **Step 1: Create `cmd/updex/features.go`**

```go
package updex

import (
	"github.com/spf13/cobra"
)

var (
	featureDisableRemove bool
	featureDisableNow    bool
	featureDisableForce  bool
	featureEnableNow     bool
	featureEnableRetry   bool
	featureUpdateNoVac   bool
)

func newFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "features",
		Aliases: []string{"feature"},
		Short:   "Manage optional features",
		Long: `List, enable, and disable optional features defined in .feature files.

Features are optional sets of transfers that can be enabled or disabled by the
system administrator. When a feature is enabled, its associated transfers will
be considered during updates. When disabled, they are skipped.

CONFIGURATION FILES:
  - /etc/sysupdate.d/*.feature
  - /run/sysupdate.d/*.feature
  - /usr/local/lib/sysupdate.d/*.feature
  - /usr/lib/sysupdate.d/*.feature

SUBCOMMANDS:
  list     Show all features and their status
  enable   Enable a feature (optionally download immediately)
  disable  Disable a feature (optionally remove files)
  update   Download newest versions for all enabled features
  check    Check for available updates across all enabled features`,
		Example: `  # List all features
  updex features list

  # Enable a feature and download its extensions
  sudo updex features enable docker --now

  # Disable a feature and remove its files
  sudo updex features disable docker --now

  # Update all enabled features
  sudo updex features update

  # Check for available updates
  updex features check`,
	}

	cmd.AddCommand(newFeaturesListCmd())
	cmd.AddCommand(newFeaturesEnableCmd())
	cmd.AddCommand(newFeaturesDisableCmd())
	cmd.AddCommand(newFeaturesUpdateCmd())
	cmd.AddCommand(newFeaturesCheckCmd())

	return cmd
}

func newFeaturesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available features",
		Long: `List all features defined in .feature configuration files with their status and associated transfers.

OUTPUT COLUMNS:
  FEATURE      - Feature name
  DESCRIPTION  - Human-readable description
  ENABLED      - yes/no/masked
  TRANSFERS    - Associated transfer configurations`,
		Example: `  # List all features
  updex features list

  # List in JSON format
  updex features list --json`,
		RunE: runFeaturesList,
	}
}

func newFeaturesEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable FEATURE",
		Short: "Enable a feature",
		Long: `Enable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=true for the specified feature.

OPTIONS:
  --now      Immediately download extensions for this feature
  --retry    Retry on network failures (3 attempts)

Use --dry-run (global flag) to preview changes without modifying filesystem.

Requires root privileges.`,
		Example: `  # Enable a feature (downloads on next update)
  sudo updex features enable docker

  # Enable and download immediately
  sudo updex features enable docker --now

  # Preview what would happen
  updex features enable --dry-run docker`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesEnable,
	}

	cmd.Flags().BoolVar(&featureEnableNow, "now", false, "Immediately download extensions")
	cmd.Flags().BoolVar(&featureEnableRetry, "retry", false, "Retry on network failures (3 attempts)")

	return cmd
}

func newFeaturesDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable FEATURE",
		Short: "Disable a feature",
		Long: `Disable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=false for the specified feature.

OPTIONS:
  --now      Immediately unmerge AND remove extension files
  --remove   Remove files (same behavior as --now for backward compat)
  --force    Allow removal of merged extensions (requires reboot)

Use --dry-run (global flag) to preview changes without modifying filesystem.

Requires root privileges.`,
		Example: `  # Disable a feature (stops future updates)
  sudo updex features disable docker

  # Disable and remove files immediately
  sudo updex features disable docker --now

  # Force removal of merged extension
  sudo updex features disable docker --now --force

  # Preview what would be removed
  updex features disable --dry-run docker --now`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesDisable,
	}

	cmd.Flags().BoolVar(&featureDisableRemove, "remove", false, "Remove downloaded files (same as --now)")
	cmd.Flags().BoolVar(&featureDisableNow, "now", false, "Immediately unmerge and remove extension files")
	cmd.Flags().BoolVar(&featureDisableForce, "force", false, "Allow removal of merged extensions (requires reboot)")

	return cmd
}

func newFeaturesUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update all enabled features",
		Long: `Download and install newest versions for all enabled features.

Iterates over all enabled features and their associated transfers,
downloading the newest available version for each component.

OPTIONS:
  --no-refresh  Skip running systemd-sysext refresh after update
  --no-vacuum   Skip removing old versions after update

Requires root privileges.`,
		Example: `  # Update all enabled features
  sudo updex features update

  # Update without refreshing sysext
  sudo updex features update --no-refresh

  # Update in JSON format
  sudo updex features update --json`,
		Args: cobra.NoArgs,
		RunE: runFeaturesUpdate,
	}

	cmd.Flags().BoolVar(&featureUpdateNoVac, "no-vacuum", false, "Skip removing old versions after update")

	return cmd
}

func newFeaturesCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check for available updates",
		Long: `Check if newer versions are available for all enabled features.

Iterates over all enabled features and their associated transfers,
comparing installed versions against the newest available versions.

This is a read-only operation that does not download or install anything.`,
		Example: `  # Check for updates
  updex features check

  # Check in JSON format
  updex features check --json`,
		Args: cobra.NoArgs,
		RunE: runFeaturesCheck,
	}
}
```

---

### Task 5: Create `features_run.go` (handler functions)

**Files:**
- Create: `cmd/updex/features_run.go`
- Source: `cmd/commands/features.go` (run* handler functions)

- [ ] **Step 1: Create `cmd/updex/features_run.go`**

```go
package updex

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

func runFeaturesList(cmd *cobra.Command, args []string) error {
	client := newClient()

	features, err := client.Features(context.Background())
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		clix.OutputJSON(features)
		return nil
	}

	if len(features) == 0 {
		fmt.Println("No features configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tDESCRIPTION\tENABLED\tTRANSFERS")
	for _, f := range features {
		status := "no"
		if f.Masked {
			status = "masked"
		} else if f.Enabled {
			status = "yes"
		}

		transfersStr := "-"
		if len(f.Transfers) > 0 {
			transfersStr = ""
			for i, t := range f.Transfers {
				if i > 0 {
					transfersStr += ", "
				}
				transfersStr += t
			}
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Name, f.Description, status, transfersStr)
	}
	_ = w.Flush()

	return nil
}

func runFeaturesEnable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.EnableFeatureOptions{
		Now:       featureEnableNow,
		DryRun:    clix.DryRun,
		Retry:     featureEnableRetry,
		NoRefresh: noRefresh,
	}

	result, err := client.EnableFeature(context.Background(), args[0], opts)

	if clix.JSONOutput {
		clix.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' enabled.\n", result.Feature)
				if len(result.DownloadedFiles) > 0 {
					fmt.Printf("Downloaded %d extension(s):\n", len(result.DownloadedFiles))
					for _, f := range result.DownloadedFiles {
						fmt.Printf("  - %s\n", f)
					}
				} else if !featureEnableNow {
					fmt.Printf("Run 'updex features update' to download extensions.\n")
				}
			}
		}
	}

	return err
}

func runFeaturesDisable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.DisableFeatureOptions{
		Remove:    featureDisableRemove,
		Now:       featureDisableNow,
		Force:     featureDisableForce,
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
	}

	result, err := client.DisableFeature(context.Background(), args[0], opts)

	if clix.JSONOutput {
		clix.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' disabled.\n", result.Feature)
				if result.Unmerged {
					fmt.Printf("Extensions unmerged.\n")
				}
				if len(result.RemovedFiles) > 0 {
					fmt.Printf("Removed %d file(s):\n", len(result.RemovedFiles))
					for _, f := range result.RemovedFiles {
						fmt.Printf("  - %s\n", f)
					}
				}
				if featureDisableForce {
					fmt.Printf("Warning: Reboot required for changes to take effect.\n")
				} else if !featureDisableNow && !featureDisableRemove {
					fmt.Printf("Run 'updex features update' to apply changes.\n")
				}
			}
		}
	}

	return err
}

func runFeaturesUpdate(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.UpdateFeaturesOptions{
		NoRefresh: noRefresh,
		NoVacuum:  featureUpdateNoVac,
	}

	results, err := client.UpdateFeatures(context.Background(), opts)

	if clix.JSONOutput {
		clix.OutputJSON(results)
		return err
	}

	if len(results) == 0 {
		fmt.Println("No enabled features with transfers found.")
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tCOMPONENT\tVERSION\tSTATUS")
	for _, fr := range results {
		for _, r := range fr.Results {
			status := "error"
			if r.Error != "" {
				status = r.Error
			} else if r.Downloaded {
				status = "downloaded"
			} else if r.Installed {
				status = "up to date"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", fr.Feature, r.Component, r.Version, status)
		}
	}
	_ = w.Flush()

	return err
}

func runFeaturesCheck(cmd *cobra.Command, args []string) error {
	client := newClient()

	results, err := client.CheckFeatures(context.Background(), updex.CheckFeaturesOptions{})

	if clix.JSONOutput {
		clix.OutputJSON(results)
		return err
	}

	if len(results) == 0 {
		fmt.Println("No enabled features with transfers found.")
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tCOMPONENT\tCURRENT\tNEWEST\tUPDATE")
	for _, fr := range results {
		for _, r := range fr.Results {
			update := "no"
			if r.UpdateAvailable {
				update = "yes"
			}
			current := r.CurrentVersion
			if current == "" {
				current = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", fr.Feature, r.Component, current, r.NewestVersion, update)
		}
	}
	_ = w.Flush()

	return err
}
```

---

### Task 6: Move `daemon.go`

**Files:**
- Create: `cmd/updex/daemon.go`
- Source: `cmd/commands/daemon.go`

- [ ] **Step 1: Create `cmd/updex/daemon.go`**

```go
package updex

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/internal/systemd"
	"github.com/spf13/cobra"
)

const unitName = "updex-update"

type daemonStatus struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
	Schedule  string `json:"schedule,omitempty"`
}

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage auto-update daemon",
		Long: `Manage the automatic update timer and service.

The daemon periodically checks for and downloads new extension versions.
Updates are staged but not activated until next reboot.

SUBCOMMANDS:
  enable   Install and start the systemd timer
  disable  Stop and remove the systemd timer
  status   Show current timer state

The timer runs daily by default. Extensions are downloaded but not
activated, allowing safe updates without unexpected system changes.`,
		Example: `  # Enable automatic updates
  sudo updex daemon enable

  # Check if auto-update is running
  updex daemon status

  # Disable automatic updates
  sudo updex daemon disable`,
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
		Long: `Install and enable the systemd timer for automatic updates.

This creates timer and service unit files in /etc/systemd/system/ and
enables the timer to run daily. Updates will download new versions but
not activate them until the next reboot.

WHAT IT DOES:
  1. Creates updex-update.timer and updex-update.service
  2. Enables the timer to start on boot
  3. Starts the timer immediately

Requires root privileges.`,
		Example: `  # Enable automatic updates
  sudo updex daemon enable`,
		Args: cobra.NoArgs,
		RunE: runDaemonEnable,
	}
}

func runDaemonEnable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
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
		ExecStart:   "/usr/bin/updex features update --no-refresh",
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

	if clix.JSONOutput {
		clix.OutputJSON(map[string]any{
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

func newDaemonDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable automatic updates",
		Long: `Stop and remove the systemd timer for automatic updates.

This stops the timer, disables it, and removes both timer and service
unit files from /etc/systemd/system/.

WHAT IT DOES:
  1. Stops the running timer
  2. Disables the timer from starting on boot
  3. Removes the unit files

Requires root privileges.`,
		Example: `  # Disable automatic updates
  sudo updex daemon disable`,
		Args: cobra.NoArgs,
		RunE: runDaemonDisable,
	}
}

func runDaemonDisable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	mgr := systemd.NewManager()

	if !mgr.Exists(unitName) {
		return fmt.Errorf("timer not installed; nothing to disable")
	}

	if err := mgr.Remove(unitName); err != nil {
		return fmt.Errorf("failed to remove timer: %w", err)
	}

	if clix.JSONOutput {
		clix.OutputJSON(map[string]any{
			"success": true,
			"message": "Auto-update daemon disabled",
		})
		return nil
	}

	fmt.Println("Auto-update daemon disabled.")
	fmt.Println("Automatic updates will no longer run.")
	return nil
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Long: `Show the current status of the auto-update daemon.

Displays whether the timer is installed, enabled, and active,
along with the configured schedule.

OUTPUT:
  Installed - Whether unit files exist
  Enabled   - Whether timer starts on boot
  Active    - Whether timer is currently running
  Schedule  - When updates run (e.g., daily)`,
		Example: `  # Check daemon status
  updex daemon status

  # Check status in JSON format
  updex daemon status --json`,
		Args: cobra.NoArgs,
		RunE: runDaemonStatus,
	}
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	mgr := systemd.NewManager()
	runner := &systemd.DefaultSystemctlRunner{}

	status := daemonStatus{
		Installed: mgr.Exists(unitName),
	}

	if status.Installed {
		status.Enabled, _ = runner.IsEnabled(unitName + ".timer")
		status.Active, _ = runner.IsActive(unitName + ".timer")
		status.Schedule = "daily"
	}

	if clix.JSONOutput {
		clix.OutputJSON(status)
		return nil
	}

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

---

### Task 7: Move `completion_test.go`

**Files:**
- Create: `cmd/updex/completion_test.go`
- Source: `cmd/commands/completion_test.go`

- [ ] **Step 1: Create `cmd/updex/completion_test.go`**

```go
package updex

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func createTestRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "updex",
		Short: "Test root command",
	}

	rootCmd.AddCommand(newFeaturesCmd())
	rootCmd.AddCommand(newDaemonCmd())

	return rootCmd
}

func TestCompletionBash(t *testing.T) {
	rootCmd := createTestRootCmd()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"completion", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion bash failed: %v", err)
	}

	output := buf.String()

	tests := []struct {
		name     string
		contains string
	}{
		{"bash header", "bash completion"},
		{"main function", "__updex"},
		{"completion results function", "__updex_get_completion_results"},
		{"shebang", "shell-script"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(output, tt.contains) {
				t.Errorf("bash completion missing %q", tt.contains)
			}
		})
	}

	lines := strings.Count(output, "\n")
	if lines < 100 {
		t.Errorf("bash completion script too short: %d lines", lines)
	}
}

func TestCompletionZsh(t *testing.T) {
	rootCmd := createTestRootCmd()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"completion", "zsh"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion zsh failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "compdef") {
		t.Error("zsh completion missing compdef")
	}
	if !strings.Contains(output, "_updex") {
		t.Error("zsh completion missing _updex function")
	}
}

func TestCompletionFish(t *testing.T) {
	rootCmd := createTestRootCmd()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"completion", "fish"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion fish failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "complete") {
		t.Error("fish completion missing complete command")
	}
	if !strings.Contains(output, "updex") {
		t.Error("fish completion missing updex reference")
	}
}
```

---

### Task 8: Delete old packages, verify, commit

**Files:**
- Delete: `cmd/commands/` (entire directory)
- Delete: `cmd/common/` (entire directory)

- [ ] **Step 1: Delete old directories**

```bash
rm -rf cmd/commands/ cmd/common/
```

- [ ] **Step 2: Run `go vet`**

Run: `go vet ./...`
Expected: No errors

- [ ] **Step 3: Run all tests**

Run: `make check`
Expected: All formatting, linting, and tests pass

- [ ] **Step 4: Verify final structure**

Run: `find cmd/ -type f | sort`
Expected:
```
cmd/updex-cli/main.go
cmd/updex/client.go
cmd/updex/completion_test.go
cmd/updex/daemon.go
cmd/updex/features.go
cmd/updex/features_run.go
cmd/updex/root.go
cmd/updex/root_test.go
```

- [ ] **Step 5: Commit**

```bash
git add -A cmd/
git commit -m "refactor: consolidate cmd packages into cmd/updex"
```
