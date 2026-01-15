package instex

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
	Use:   "instex",
	Short: "Discover and install systemd-sysext extensions",
	Long: `instex is a tool for discovering and installing systemd-sysext extensions
from remote repositories.

Use 'discover' to find available extensions, and 'install' to download and
configure them on your system.`,
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

	// Register instex subcommands
	rootCmd.AddCommand(commands.NewDiscoverCmd())
	rootCmd.AddCommand(commands.NewInstallCmd())
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
