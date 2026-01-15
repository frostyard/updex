package updex

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/frostyard/updex/cmd/commands"
	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

var (
	commit  string
	date    string
	builtBy string
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

// SetVersion sets the version for the root command
func SetVersion(version string) {
	rootCmd.Version = version
}

func SetCommit(incoming_commit string) {
	commit = incoming_commit
}

func SetDate(incoming_date string) {
	date = incoming_date
}
func SetBuiltBy(incoming_builtBy string) {
	builtBy = incoming_builtBy
}

func makeVersionString() string {
	return fmt.Sprintf("%s (Commit: %s) (Date: %s) (Built by: %s)", rootCmd.Version, commit, date, builtBy)
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

// Execute runs the root command
func Execute() error {
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(makeVersionString()),
		fang.WithNotifySignal(os.Interrupt, os.Kill),
	); err != nil {
		return err
	}
	return nil
}
