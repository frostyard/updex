package updex

import (
	"github.com/frostyard/updex/cmd/commands"
	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root cobra command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "updex",
		Short: "Manage systemd-sysext extensions through features",
		Long: `updex manages systemd-sysext extensions through a feature-based interface.

Features group related sysext transfers that can be enabled, disabled,
updated, and checked together. Use 'updex features' to manage them.

Configuration is read from .feature and .transfer files in:
  - /etc/sysupdate.d/
  - /run/sysupdate.d/
  - /usr/local/lib/sysupdate.d/
  - /usr/lib/sysupdate.d/`,
	}

	common.RegisterAppFlags(cmd)
	cmd.AddCommand(commands.NewFeaturesCmd())
	cmd.AddCommand(commands.NewDaemonCmd())

	return cmd
}
