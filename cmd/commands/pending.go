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

// PendingResult represents the result of a pending check
type PendingResult struct {
	Component        string `json:"component"`
	ActiveVersion    string `json:"active_version,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	Pending          bool   `json:"pending"`
}

func runPending(cmd *cobra.Command, args []string) error {
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

	hasPending := false
	var results []PendingResult

	for _, transfer := range transfers {
		result := PendingResult{
			Component: transfer.Component,
		}

		// Get installed versions (what's on disk)
		installed, _, err := sysext.GetInstalledVersions(transfer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get installed versions for %s: %v\n", transfer.Component, err)
			continue
		}

		if len(installed) == 0 {
			continue
		}

		// Get active version (what systemd-sysext is currently using)
		active, err := sysext.GetActiveVersion(transfer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get active version for %s: %v\n", transfer.Component, err)
		}

		result.ActiveVersion = active

		// Sort installed versions (newest first)
		version.Sort(installed)
		newestInstalled := installed[0]
		result.InstalledVersion = newestInstalled

		// Check if newest installed is newer than active
		if active == "" || version.Compare(newestInstalled, active) > 0 {
			result.Pending = true
			hasPending = true
		}

		results = append(results, result)
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
