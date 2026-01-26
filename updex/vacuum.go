package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/sysext"
)

// Vacuum removes old versions according to InstancesMax settings.
// Returns results for all components that were processed.
func (c *Client) Vacuum(ctx context.Context, opts VacuumOptions) ([]VacuumResult, error) {
	c.helper.BeginAction("Vacuum old versions")
	defer c.helper.EndAction()

	transfers, err := c.loadTransfersUnfiltered(opts.Component)
	if err != nil {
		return nil, err
	}

	if len(transfers) == 0 {
		return nil, fmt.Errorf("no transfer configurations found")
	}

	var results []VacuumResult
	var hasErrors bool

	for _, transfer := range transfers {
		c.helper.BeginTask(fmt.Sprintf("Vacuuming %s", transfer.Component))

		result := VacuumResult{
			Component: transfer.Component,
		}

		removed, kept, err := sysext.VacuumWithDetails(transfer)
		if err != nil {
			result.Error = err.Error()
			c.helper.Warning(result.Error)
			hasErrors = true
		} else {
			result.Removed = removed
			result.Kept = kept
			if len(removed) > 0 {
				c.helper.Info(fmt.Sprintf("Removed %d version(s)", len(removed)))
			} else {
				c.helper.Info("Nothing to remove")
			}
		}

		results = append(results, result)
		c.helper.EndTask()
	}

	if hasErrors {
		return results, fmt.Errorf("one or more components failed to vacuum")
	}
	return results, nil
}

// loadTransfersUnfiltered loads transfers without feature filtering.
// Used for operations that should work on all configured transfers.
func (c *Client) loadTransfersUnfiltered(component string) ([]*config.Transfer, error) {
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfer configs: %w", err)
	}

	// Filter by component if specified
	if component != "" {
		filtered := make([]*config.Transfer, 0)
		for _, t := range transfers {
			if t.Component == component {
				filtered = append(filtered, t)
			}
		}
		transfers = filtered
		if len(transfers) == 0 {
			return nil, fmt.Errorf("no transfer configuration found for component: %s", component)
		}
	}

	return transfers, nil
}
