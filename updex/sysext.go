package updex

import (
	"context"

	"github.com/frostyard/updex/internal/sysext"
)

// Refresh calls systemd-sysext refresh to reload extensions.
func (c *Client) Refresh(ctx context.Context) error {
	c.helper.BeginAction("Refresh sysext")
	defer c.helper.EndAction()

	c.helper.BeginTask("Running systemd-sysext refresh")
	err := sysext.Refresh()
	if err != nil {
		c.helper.Warning(err.Error())
	} else {
		c.helper.Info("Sysext refreshed successfully")
	}
	c.helper.EndTask()

	return err
}

// Merge calls systemd-sysext merge to merge extensions.
func (c *Client) Merge(ctx context.Context) error {
	c.helper.BeginAction("Merge sysext")
	defer c.helper.EndAction()

	c.helper.BeginTask("Running systemd-sysext merge")
	err := sysext.Merge()
	if err != nil {
		c.helper.Warning(err.Error())
	} else {
		c.helper.Info("Sysext merged successfully")
	}
	c.helper.EndTask()

	return err
}

// Unmerge calls systemd-sysext unmerge to unmerge extensions.
func (c *Client) Unmerge(ctx context.Context) error {
	c.helper.BeginAction("Unmerge sysext")
	defer c.helper.EndAction()

	c.helper.BeginTask("Running systemd-sysext unmerge")
	err := sysext.Unmerge()
	if err != nil {
		c.helper.Warning(err.Error())
	} else {
		c.helper.Info("Sysext unmerged successfully")
	}
	c.helper.EndTask()

	return err
}
