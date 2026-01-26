package commands

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
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

func runVacuum(cmd *cobra.Command, args []string) error {
	// Check for root privileges before attempting any operations
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.VacuumOptions{
		Component: common.Component,
	}

	results, err := client.Vacuum(context.Background(), opts)

	if common.JSONOutput {
		common.OutputJSON(results)
	} else {
		for _, result := range results {
			if result.Error != "" {
				fmt.Printf("%s: error: %s\n", result.Component, result.Error)
			} else if len(result.Removed) > 0 {
				fmt.Printf("%s: removed %d version(s)\n", result.Component, len(result.Removed))
				for _, v := range result.Removed {
					fmt.Printf("  - %s\n", v)
				}
			} else {
				fmt.Printf("%s: nothing to remove\n", result.Component)
			}
		}
	}

	return err
}
