package updex

import "github.com/frostyard/updex/v2/manifest"

// EnableDaemonOptions configures the EnableDaemon operation.
type EnableDaemonOptions struct {
	// DryRun previews changes without modifying filesystem.
	DryRun bool
}

// DisableDaemonOptions configures the DisableDaemon operation.
type DisableDaemonOptions struct {
	// DryRun previews changes without modifying filesystem.
	DryRun bool
}

// DaemonStatusOptions configures the DaemonStatus operation.
type DaemonStatusOptions struct{}

// UpdateFeaturesOptions configures the UpdateFeatures operation.
type UpdateFeaturesOptions struct {
	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh after update.
	NoRefresh bool

	// NoVacuum skips removing old versions after update.
	NoVacuum bool

	// Component scopes the operation to a single named systemd-sysupdate
	// component. Empty operates on the default domain: the union of the
	// legacy default sysupdate.d directory and every discovered component.
	Component string
}

// CheckFeaturesOptions configures the CheckFeatures operation.
type CheckFeaturesOptions struct {
	// Component scopes the operation to a single named systemd-sysupdate
	// component. Empty operates on the default domain: the union of the
	// legacy default sysupdate.d directory and every discovered component.
	Component string
}

// EnableFeatureOptions configures the EnableFeature operation.
type EnableFeatureOptions struct {
	// Now immediately downloads extensions after enabling.
	Now bool

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh after download.
	NoRefresh bool

	// Component scopes the operation to a single named systemd-sysupdate
	// component. Empty operates on the default domain: the union of the
	// legacy default sysupdate.d directory and every discovered component.
	Component string
}

// installTransferOptions configures the stageTransfer operation.
type installTransferOptions struct {
	// DryRun skips filesystem and sysext mutations.
	DryRun bool

	// CachedManifest, if non-nil, is used instead of fetching the manifest over HTTP.
	CachedManifest *manifest.Manifest
}

// CatalogListOptions configures the CatalogList operation.
type CatalogListOptions struct {
	// Repo limits the listing to a single configured catalog repo.
	// Empty lists every configured repo.
	Repo string

	// Search filters entries to names containing this substring.
	Search string

	// NoCache bypasses the local listing cache and queries the catalog
	// directly (the result still refreshes the cache).
	NoCache bool
}

// CatalogAddOptions configures the CatalogAdd operation.
type CatalogAddOptions struct {
	// Repo selects the catalog repo to add from. Empty probes every
	// configured repo and errors when the name exists in more than one.
	Repo string

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh after download.
	NoRefresh bool
}

// CatalogRemoveOptions configures the CatalogRemove operation.
type CatalogRemoveOptions struct {
	// Repo selects the catalog repo the sysext was added from. Empty
	// locates it automatically and errors when the name is managed by
	// more than one configured repo.
	Repo string

	// Force allows removal of merged extensions (requires reboot).
	Force bool

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh.
	NoRefresh bool
}

// DisableFeatureOptions configures the DisableFeature operation.
type DisableFeatureOptions struct {
	// Now immediately removes files AND unmerges extensions.
	Now bool

	// Force allows removal of merged extensions (requires reboot).
	Force bool

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh.
	NoRefresh bool

	// Component scopes the operation to a single named systemd-sysupdate
	// component. Empty operates on the default domain: the union of the
	// legacy default sysupdate.d directory and every discovered component.
	Component string
}

// RollbackFeatureOptions configures the RollbackFeature operation.
type RollbackFeatureOptions struct {
	// To is the version to link for every transfer of the feature. Empty
	// selects, per transfer, the newest staged version older than the
	// linked one. It must be staged, differ from the linked version and be
	// older than the version being rolled back from.
	To string

	// Now applies the rollback with a systemd-sysext refresh instead of
	// waiting for the next boot.
	Now bool

	// Clear deletes the feature's rollback record and changes no link. It
	// cannot be combined with To or Now. A missing record is not an error.
	Clear bool

	// DryRun previews the rollback without writing the record, changing a
	// link, or refreshing.
	DryRun bool

	// NoRefresh skips the refresh that Now would run.
	NoRefresh bool

	// Component scopes the operation to a single named systemd-sysupdate
	// component. Empty operates on the default domain: the union of the
	// legacy default sysupdate.d directory and every discovered component.
	Component string
}
