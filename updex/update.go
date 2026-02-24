package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/sysupdate"
)

// Update runs systemd-sysupdate to pull latest versions for enabled features.
func (c *Client) Update(ctx context.Context, opts UpdateOptions) ([]UpdateResult, error) {
	c.helper.BeginAction("Update extensions")
	defer c.helper.EndAction()

	// If a specific component is requested, just update that one
	if opts.Component != "" {
		c.helper.BeginTask(fmt.Sprintf("Updating %s", opts.Component))
		result := UpdateResult{Component: opts.Component}

		if err := sysupdate.Update(opts.Component); err != nil {
			result.Error = fmt.Sprintf("systemd-sysupdate failed: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return []UpdateResult{result}, fmt.Errorf("%s", result.Error)
		}

		result.Downloaded = true
		result.Installed = true
		c.helper.Info(fmt.Sprintf("Updated %s", opts.Component))
		c.helper.EndTask()

		if !opts.NoRefresh {
			c.helper.BeginTask("Refreshing sysext")
			if err := sysext.Refresh(); err != nil {
				c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
			}
			c.helper.EndTask()
		}

		result.NextActionMessage = "Reboot required to activate changes"
		return []UpdateResult{result}, nil
	}

	// Update all enabled features' transfers
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load features: %w", err)
	}
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfers: %w", err)
	}
	enabledTransfers := config.FilterTransfersByFeatures(transfers, features)

	if len(enabledTransfers) == 0 {
		c.helper.Info("No enabled transfers found")
		return nil, nil
	}

	var results []UpdateResult
	var hasErrors bool

	for _, transfer := range enabledTransfers {
		c.helper.BeginTask(fmt.Sprintf("Updating %s", transfer.Component))
		result := UpdateResult{Component: transfer.Component}

		if err := sysupdate.Update(transfer.Component); err != nil {
			result.Error = fmt.Sprintf("systemd-sysupdate failed: %v", err)
			c.helper.Warning(result.Error)
			hasErrors = true
		} else {
			result.Downloaded = true
			result.Installed = true
			c.helper.Info(fmt.Sprintf("Updated %s", transfer.Component))
		}

		results = append(results, result)
		c.helper.EndTask()
	}

	if !opts.NoRefresh {
		c.helper.BeginTask("Refreshing sysext")
		if err := sysext.Refresh(); err != nil {
			c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
		}
		c.helper.EndTask()
	}

	if hasErrors {
		return results, fmt.Errorf("one or more components failed to update")
	}

	return results, nil
}
