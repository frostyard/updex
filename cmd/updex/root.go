package updex

import (
	"github.com/frostyard/updex/cmd/commands"
	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "updex",
	Short: "Manage systemd-sysext images",
	Long: `updex is a tool for managing systemd-sysext images.

It replicates the functionality of systemd-sysupdate for url-file transfers,
allowing you to download, verify, and manage sysext images from remote sources.

Configuration is read from .transfer files in:
  - /etc/sysupdate.d/*.transfer
  - /run/sysupdate.d/*.transfer
  - /usr/local/lib/sysupdate.d/*.transfer
  - /usr/lib/sysupdate.d/*.transfer`,
}

func init() {
	common.RegisterCommonFlags(rootCmd)

	// Register all updex subcommands
	rootCmd.AddCommand(commands.NewListCmd())
	rootCmd.AddCommand(commands.NewCheckCmd())
	rootCmd.AddCommand(commands.NewUpdateCmd())
	rootCmd.AddCommand(commands.NewVacuumCmd())
	rootCmd.AddCommand(commands.NewPendingCmd())
	rootCmd.AddCommand(commands.NewComponentsCmd())
}

// Execute runs the updex root command
func Execute() error {
	return rootCmd.Execute()
}
