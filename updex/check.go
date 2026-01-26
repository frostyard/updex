package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
)

// CheckNew checks if newer versions are available for download.
// Returns results for all components, indicating which have updates available.
func (c *Client) CheckNew(ctx context.Context, opts CheckOptions) ([]CheckResult, error) {
	c.helper.BeginAction("Check for updates")
	defer c.helper.EndAction()

	transfers, err := c.loadTransfers(opts.Component)
	if err != nil {
		return nil, err
	}

	if len(transfers) == 0 {
		return nil, fmt.Errorf("no transfer configurations found")
	}

	var results []CheckResult

	for _, transfer := range transfers {
		c.helper.BeginTask(fmt.Sprintf("Checking %s", transfer.Component))

		available, err := c.getAvailableVersions(transfer)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to get available versions: %v", err))
			c.helper.EndTask()
			continue
		}

		if len(available) == 0 {
			c.helper.EndTask()
			continue
		}

		version.Sort(available)
		newest := available[0] // After sorting, newest is first

		installed, current, err := sysext.GetInstalledVersions(transfer)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to get installed versions: %v", err))
		}

		result := CheckResult{
			Component:      transfer.Component,
			CurrentVersion: current,
			NewestVersion:  newest,
		}

		// Check if update is available
		if len(installed) == 0 {
			// Nothing installed, update available
			result.UpdateAvailable = true
			c.helper.Info(fmt.Sprintf("New version available: %s", newest))
		} else if version.Compare(newest, current) > 0 {
			result.UpdateAvailable = true
			c.helper.Info(fmt.Sprintf("Update available: %s → %s", current, newest))
		} else {
			c.helper.Info(fmt.Sprintf("Up to date: %s", current))
		}

		results = append(results, result)
		c.helper.EndTask()
	}

	return results, nil
}
