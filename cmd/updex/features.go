package updex

import (
	"github.com/spf13/cobra"
)

var (
	featureDisableNow   bool
	featureDisableForce bool
	featureEnableNow    bool
	featureUpdateNoVac  bool
	featureComponent    string

	featureRollbackTo    string
	featureRollbackNow   bool
	featureRollbackClear bool
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

By default, features and transfers are read from the union of the legacy
default directories above and every discovered systemd-sysupdate component
(sysupdate.<name>.d/*, see 'updex components'). Use --component=<name> to
scope an operation to a single named component instead.

SUBCOMMANDS:
  list     Show all features and their status
  enable   Enable a feature (optionally download immediately)
  disable  Disable a feature (optionally remove files)
  update   Download newest versions for all enabled features
  check    Check for available updates across all enabled features
  rollback Link a feature to an older staged version and hold updates`,
		Example: `  # List all features
  updex features list

  # Enable a feature and download its extensions
  sudo updex features enable docker --now

  # Disable a feature and remove its files
  sudo updex features disable docker --now

  # Update all enabled features
  sudo updex features update

  # Scope an operation to a single component
  updex features list --component=docker`,
	}

	cmd.PersistentFlags().StringVar(&featureComponent, "component", "", "Scope the operation to a single named systemd-sysupdate component")

	cmd.AddCommand(newFeaturesListCmd())
	cmd.AddCommand(newFeaturesEnableCmd())
	cmd.AddCommand(newFeaturesDisableCmd())
	cmd.AddCommand(newFeaturesUpdateCmd())
	cmd.AddCommand(newFeaturesCheckCmd())
	cmd.AddCommand(newFeaturesRollbackCmd())

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
  CATALOG      - Where the feature came from (see below)
  TRANSFERS    - Associated transfer configurations
  RUNNING      - Version merged now, per transfer (see below)
  LINKED       - Version the next boot merges, per transfer (see below)

RUNNING and LINKED show one value per transfer, in the order of TRANSFERS,
joined by ", ". "-" means no version (not merged, or not linked), "unknown"
means merged with no recorded version, and "error" means the state could not
be read. A transfer whose LINKED differs from its RUNNING has an update
pending a reboot; a PENDING REBOOT line lists them.

CATALOG VALUES:
  <name>       - Added by 'updex catalog add' from that catalog
  image:<id>   - Shipped in /usr/lib by the OS image
  local:etc    - Written by hand in /etc
  local:usr    - Installed in /usr/local/lib
  local:run    - Generated at runtime in /run
  unknown      - Loaded from a -C/--definitions directory`,
		Example: `  # List all features
  updex features list

  # List in JSON format
  updex features list --json`,
		Args: cobra.NoArgs,
		RunE: runFeaturesList,
	}
}

func newFeaturesEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable FEATURE",
		Short: "Enable a feature",
		Long: `Enable a feature by creating a drop-in configuration file.

This creates a drop-in that sets Enabled=true for the specified feature:
/etc/sysupdate.d/<feature>.feature.d/00-updex.conf for a feature from the
legacy default directory (or a -C/--definitions override), or
/etc/sysupdate.<component>.d/<feature>.feature.d/00-updex.conf for a feature
discovered under a systemd-sysupdate component.

OPTIONS:
  --now      Immediately download extensions for this feature

Use --dry-run (global flag) to preview changes without modifying filesystem.

Requires root privileges.`,
		Example: `  # Enable a feature (downloads on next update)
  sudo updex features enable docker

  # Enable and download immediately
  sudo updex features enable docker --now

  # Preview what would happen
  sudo updex features enable --dry-run docker`,
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

This creates a drop-in that sets Enabled=false for the specified feature:
/etc/sysupdate.d/<feature>.feature.d/00-updex.conf for a feature from the
legacy default directory (or a -C/--definitions override), or
/etc/sysupdate.<component>.d/<feature>.feature.d/00-updex.conf for a feature
discovered under a systemd-sysupdate component.

Without --now, the feature's extensions stay installed and merged; only
future updates stop. Use --now to unmerge and remove them.

OPTIONS:
  --now      Immediately unmerge AND remove extension files
  --force    Allow removal of merged extensions (requires reboot)

Use --dry-run (global flag) to preview changes without modifying filesystem.

Requires root privileges.`,
		Example: `  # Disable a feature (stops future updates; extensions stay installed and merged)
  sudo updex features disable docker

  # Disable and remove files immediately
  sudo updex features disable docker --now

  # Force removal of merged extension
  sudo updex features disable docker --now --force

  # Preview what would be removed
  sudo updex features disable --dry-run docker --now`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesDisable,
	}

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

Unless --no-refresh is set, a warning first lists the other sysexts (those
outside the enabled features, such as a disabled feature) that are linked to a
version the host does not run and that the refresh activates too.

Use --dry-run (global flag) to preview downloads, installs, refreshes, and
vacuum removals without modifying filesystem or sysext state.

Requires root privileges.`,
		Example: `  # Update all enabled features
  sudo updex features update

  # Update without refreshing sysext
  sudo updex features update --no-refresh

  # Preview what would be updated
  sudo updex --dry-run features update

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

OUTPUT COLUMNS:
  CURRENT  - Version the systemd-sysext link names (staged version when
             there is no usable link)
  RUNNING  - Version merged now ("-" when not merged, "unknown" when merged
             with no recorded version)
  LINKED   - Version the next boot merges ("-" when there is no usable link)
  NEWEST   - Newest available version
  UPDATE   - yes, no, or error

A transfer whose LINKED differs from its RUNNING has an update pending a
reboot; a PENDING REBOOT line lists them.

This is a read-only operation that does not download or install anything.`,
		Example: `  # Check for updates
  updex features check

  # Check in JSON format
  updex features check --json`,
		Args: cobra.NoArgs,
		RunE: runFeaturesCheck,
	}
}

func newFeaturesRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback FEATURE",
		Short: "Roll a feature back to an older staged version",
		Long: `Link every transfer of a feature to an older version that is still staged on
disk, and record the version it rolled back from as rejected.

By default each transfer goes to its own newest staged version older than the
version its systemd-sysext link names. With --to, every transfer goes to that
version. If any transfer has no such version on disk, nothing is changed and
the command fails. The links move together: all or none.

The record, /var/lib/updex/rollback/<feature>, keeps one rejected version per
transfer. While it exists, 'updex features update' (and the timer) links
nothing at or below the rejected version and downloads nothing for the
feature; this is not an error. When a newer version is published, update
downloads and links it as usual and deletes the record.

The rollback takes effect at the next boot. With --now, it takes effect
through a systemd-sysext refresh, which also activates other sysexts that are
linked but not merged (a warning lists them).

OPTIONS:
  --to VERSION  Link this staged version instead of the next older one
  --now         Refresh systemd-sysext now (skipped with --no-refresh)
  --clear       Delete the feature's rollback record and change no link

Use --dry-run (global flag) to preview. A dry run needs no root privileges.
Otherwise root privileges are required.`,
		Example: `  # Roll incus back to its previous staged version (applies at next boot)
  sudo updex features rollback incus

  # Roll back to a specific version and apply it now
  sudo updex features rollback incus --to r4 --now

  # Let updates resume without waiting for a newer version
  sudo updex features rollback --clear incus

  # Preview
  updex --dry-run features rollback incus`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesRollback,
	}

	cmd.Flags().StringVar(&featureRollbackTo, "to", "", "Link this staged version instead of the next older one")
	cmd.Flags().BoolVar(&featureRollbackNow, "now", false, "Apply the rollback with a systemd-sysext refresh")
	cmd.Flags().BoolVar(&featureRollbackClear, "clear", false, "Delete the rollback record and change no link")

	return cmd
}
