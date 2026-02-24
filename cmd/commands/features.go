package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

// NewFeaturesCmd creates the features command (list only).
func NewFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "features",
		Aliases: []string{"feature", "list"},
		Short:   "List available features and their status",
		Long: `List all features defined in .feature configuration files with their status
and associated transfers.

CONFIGURATION FILES:
  - /etc/sysupdate.d/*.feature
  - /run/sysupdate.d/*.feature
  - /usr/local/lib/sysupdate.d/*.feature
  - /usr/lib/sysupdate.d/*.feature

OUTPUT COLUMNS:
  FEATURE      - Feature name
  DESCRIPTION  - Human-readable description
  ENABLED      - yes/no/masked
  TRANSFERS    - Associated transfer configurations`,
		Example: `  # List all features
  updex features

  # List in JSON format
  updex features --json`,
		RunE: runFeaturesList,
	}

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
