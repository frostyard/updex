package commands

import (
	"fmt"
	"os"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/sysext"
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

// RemoveResult represents the result of a remove operation
type RemoveResult struct {
	Component         string   `json:"component"`
	RemovedFiles      []string `json:"removed_files,omitempty"`
	RemovedSymlink    bool     `json:"removed_symlink"`
	Unmerged          bool     `json:"unmerged"`
	Success           bool     `json:"success"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
}

func runRemove(cmd *cobra.Command, args []string) error {
	result := RemoveResult{
		Component: common.Component,
	}

	// Check for required flag
	if common.Component == "" {
		result.Error = "required flag --component is missing"
		if common.JSONOutput {
			common.OutputJSON(result)
		}
		return fmt.Errorf("%s", result.Error)
	}

	// Check for root privileges
	if err := common.RequireRoot(); err != nil {
		result.Error = err.Error()
		if common.JSONOutput {
			common.OutputJSON(result)
		}
		return err
	}

	// Load transfer configuration
	transfers, err := config.LoadTransfers(common.Definitions)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load transfer configs: %v", err)
		if common.JSONOutput {
			common.OutputJSON(result)
		}
		return fmt.Errorf("%s", result.Error)
	}

	// Find the transfer for this component
	var transfer *config.Transfer
	for _, t := range transfers {
		if t.Component == common.Component {
			transfer = t
			break
		}
	}

	if transfer == nil {
		result.Error = fmt.Sprintf("component '%s' not found in configuration", common.Component)
		if common.JSONOutput {
			common.OutputJSON(result)
		}
		return fmt.Errorf("%s", result.Error)
	}

	// Unmerge first if --now is specified
	if removeNow {
		if !common.JSONOutput {
			fmt.Printf("Unmerging extensions...\n")
		}
		if err := sysext.Unmerge(); err != nil {
			result.Error = fmt.Sprintf("failed to unmerge: %v", err)
			if common.JSONOutput {
				common.OutputJSON(result)
			}
			return fmt.Errorf("%s", result.Error)
		}
		result.Unmerged = true
	}

	// Remove the symlink from /var/lib/extensions
	if !common.JSONOutput {
		fmt.Printf("Removing symlink from %s...\n", sysext.SysextDir)
	}
	if err := sysext.UnlinkFromSysext(transfer); err != nil {
		// Not a fatal error if symlink doesn't exist
		if !common.JSONOutput {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	} else {
		result.RemovedSymlink = true
	}

	// Remove all versions of the component
	if !common.JSONOutput {
		fmt.Printf("Removing files for component '%s'...\n", common.Component)
	}
	removed, err := sysext.RemoveAllVersions(transfer)
	if err != nil {
		result.Error = fmt.Sprintf("failed to remove files: %v", err)
		if common.JSONOutput {
			common.OutputJSON(result)
		}
		return fmt.Errorf("%s", result.Error)
	}

	result.RemovedFiles = removed
	result.Success = true

	if len(removed) == 0 {
		result.NextActionMessage = "No files found to remove"
		if common.JSONOutput {
			common.OutputJSON(result)
		} else {
			fmt.Printf("No files found for component '%s'\n", common.Component)
		}
		return nil
	}

	// Refresh systemd-sysext if we unmerged (unless --no-refresh)
	if removeNow && !common.NoRefresh {
		if !common.JSONOutput {
			fmt.Printf("Refreshing systemd-sysext...\n")
		}
		if err := sysext.Refresh(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: sysext refresh failed: %v\n", err)
		}
	}

	if removeNow {
		result.NextActionMessage = "Extension removed and unmerged"
	} else {
		result.NextActionMessage = "Extension removed. Changes will take effect after reboot."
	}

	if common.JSONOutput {
		common.OutputJSON(result)
	} else {
		fmt.Printf("Successfully removed %d file(s) for component '%s'\n", len(removed), common.Component)
		if removeNow {
			fmt.Printf("Extension unmerged immediately.\n")
		} else {
			fmt.Printf("Changes will take effect after reboot.\n")
		}
	}

	return nil
}
