package commands

import (
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/systemd"
	"github.com/spf13/cobra"
)

const unitName = "updex-update"

// DaemonStatus represents the current state of the auto-update daemon
type DaemonStatus struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
	Schedule  string `json:"schedule,omitempty"`
}

// NewDaemonCmd creates the daemon command with subcommands
func NewDaemonCmd() *cobra.Command {
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
	if err := common.RequireRoot(); err != nil {
		return err
	}

	mgr := systemd.NewManager()

	if !mgr.Exists(unitName) {
		return fmt.Errorf("timer not installed; nothing to disable")
	}

	if err := mgr.Remove(unitName); err != nil {
		return fmt.Errorf("failed to remove timer: %w", err)
	}

	if common.JSONOutput {
		common.OutputJSON(map[string]interface{}{
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

	status := DaemonStatus{
		Installed: mgr.Exists(unitName),
	}

	if status.Installed {
		status.Enabled, _ = runner.IsEnabled(unitName + ".timer")
		status.Active, _ = runner.IsActive(unitName + ".timer")
		status.Schedule = "daily"
	}

	if common.JSONOutput {
		common.OutputJSON(status)
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
