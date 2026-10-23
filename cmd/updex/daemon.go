package updex

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/updex"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage auto-update daemon",
		Long: `Manage the automatic update timer and service.

The daemon periodically checks for and downloads new extension versions.
Updates are linked but not activated until the next reboot.

SUBCOMMANDS:
  enable   Enable and start the systemd timer
  disable  Stop and disable the systemd timer
  status   Show timer state, next run, and last result

The frostyard-updex package ships updex-update.timer and
updex-update.service in /usr/lib/systemd/system, disabled. The timer runs
every 6 hours (with up to an hour of random delay) while the host is on AC
power. Extensions are downloaded but not activated, allowing safe updates
without unexpected system changes.

To keep a host from updating at all, mask the timer; the mask survives
image updates:
  sudo systemctl mask updex-update.timer`,
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
		Long: `Enable the systemd timer for automatic updates.

When the package ships updex-update.timer (in /usr/lib/systemd/system), this
enables and starts it and writes nothing. When it does not (updex installed
without the package), this writes updex-update.timer and
updex-update.service to /etc/systemd/system/ first. Updates download new
versions but do not activate them until the next reboot.

WHAT IT DOES:
  1. Writes the unit files to /etc/systemd/system/, only if the package
     does not ship them
  2. Enables the timer to start on boot
  3. Starts the timer immediately

With --dry-run, the command prints the unit files and actions and changes
nothing.

Requires root privileges.`,
		Example: `  # Enable automatic updates
  sudo updex daemon enable

  # Preview the timer install without changing anything
  sudo updex --dry-run daemon enable`,
		Args: cobra.NoArgs,
		RunE: runDaemonEnable,
	}
}

func runDaemonEnable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	result, err := newClient().EnableDaemon(cmd.Context(), updex.EnableDaemonOptions{
		DryRun: clix.DryRun,
	})
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		_, err := clix.OutputJSON(result)
		return err
	}

	if result.DryRun {
		fmt.Printf("[DRY RUN] %s\n", result.Message)
		return nil
	}

	fmt.Println(result.Message + ".")
	if result.Packaged {
		fmt.Println("Using the units shipped by the package; no unit file was written.")
	}
	fmt.Println("Updates run every 6 hours while on AC power and download new versions.")
	fmt.Println("Reboot required to activate downloaded extensions.")
	return nil
}

func newDaemonDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable automatic updates",
		Long: `Stop and disable the systemd timer for automatic updates.

This stops the timer, disables it, and removes the unit files that updex
wrote to /etc/systemd/system/ (the ones with its marker, and the units that
older updex releases wrote). It keeps any other file there, and it never
removes the units that the package ships in /usr/lib/systemd/system.

To keep the timer off across image updates, mask it instead:
  sudo systemctl mask updex-update.timer

WHAT IT DOES:
  1. Stops the running timer
  2. Disables the timer from starting on boot
  3. Removes the unit files that updex wrote

With --dry-run, the command prints the unit files and actions and changes
nothing.

Requires root privileges.`,
		Example: `  # Disable automatic updates
  sudo updex daemon disable

  # Preview stopping and removing the timer without changing anything
  sudo updex --dry-run daemon disable`,
		Args: cobra.NoArgs,
		RunE: runDaemonDisable,
	}
}

func runDaemonDisable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	result, err := newClient().DisableDaemon(cmd.Context(), updex.DisableDaemonOptions{
		DryRun: clix.DryRun,
	})
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		_, err := clix.OutputJSON(result)
		return err
	}

	if result.DryRun {
		fmt.Printf("[DRY RUN] %s\n", result.Message)
		return nil
	}

	fmt.Println(result.Message + ".")
	fmt.Println("Automatic updates will no longer run.")
	if result.Packaged {
		fmt.Println("The package keeps its units in /usr/lib/systemd/system; to keep the timer off")
		fmt.Println("across image updates, run: sudo systemctl mask updex-update.timer")
	}
	return nil
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Long: `Show the current status of the auto-update daemon.

Displays whether the timer is installed, enabled, and active, where its unit
files come from, its schedule, the next run, and the last run.

OUTPUT:
  Installed  - Whether a timer unit file exists
  Enabled    - Whether timer starts on boot
  Active     - Whether timer is currently running
  Source     - "packaged" (/usr/lib/systemd/system) or "etc"
               (/etc/systemd/system, which overrides a packaged unit)
  Unit files - The unit files in use
  Schedule   - When updates run (the timer's OnCalendar)
  Next run   - When the timer fires next
  Last run   - When the service last finished, and its result

The run log is in the journal: journalctl -u updex-update.service`,
		Example: `  # Check daemon status
  updex daemon status

  # Check status in JSON format
  updex daemon status --json`,
		Args: cobra.NoArgs,
		RunE: runDaemonStatus,
	}
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	status, err := newClient().DaemonStatus(cmd.Context(), updex.DaemonStatusOptions{})
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		_, err := clix.OutputJSON(status)
		return err
	}

	if !status.Installed {
		fmt.Println("Auto-update daemon: not installed")
		fmt.Println("Run 'updex daemon enable' to enable automatic updates.")
		return nil
	}

	fmt.Println("Auto-update daemon: installed")
	fmt.Printf("  Enabled: %v\n", status.Enabled)
	fmt.Printf("  Active: %v\n", status.Active)
	fmt.Printf("  Source: %s\n", status.Source)
	for _, path := range status.UnitFiles {
		fmt.Printf("  Unit file: %s\n", path)
	}
	fmt.Printf("  Schedule: %s\n", orDash(status.Schedule))
	if status.NextRun != "" {
		fmt.Printf("  Next run: %s\n", status.NextRun)
	} else {
		fmt.Println("  Next run: none scheduled")
	}
	if status.LastRun != "" {
		fmt.Printf("  Last run: %s (%s)\n", status.LastRun, orDash(status.LastResult))
	} else {
		fmt.Println("  Last run: none recorded since boot")
	}
	return nil
}

// orDash returns value, or "-" when it is empty.
func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
