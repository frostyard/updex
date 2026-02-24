package commands

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var reboot bool

// NewUpdateCmd creates the update command.
func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update extensions via systemd-sysupdate",
		Long: `Run systemd-sysupdate to pull latest versions for all enabled features,
or for a specific component if --component is specified.

After a successful update, a reboot is required to activate changes.
Use --reboot to automatically reboot after updating.

REQUIREMENTS:
  - Root privileges (run with sudo)`,
		Example: `  # Update all enabled extensions
  sudo updex update

  # Update only a specific component
  sudo updex update --component docker

  # Update and reboot to activate changes
  sudo updex update --reboot`,
		RunE: runUpdate,
	}
	cmd.Flags().BoolVar(&reboot, "reboot", false, "Reboot system after successful update")
	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.UpdateOptions{
		Component: common.Component,
		NoRefresh: common.NoRefresh,
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
