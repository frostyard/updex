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

	rootCmd.AddCommand(commands.NewFeaturesCmd())
	rootCmd.AddCommand(commands.NewDaemonCmd())
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
