package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

// NewPendingCmd creates the pending command
func NewPendingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pending",
		Short: "Check if there is a pending update that requires reboot",
		Long: `Check if a newer version has been installed but is not yet active.

This typically happens when a new sysext image has been downloaded but
systemd-sysext has not been refreshed or the system has not been rebooted.

Exit codes:
  0 - A pending update exists
  1 - An error occurred
  2 - No pending update`,
		RunE: runPending,
	}
}

func runPending(cmd *cobra.Command, args []string) error {
	client := newClient()

	opts := updex.PendingOptions{
		Component: common.Component,
	}

	results, err := client.Pending(context.Background(), opts)
	if err != nil {
		return err
	}

	hasPending := false
	for _, r := range results {
		if r.Pending {
			hasPending = true
			break
		}
	}

	if common.JSONOutput {
		common.OutputJSON(results)
	} else {
		for _, r := range results {
			if r.Pending {
				if r.ActiveVersion == "" {
					fmt.Printf("%s: pending activation of %s\n", r.Component, r.InstalledVersion)
				} else {
					fmt.Printf("%s: pending update %s → %s\n", r.Component, r.ActiveVersion, r.InstalledVersion)
				}
			} else {
				fmt.Printf("%s: no pending update (active: %s)\n", r.Component, r.ActiveVersion)
			}
		}
	}

	if !hasPending {
		os.Exit(2)
	}

	return nil
}
