package updex

import (
	"github.com/spf13/cobra"
)

var (
	featureDisableRemove bool
	featureDisableNow    bool
	featureDisableForce  bool
	featureEnableNow     bool
	featureUpdateNoVac   bool
)

func newFeaturesCmd() *cobra.Command {
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

Use --dry-run (global flag) to preview changes without modifying filesystem.

Requires root privileges.`,
		Example: `  # Enable a feature (downloads on next update)
  sudo updex features enable docker

  # Enable and download immediately
  sudo updex features enable docker --now

  # Preview what would happen
  updex features enable --dry-run docker`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesEnable,
	}

	cmd.Flags().BoolVar(&featureEnableNow, "now", false, "Immediately download extensions")

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

Use --dry-run (global flag) to preview changes without modifying filesystem.

Requires root privileges.`,
		Example: `  # Disable a feature (stops future updates)
  sudo updex features disable docker

  # Disable and remove files immediately
  sudo updex features disable docker --now

  # Force removal of merged extension
  sudo updex features disable docker --now --force

  # Preview what would be removed
  updex features disable --dry-run docker --now`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesDisable,
	}

	cmd.Flags().BoolVar(&featureDisableRemove, "remove", false, "Remove downloaded files (same as --now)")
	cmd.Flags().BoolVar(&featureDisableNow, "now", false, "Immediately unmerge and remove extension files")
	cmd.Flags().BoolVar(&featureDisableForce, "force", false, "Allow removal of merged extensions (requires reboot)")

	return cmd
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
