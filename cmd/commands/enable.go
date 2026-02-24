package commands

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	enableNow    bool
	enableDryRun bool
)

// NewEnableCmd creates the enable command.
func NewEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable FEATURE",
		Short: "Enable a feature",
		Long: `Enable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=true. Use --now to also download extensions immediately
via systemd-sysupdate and refresh systemd-sysext.

Requires root privileges.`,
		Example: `  # Enable a feature (downloads on next update)
  sudo updex enable docker

  # Enable and download immediately
  sudo updex enable docker --now

  # Preview what would happen
  updex enable docker --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runEnable,
	}

	cmd.Flags().BoolVar(&enableNow, "now", false, "Immediately download extensions via systemd-sysupdate")
	cmd.Flags().BoolVar(&enableDryRun, "dry-run", false, "Preview changes without modifying filesystem")

	return cmd
}

func runEnable(cmd *cobra.Command, args []string) error {
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.EnableFeatureOptions{
		Now:       enableNow,
		DryRun:    enableDryRun,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.EnableFeature(context.Background(), args[0], opts)

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' enabled.\n", result.Feature)
				if len(result.DownloadedFiles) > 0 {
					fmt.Printf("Updated %d extension(s):\n", len(result.DownloadedFiles))
					for _, f := range result.DownloadedFiles {
						fmt.Printf("  - %s\n", f)
					}
				} else if !enableNow {
					fmt.Printf("Run 'updex update' to download extensions.\n")
				}
			}
		}
	}

	return err
}
