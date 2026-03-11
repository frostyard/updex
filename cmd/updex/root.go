package updex

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	definitions string
	verify      bool
	noRefresh   bool
)

func registerAppFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&definitions, "definitions", "C", "", "Path to directory containing .transfer and .feature files")
	cmd.PersistentFlags().BoolVar(&verify, "verify", false, "Verify GPG signatures on SHA256SUMS")
	cmd.PersistentFlags().BoolVar(&noRefresh, "no-refresh", false, "Skip running systemd-sysext refresh after install/update")
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this operation requires root privileges")
	}
	return nil
}

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

	registerAppFlags(cmd)
	cmd.AddCommand(newFeaturesCmd())
	cmd.AddCommand(newDaemonCmd())

	return cmd
}
