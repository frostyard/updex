package commands

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	removeNow bool
)

// NewRemoveCmd creates the remove command
func NewRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an extension",
		Long: `Remove an extension by deleting its files and symlinks.

Removes extension files from the staging directory (e.g., /var/lib/extensions.d/)
and the symlink from /var/lib/extensions.

Files can be deleted while the system is merged, and things will keep working
until the next reboot. Use --now to unmerge immediately.

Requires --component flag to specify which extension to remove.
Requires root privileges.

Examples:
  updex remove --component docker
  updex remove --component vscode --now`,
		RunE: runRemove,
	}

	cmd.Flags().BoolVar(&removeNow, "now", false, "Unmerge the extension immediately")

	return cmd
}

func runRemove(cmd *cobra.Command, args []string) error {
	// Check for required flag
	if common.Component == "" {
		return fmt.Errorf("missing --component flag; specify which extension to remove (e.g., --component docker)")
	}

	// Check for root privileges
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.RemoveOptions{
		Now:       removeNow,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.Remove(context.Background(), common.Component, opts)

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if len(result.RemovedFiles) == 0 {
				fmt.Printf("No files found for component '%s'\n", common.Component)
			} else {
				fmt.Printf("Successfully removed %d file(s) for component '%s'\n", len(result.RemovedFiles), common.Component)
				if removeNow {
					fmt.Printf("Extension unmerged immediately.\n")
				} else {
					fmt.Printf("Changes will take effect after reboot.\n")
				}
			}
		}
	}

	return err
}
