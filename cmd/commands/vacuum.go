package commands

import (
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/spf13/cobra"
)

// NewVacuumCmd creates the vacuum command
func NewVacuumCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vacuum",
		Short: "Remove old versions according to InstancesMax",
		Long: `Remove old versions of sysext images according to the InstancesMax setting
in transfer configurations. Protected versions and the current version are never removed.`,
		RunE: runVacuum,
	}
}

// VacuumResult represents the result of a vacuum operation
type VacuumResult struct {
	Component string   `json:"component"`
	Removed   []string `json:"removed"`
	Kept      []string `json:"kept"`
	Error     string   `json:"error,omitempty"`
}

func runVacuum(cmd *cobra.Command, args []string) error {
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

	var results []VacuumResult

	for _, transfer := range transfers {
		result := VacuumResult{
			Component: transfer.Component,
		}

		removed, kept, err := sysext.VacuumWithDetails(transfer)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Removed = removed
			result.Kept = kept
		}

		results = append(results, result)

		if !common.JSONOutput {
			if len(removed) > 0 {
				fmt.Printf("%s: removed %d version(s)\n", transfer.Component, len(removed))
				for _, v := range removed {
					fmt.Printf("  - %s\n", v)
				}
			} else {
				fmt.Printf("%s: nothing to remove\n", transfer.Component)
			}
		}
	}

	if common.JSONOutput {
		items := make([]interface{}, len(results))
		for i, r := range results {
			items[i] = r
		}
		common.OutputJSONLines(items)
	}

	return nil
}
