package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
)

// Pending checks if there are pending updates that require reboot.
// Returns results for all components, indicating which have pending updates.
func (c *Client) Pending(ctx context.Context, opts PendingOptions) ([]PendingResult, error) {
	c.helper.BeginAction("Check pending updates")
	defer c.helper.EndAction()

	transfers, err := c.loadTransfers(opts.Component)
	if err != nil {
		return nil, err
	}

	if len(transfers) == 0 {
		return nil, fmt.Errorf("no transfer configurations found")
	}

	var results []PendingResult

	for _, transfer := range transfers {
		c.helper.BeginTask(fmt.Sprintf("Checking %s", transfer.Component))

		result := PendingResult{
			Component: transfer.Component,
		}

		// Get installed versions (what's on disk)
		installed, _, err := sysext.GetInstalledVersions(transfer)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to get installed versions: %v", err))
			c.helper.EndTask()
			continue
		}

		if len(installed) == 0 {
			c.helper.EndTask()
			continue
		}

		// Get active version (what systemd-sysext is currently using)
		active, err := sysext.GetActiveVersion(transfer)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to get active version: %v", err))
		}

		result.ActiveVersion = active

		// Sort installed versions (newest first)
		version.Sort(installed)
		newestInstalled := installed[0]
		result.InstalledVersion = newestInstalled

		// Check if newest installed is newer than active
		if active == "" || version.Compare(newestInstalled, active) > 0 {
			result.Pending = true
			if active == "" {
				c.helper.Info(fmt.Sprintf("Pending activation of %s", newestInstalled))
			} else {
				c.helper.Info(fmt.Sprintf("Pending update: %s → %s", active, newestInstalled))
			}
		} else {
			c.helper.Info(fmt.Sprintf("No pending update (active: %s)", active))
		}

		results = append(results, result)
		c.helper.EndTask()
	}

	return results, nil
}
