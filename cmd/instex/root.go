package instex

import (
	"github.com/frostyard/updex/cmd/commands"
	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "instex",
	Short: "Discover and install systemd-sysext extensions",
	Long: `instex is a tool for discovering and installing systemd-sysext extensions
from remote repositories.

Use 'discover' to find available extensions, and 'install' to download and
configure them on your system.`,
}

func init() {
	common.RegisterCommonFlags(rootCmd)

	// Register instex subcommands
	rootCmd.AddCommand(commands.NewDiscoverCmd())
	rootCmd.AddCommand(commands.NewInstallCmd())
}

// Execute runs the instex root command
func Execute() error {
	return rootCmd.Execute()
}
