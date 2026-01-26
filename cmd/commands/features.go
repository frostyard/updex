package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	featureDisableRemove bool
	featureDisableNow    bool
)

// NewFeaturesCmd creates the features command with subcommands
func NewFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "features",
		Aliases: []string{"feature"},
		Short:   "Manage optional features",
		Long: `List, enable, and disable optional features defined in .feature files.

Features are optional sets of transfers that can be enabled or disabled by the
system administrator. When a feature is enabled, its associated transfers will
be considered during updates. When disabled, they are skipped.

Configuration files are read from:
  - /etc/sysupdate.d/*.feature
  - /run/sysupdate.d/*.feature
  - /usr/local/lib/sysupdate.d/*.feature
  - /usr/lib/sysupdate.d/*.feature`,
	}

	cmd.AddCommand(newFeaturesListCmd())
	cmd.AddCommand(newFeaturesEnableCmd())
	cmd.AddCommand(newFeaturesDisableCmd())

	return cmd
}

func newFeaturesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available features",
		Long:  `List all features defined in .feature configuration files with their status and associated transfers.`,
		RunE:  runFeaturesList,
	}
}

func newFeaturesEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable FEATURE",
		Short: "Enable a feature",
		Long: `Enable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=true for the specified feature.

Requires root privileges.`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesEnable,
	}
}

func newFeaturesDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable FEATURE",
		Short: "Disable a feature",
		Long: `Disable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=false for the specified feature.

With --remove flag, also removes all downloaded files for transfers in this feature.
With --now flag, unmerges extensions immediately.

Requires root privileges.`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesDisable,
	}

	cmd.Flags().BoolVar(&featureDisableRemove, "remove", false, "Remove downloaded files for this feature's transfers")
	cmd.Flags().BoolVar(&featureDisableNow, "now", false, "Unmerge extensions immediately")

	return cmd
}

func runFeaturesList(cmd *cobra.Command, args []string) error {
	client := newClient()

	features, err := client.Features(context.Background())
	if err != nil {
		return err
	}

	if common.JSONOutput {
		common.OutputJSON(features)
		return nil
	}

	if len(features) == 0 {
		fmt.Println("No features configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tDESCRIPTION\tENABLED\tTRANSFERS")
	for _, f := range features {
		status := "no"
		if f.Masked {
			status = "masked"
		} else if f.Enabled {
			status = "yes"
		}

		transfersStr := "-"
		if len(f.Transfers) > 0 {
			transfersStr = ""
			for i, t := range f.Transfers {
				if i > 0 {
					transfersStr += ", "
				}
				transfersStr += t
			}
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Name, f.Description, status, transfersStr)
	}
	_ = w.Flush()

	return nil
}

func runFeaturesEnable(cmd *cobra.Command, args []string) error {
	// Check for root privileges
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	result, err := client.EnableFeature(context.Background(), args[0])

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			fmt.Printf("Feature '%s' enabled.\n", result.Feature)
			fmt.Printf("Run 'updex update' to apply changes.\n")
		}
	}

	return err
}

func runFeaturesDisable(cmd *cobra.Command, args []string) error {
	// Check for root privileges
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.DisableFeatureOptions{
		Remove:    featureDisableRemove,
		Now:       featureDisableNow,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.DisableFeature(context.Background(), args[0], opts)

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			fmt.Printf("Feature '%s' disabled.\n", result.Feature)
			if featureDisableRemove {
				fmt.Printf("Removed %d file(s).\n", len(result.RemovedFiles))
			}
			if featureDisableNow {
				fmt.Printf("Extensions unmerged immediately.\n")
			} else if !featureDisableRemove {
				fmt.Printf("Run 'updex update' to apply changes.\n")
			}
		}
	}

	return err
}
