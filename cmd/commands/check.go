package commands

import (
	"fmt"
	"os"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
	"github.com/spf13/cobra"
)

// NewCheckCmd creates the check-new command
func NewCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check-new",
		Short: "Check if a newer version is available",
		Long: `Check if a newer version is available for download.

Exit codes:
  0 - A newer version is available
  1 - An error occurred
  2 - No newer version is available`,
		RunE: runCheck,
	}
}

// CheckResult represents the result of a check operation
type CheckResult struct {
	Component       string `json:"component"`
	CurrentVersion  string `json:"current_version,omitempty"`
	NewestVersion   string `json:"newest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

func runCheck(cmd *cobra.Command, args []string) error {
	transfers, err := config.LoadTransfers(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load transfer configs: %w", err)
	}

	if len(transfers) == 0 {
		return fmt.Errorf("no transfer configurations found")
	}

	// Filter by component if specified
	if common.Component != "" {
		filtered := make([]*config.Transfer, 0)
		for _, t := range transfers {
			if t.Component == common.Component {
				filtered = append(filtered, t)
			}
		}
		transfers = filtered
	}

	// Filter by enabled features
	features, err := config.LoadFeatures(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load features: %w", err)
	}
	transfers = config.FilterTransfersByFeatures(transfers, features)

	updateAvailable := false
	var results []CheckResult

	for _, transfer := range transfers {
		available, err := GetAvailableVersions(transfer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get available versions for %s: %v\n", transfer.Component, err)
			continue
		}

		if len(available) == 0 {
			continue
		}

		version.Sort(available)
		newest := available[0] // After sorting, newest is first

		installed, current, err := sysext.GetInstalledVersions(transfer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get installed versions for %s: %v\n", transfer.Component, err)
		}

		result := CheckResult{
			Component:      transfer.Component,
			CurrentVersion: current,
			NewestVersion:  newest,
		}

		// Check if update is available
		if len(installed) == 0 {
			// Nothing installed, update available
			result.UpdateAvailable = true
			updateAvailable = true
		} else if version.Compare(newest, current) > 0 {
			result.UpdateAvailable = true
			updateAvailable = true
		}

		results = append(results, result)
	}

	if common.JSONOutput {
		items := make([]interface{}, len(results))
		for i, r := range results {
			items[i] = r
		}
		common.OutputJSONLines(items)
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
