package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

// NewCheckCmd creates the check-new command
func NewCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check-new",
		Short: "Check if a newer version is available",
		Long: `Check if a newer version is available for download.

Compares installed versions against available versions from configured
repositories. Useful for scripting update notifications.

EXIT CODES:
  0 - A newer version is available
  1 - An error occurred
  2 - No newer version is available (already up to date)`,
		Example: `  # Check all components for updates
  updex check-new

  # Check a specific component
  updex check-new --component docker

  # Use in a script
  if updex check-new --component docker; then
    echo "Update available!"
  fi`,
		RunE: runCheck,
	}
}

func runCheck(cmd *cobra.Command, args []string) error {
	client := newClient()

	opts := updex.CheckOptions{
		Component: common.Component,
	}

	results, err := client.CheckNew(context.Background(), opts)
	if err != nil {
		return err
	}

	updateAvailable := false
	for _, r := range results {
		if r.UpdateAvailable {
			updateAvailable = true
			break
		}
	}

	if common.JSONOutput {
		common.OutputJSON(results)
	} else {
		for _, r := range results {
			if r.UpdateAvailable {
				if r.CurrentVersion == "" {
					fmt.Printf("%s: new version available: %s\n", r.Component, r.NewestVersion)
				} else {
					fmt.Printf("%s: update available: %s → %s\n", r.Component, r.CurrentVersion, r.NewestVersion)
				}
			} else {
				fmt.Printf("%s: up to date (%s)\n", r.Component, r.CurrentVersion)
			}
		}
	}

	if !updateAvailable {
		os.Exit(2)
	}

	return nil
}
