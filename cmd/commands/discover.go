package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

// NewDiscoverCmd creates the discover command
func NewDiscoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover URL",
		Short: "Discover available extensions from a remote repository",
		Long: `Discover available extensions from a remote repository.

Downloads the index file from {URL}/ext/index to get a list of available
extensions, then fetches SHA256SUMS for each extension to list available versions.

Use this command to explore what extensions are available before installing.

WORKFLOW:
  1. Fetches {URL}/ext/index for extension list
  2. For each extension, fetches SHA256SUMS
  3. Displays available extensions and their versions`,
		Example: `  # Discover extensions from Frostyard repository
  updex discover https://repo.frostyard.org

  # Discover extensions from a custom repository
  updex discover https://example.com/sysext

  # Output as JSON for scripting
  updex discover https://repo.example.com --json`,
		Args: cobra.ExactArgs(1),
		RunE: runDiscover,
	}
}

func runDiscover(cmd *cobra.Command, args []string) error {
	client := newClient()

	result, err := client.Discover(context.Background(), args[0])
	if err != nil {
		return err
	}

	if common.JSONOutput {
		common.OutputJSON(result)
		return nil
	}

	if len(result.Extensions) == 0 {
		fmt.Println("No extensions found in repository.")
		return nil
	}

	// Tabular output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "EXTENSION\tVERSIONS\n")
	for _, ext := range result.Extensions {
		if ext.Error != "" {
			_, _ = fmt.Fprintf(w, "%s\t(error: %s)\n", ext.Name, ext.Error)
		} else if len(ext.Versions) == 0 {
			_, _ = fmt.Fprintf(w, "%s\t(no versions)\n", ext.Name)
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", ext.Name, strings.Join(ext.Versions, ", "))
		}
	}
	_ = w.Flush()

	return nil
}
