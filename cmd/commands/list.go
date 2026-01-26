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

// NewListCmd creates the list command
func NewListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [VERSION]",
		Short: "List available and installed versions",
		Long: `List all available versions from remote sources and installed versions.

If VERSION is specified, show detailed information about that specific version.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runList,
	}
}

func runList(cmd *cobra.Command, args []string) error {
	client := newClient()

	opts := updex.ListOptions{
		Component: common.Component,
	}

	if len(args) == 1 {
		opts.Version = args[0]
	}

	versions, err := client.List(context.Background(), opts)
	if err != nil {
		return err
	}

	if common.JSONOutput {
		common.OutputJSON(versions)
		return nil
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "VERSION\tINSTALLED\tAVAILABLE\tCURRENT\tCOMPONENT")
	for _, v := range versions {
		installed := "-"
		if v.Installed {
			installed = "yes"
		}
		available := "-"
		if v.Available {
			available = "yes"
		}
		current := ""
		if v.Current {
			current = "→"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.Version, installed, available, current, v.Component)
	}
	_ = w.Flush()

	return nil
}
