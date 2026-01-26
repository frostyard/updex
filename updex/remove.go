package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/sysext"
)

// Remove removes an extension by deleting its files and symlinks.
func (c *Client) Remove(ctx context.Context, component string, opts RemoveOptions) (*RemoveResult, error) {
	c.helper.BeginAction("Remove extension")
	defer c.helper.EndAction()

	result := &RemoveResult{
		Component: component,
	}

	if component == "" {
		result.Error = "component name is required"
		return result, fmt.Errorf("%s", result.Error)
	}

	c.helper.BeginTask(fmt.Sprintf("Removing %s", component))

	// Load transfer configuration
	transfers, err := c.loadTransfersUnfiltered(component)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load transfer configs: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	if len(transfers) == 0 {
		result.Error = fmt.Sprintf("component '%s' not found in configuration", component)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	transfer := transfers[0]

	// Unmerge first if --now is specified
	if opts.Now {
		c.helper.Info("Unmerging extensions")
		if err := sysext.Unmerge(); err != nil {
			result.Error = fmt.Sprintf("failed to unmerge: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", result.Error)
		}
		result.Unmerged = true
	}

	// Remove the symlink from /var/lib/extensions
	c.helper.Info(fmt.Sprintf("Removing symlink from %s", sysext.SysextDir))
	if err := sysext.UnlinkFromSysext(transfer); err != nil {
		c.helper.Warning(fmt.Sprintf("failed to remove symlink: %v", err))
	} else {
		result.RemovedSymlink = true
	}

	// Remove all versions of the component
	c.helper.Info("Removing files")
	removed, err := sysext.RemoveAllVersions(transfer)
	if err != nil {
		result.Error = fmt.Sprintf("failed to remove files: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	result.RemovedFiles = removed
	result.Success = true

	if len(removed) == 0 {
		result.NextActionMessage = "No files found to remove"
		c.helper.Info("No files found to remove")
		c.helper.EndTask()
		return result, nil
	}

	c.helper.Info(fmt.Sprintf("Removed %d file(s)", len(removed)))

	// Refresh systemd-sysext if we unmerged (unless --no-refresh)
	if opts.Now && !opts.NoRefresh {
		c.helper.Info("Refreshing sysext")
		if err := sysext.Refresh(); err != nil {
			c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
		}
	}

	if opts.Now {
		result.NextActionMessage = "Extension removed and unmerged"
	} else {
		result.NextActionMessage = "Extension removed. Changes will take effect after reboot."
	}

	c.helper.EndTask()

	return result, nil
}
