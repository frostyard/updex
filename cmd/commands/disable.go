package commands

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	disableNow    bool
	disableForce  bool
	disableDryRun bool
)

// NewDisableCmd creates the disable command.
func NewDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable FEATURE",
		Short: "Disable a feature",
		Long: `Disable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=false for the specified feature.

Use --now to immediately unmerge and remove extension files.
Use --force with --now to remove extensions that are currently active
(requires a reboot to take effect).

Requires root privileges.`,
		Example: `  # Disable a feature (stops future updates)
  sudo updex disable docker

  # Disable and remove files immediately
  sudo updex disable docker --now

  # Force removal of active extension
  sudo updex disable docker --now --force

  # Preview what would be removed
  updex disable docker --now --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runDisable,
	}

	cmd.Flags().BoolVar(&disableNow, "now", false, "Immediately unmerge and remove extension files")
	cmd.Flags().BoolVar(&disableForce, "force", false, "Allow removal of active extensions (requires reboot)")
	cmd.Flags().BoolVar(&disableDryRun, "dry-run", false, "Preview changes without modifying filesystem")

	return cmd
}

func runDisable(cmd *cobra.Command, args []string) error {
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.DisableFeatureOptions{
		Now:       disableNow,
		Force:     disableForce,
		DryRun:    disableDryRun,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.DisableFeature(context.Background(), args[0], opts)

	if common.JSONOutput {
		common.OutputJSON(result)
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
				if disableForce {
					fmt.Printf("Warning: Reboot required for changes to take effect.\n")
				} else if !disableNow {
					fmt.Printf("Run 'updex update' to apply changes.\n")
				}
			}
		}
	}

	return err
}
