package common

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Global flags shared by both updex and instex
var (
	Definitions string
	JSONOutput  bool
	Verify      bool
	Component   string
	NoRefresh   bool
)

// RegisterCommonFlags adds the common flags to a root command
func RegisterCommonFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&Definitions, "definitions", "C", "", "Path to directory containing .transfer files")
	cmd.PersistentFlags().BoolVar(&JSONOutput, "json", false, "Output in JSON format (jq-compatible)")
	cmd.PersistentFlags().BoolVar(&Verify, "verify", false, "Verify GPG signatures on SHA256SUMS")
	cmd.PersistentFlags().StringVar(&Component, "component", "", "Select a specific component to operate on")
	cmd.PersistentFlags().BoolVar(&NoRefresh, "no-refresh", false, "Skip running systemd-sysext refresh after install/update")
}

// OutputJSON prints data as JSON if --json flag is set, otherwise returns false
func OutputJSON(data interface{}) bool {
	if !JSONOutput {
		return false
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
	}
	return true
}

// OutputJSONLines prints each item as a separate JSON line (for streaming)
func OutputJSONLines(items []interface{}) bool {
	if !JSONOutput {
		return false
	}
	enc := json.NewEncoder(os.Stdout)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		}
	}
	return true
}

// RequireRoot checks if the current process has root privileges
// Returns an error if the process is not running as root (EUID != 0)
func RequireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this operation requires root privileges")
	}
	return nil
}
