package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/config"
)

// Components returns all configured components.
func (c *Client) Components(ctx context.Context) ([]ComponentInfo, error) {
	c.helper.BeginAction("List components")
	defer c.helper.EndAction()

	c.helper.BeginTask("Loading configurations")

	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		c.helper.EndTask()
		return nil, fmt.Errorf("failed to load transfer configs: %w", err)
	}

	var components []ComponentInfo

	for _, t := range transfers {
		info := ComponentInfo{
			Name:         t.Component,
			Source:       t.Source.Path,
			SourceType:   t.Source.Type,
			TargetPath:   t.Target.Path,
			InstancesMax: t.Transfer.InstancesMax,
		}
		components = append(components, info)
	}

	c.helper.Info(fmt.Sprintf("Found %d component(s)", len(components)))
	c.helper.EndTask()

	return components, nil
}
