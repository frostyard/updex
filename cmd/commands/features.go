package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	featureDisableRemove bool
	featureDisableNow    bool
	featureDisableForce  bool
	featureDisableDryRun bool
	featureEnableNow     bool
	featureEnableDryRun  bool
	featureEnableRetry   bool
	featureUpdateNoVac   bool
)

// NewFeaturesCmd creates the features command with subcommands
func NewFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "features",
		Aliases: []string{"feature"},
		Short:   "Manage optional features",
		Long: `List, enable, and disable optional features defined in .feature files.

Features are optional sets of transfers that can be enabled or disabled by the
system administrator. When a feature is enabled, its associated transfers will
be considered during updates. When disabled, they are skipped.

CONFIGURATION FILES:
  - /etc/sysupdate.d/*.feature
  - /run/sysupdate.d/*.feature
  - /usr/local/lib/sysupdate.d/*.feature
  - /usr/lib/sysupdate.d/*.feature

SUBCOMMANDS:
  list     Show all features and their status
  enable   Enable a feature (optionally download immediately)
  disable  Disable a feature (optionally remove files)
  update   Download newest versions for all enabled features
  check    Check for available updates across all enabled features`,
		Example: `  # List all features
  updex features list

  # Enable a feature and download its extensions
  sudo updex features enable docker --now

  # Disable a feature and remove its files
  sudo updex features disable docker --now

  # Update all enabled features
  sudo updex features update

  # Check for available updates
  updex features check`,
	}

	cmd.AddCommand(newFeaturesListCmd())
	cmd.AddCommand(newFeaturesEnableCmd())
	cmd.AddCommand(newFeaturesDisableCmd())
	cmd.AddCommand(newFeaturesUpdateCmd())
	cmd.AddCommand(newFeaturesCheckCmd())

	return cmd
}

func newFeaturesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available features",
		Long: `List all features defined in .feature configuration files with their status and associated transfers.

OUTPUT COLUMNS:
  FEATURE      - Feature name
  DESCRIPTION  - Human-readable description
  ENABLED      - yes/no/masked
  TRANSFERS    - Associated transfer configurations`,
		Example: `  # List all features
  updex features list

  # List in JSON format
  updex features list --json`,
		RunE: runFeaturesList,
	}
}

func newFeaturesEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable FEATURE",
		Short: "Enable a feature",
		Long: `Enable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=true for the specified feature.

OPTIONS:
  --now      Immediately download extensions for this feature
  --dry-run  Preview changes without modifying filesystem
  --retry    Retry on network failures (3 attempts)

Requires root privileges.`,
		Example: `  # Enable a feature (downloads on next update)
  sudo updex features enable docker

  # Enable and download immediately
  sudo updex features enable docker --now

  # Preview what would happen
  updex features enable docker --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesEnable,
	}

	cmd.Flags().BoolVar(&featureEnableNow, "now", false, "Immediately download extensions")
	cmd.Flags().BoolVar(&featureEnableDryRun, "dry-run", false, "Preview changes without modifying filesystem")
	cmd.Flags().BoolVar(&featureEnableRetry, "retry", false, "Retry on network failures (3 attempts)")

	return cmd
}

func newFeaturesDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable FEATURE",
		Short: "Disable a feature",
		Long: `Disable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=false for the specified feature.

OPTIONS:
  --now      Immediately unmerge AND remove extension files
  --remove   Remove files (same behavior as --now for backward compat)
  --force    Allow removal of merged extensions (requires reboot)
  --dry-run  Preview changes without modifying filesystem

Requires root privileges.`,
		Example: `  # Disable a feature (stops future updates)
  sudo updex features disable docker

  # Disable and remove files immediately
  sudo updex features disable docker --now

  # Force removal of merged extension
  sudo updex features disable docker --now --force

  # Preview what would be removed
  updex features disable docker --now --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesDisable,
	}

	cmd.Flags().BoolVar(&featureDisableRemove, "remove", false, "Remove downloaded files (same as --now)")
	cmd.Flags().BoolVar(&featureDisableNow, "now", false, "Immediately unmerge and remove extension files")
	cmd.Flags().BoolVar(&featureDisableForce, "force", false, "Allow removal of merged extensions (requires reboot)")
	cmd.Flags().BoolVar(&featureDisableDryRun, "dry-run", false, "Preview changes without modifying filesystem")

	return cmd
}

func runFeaturesList(cmd *cobra.Command, args []string) error {
	client := newClient()

	features, err := client.Features(context.Background())
	if err != nil {
		return err
	}

	if common.JSONOutput {
		common.OutputJSON(features)
		return nil
	}

	if len(features) == 0 {
		fmt.Println("No features configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tDESCRIPTION\tENABLED\tTRANSFERS")
	for _, f := range features {
		status := "no"
		if f.Masked {
			status = "masked"
		} else if f.Enabled {
			status = "yes"
		}

		transfersStr := "-"
		if len(f.Transfers) > 0 {
			transfersStr = ""
			for i, t := range f.Transfers {
				if i > 0 {
					transfersStr += ", "
				}
				transfersStr += t
			}
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Name, f.Description, status, transfersStr)
	}
	_ = w.Flush()

	return nil
}

func runFeaturesEnable(cmd *cobra.Command, args []string) error {
	// Check for root privileges
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.EnableFeatureOptions{
		Now:       featureEnableNow,
		DryRun:    featureEnableDryRun,
		Retry:     featureEnableRetry,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.EnableFeature(context.Background(), args[0], opts)

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' enabled.\n", result.Feature)
				if len(result.DownloadedFiles) > 0 {
					fmt.Printf("Downloaded %d extension(s):\n", len(result.DownloadedFiles))
					for _, f := range result.DownloadedFiles {
						fmt.Printf("  - %s\n", f)
					}
				} else if !featureEnableNow {
					fmt.Printf("Run 'updex features update' to download extensions.\n")
				}
			}
		}
	}

	return err
}

func newFeaturesUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update all enabled features",
		Long: `Download and install newest versions for all enabled features.

Iterates over all enabled features and their associated transfers,
downloading the newest available version for each component.

OPTIONS:
  --no-refresh  Skip running systemd-sysext refresh after update
  --no-vacuum   Skip removing old versions after update

Requires root privileges.`,
		Example: `  # Update all enabled features
  sudo updex features update

  # Update without refreshing sysext
  sudo updex features update --no-refresh

  # Update in JSON format
  sudo updex features update --json`,
		Args: cobra.NoArgs,
		RunE: runFeaturesUpdate,
	}

	cmd.Flags().BoolVar(&featureUpdateNoVac, "no-vacuum", false, "Skip removing old versions after update")

	return cmd
}

func newFeaturesCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check for available updates",
		Long: `Check if newer versions are available for all enabled features.

Iterates over all enabled features and their associated transfers,
comparing installed versions against the newest available versions.

This is a read-only operation that does not download or install anything.`,
		Example: `  # Check for updates
  updex features check

  # Check in JSON format
  updex features check --json`,
		Args: cobra.NoArgs,
		RunE: runFeaturesCheck,
	}
}

func runFeaturesUpdate(cmd *cobra.Command, args []string) error {
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.UpdateFeaturesOptions{
		NoRefresh: common.NoRefresh,
		NoVacuum:  featureUpdateNoVac,
	}

	results, err := client.UpdateFeatures(context.Background(), opts)

	if common.JSONOutput {
		common.OutputJSON(results)
		return err
	}

	if len(results) == 0 {
		fmt.Println("No enabled features with transfers found.")
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tCOMPONENT\tVERSION\tSTATUS")
	for _, fr := range results {
		for _, r := range fr.Results {
			status := "error"
			if r.Error != "" {
				status = r.Error
			} else if r.Downloaded {
				status = "downloaded"
			} else if r.Installed {
				status = "up to date"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", fr.Feature, r.Component, r.Version, status)
		}
	}
	_ = w.Flush()

	return err
}

func runFeaturesCheck(cmd *cobra.Command, args []string) error {
	client := newClient()

	results, err := client.CheckFeatures(context.Background(), updex.CheckFeaturesOptions{})

	if common.JSONOutput {
		common.OutputJSON(results)
		return err
	}

	if len(results) == 0 {
		fmt.Println("No enabled features with transfers found.")
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tCOMPONENT\tCURRENT\tNEWEST\tUPDATE")
	for _, fr := range results {
		for _, r := range fr.Results {
			update := "no"
			if r.UpdateAvailable {
				update = "yes"
			}
			current := r.CurrentVersion
			if current == "" {
				current = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", fr.Feature, r.Component, current, r.NewestVersion, update)
		}
	}
	_ = w.Flush()

	return err
}

func runFeaturesDisable(cmd *cobra.Command, args []string) error {
	// Check for root privileges
	if err := common.RequireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.DisableFeatureOptions{
		Remove:    featureDisableRemove,
		Now:       featureDisableNow,
		Force:     featureDisableForce,
		DryRun:    featureDisableDryRun,
		NoRefresh: common.NoRefresh,
	}

	result, err := client.DisableFeature(context.Background(), args[0], opts)

	if common.JSONOutput {
		common.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' disabled.\n", result.Feature)
				if result.Unmerged {
					fmt.Printf("Extensions unmerged.\n")
				}
				if len(result.RemovedFiles) > 0 {
					fmt.Printf("Removed %d file(s):\n", len(result.RemovedFiles))
					for _, f := range result.RemovedFiles {
						fmt.Printf("  - %s\n", f)
					}
				}
				if featureDisableForce {
					fmt.Printf("Warning: Reboot required for changes to take effect.\n")
				} else if !featureDisableNow && !featureDisableRemove {
					fmt.Printf("Run 'updex features update' to apply changes.\n")
				}
			}
		}
	}

	return err
}
