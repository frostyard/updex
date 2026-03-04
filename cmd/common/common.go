package common

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// App-specific flags (not provided by clix)
var (
	Definitions string
	Verify      bool
	NoRefresh   bool
)

// RegisterAppFlags adds updex-specific persistent flags to the root command.
func RegisterAppFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&Definitions, "definitions", "C", "", "Path to directory containing .transfer and .feature files")
	cmd.PersistentFlags().BoolVar(&Verify, "verify", false, "Verify GPG signatures on SHA256SUMS")
	cmd.PersistentFlags().BoolVar(&NoRefresh, "no-refresh", false, "Skip running systemd-sysext refresh after install/update")
}

// RequireRoot checks if the current process has root privileges
// Returns an error if the process is not running as root (EUID != 0)
func RequireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this operation requires root privileges")
	}
	return nil
}
