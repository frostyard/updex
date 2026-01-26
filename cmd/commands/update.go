package commands

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	noVacuum bool
	reboot   bool
)

// NewUpdateCmd creates the update command
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
	// Check for root privileges before attempting any operations
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.UpdateOptions{
		Component: common.Component,
		NoVacuum:  noVacuum,
		NoRefresh: common.NoRefresh,
	}

	if len(args) == 1 {
		opts.Version = args[0]
	}

	results, err := client.Update(context.Background(), opts)

	if common.JSONOutput {
		common.OutputJSON(results)
	} else {
		// Print errors for failed components
		for _, r := range results {
			if r.Error != "" {
				fmt.Printf("%s: %s\n", r.Component, r.Error)
			}
		}

		// Check if any updates were installed and notify about reboot
		anyInstalled := false
		for _, r := range results {
			if r.Downloaded && r.Error == "" {
				anyInstalled = true
				break
			}
		}
		if anyInstalled {
			fmt.Printf("\nReboot required to activate changes.\n")
		}

		// Reboot if requested and updates were installed
		if reboot && anyInstalled && err == nil {
			fmt.Println("\nRebooting system to activate changes...")
			return exec.Command("systemctl", "reboot").Run()
		}
	}

	return err
}
