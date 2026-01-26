package commands

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

// NewInstallCmd creates the install command
func NewInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install URL",
		Short: "Install an extension from a remote repository",
		Long: `Install an extension from a remote repository.

Downloads the transfer file from the repository and places it in /etc/sysupdate.d/,
then downloads and installs the extension.

REQUIREMENTS:
  - Root privileges (run with sudo)
  - Network access to repository

WORKFLOW:
  1. Fetches extension list from URL/ext/index
  2. Downloads .transfer configuration to /etc/sysupdate.d/
  3. Downloads the extension image to staging directory
  4. Extension becomes active after reboot`,
		Example: `  # Install Docker extension from repository
  updex install https://repo.frostyard.org --component docker

  # Install VSCode extension
  updex install https://repo.frostyard.org --component vscode

  # Install without automatic refresh
  updex install https://repo.example.com --component myext --no-refresh`,
		Args: cobra.ExactArgs(1),
		RunE: runInstall,
	}
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Check for root privileges before attempting any operations
	if err := common.RequireRoot(); err != nil {
		return err
	}

	if common.Component == "" {
		return fmt.Errorf("missing --component flag; specify which extension to install (e.g., --component docker)")
	}

	client := newClient()

	opts := updex.InstallOptions{
		Component: common.Component,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.Install(context.Background(), args[0], opts)

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Installed {
			fmt.Printf("Successfully installed %s version %s\n", result.Component, result.Version)
			fmt.Printf("Reboot required to activate changes.\n")
		}
	}

	return err
}
