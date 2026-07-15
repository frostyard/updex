package updex

import "github.com/frostyard/updex/manifest"

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

// installTransferOptions configures the installTransfer operation.
type installTransferOptions struct {
	// DryRun skips filesystem and sysext mutations.
	DryRun bool

	// NoVacuum skips removing old versions after install.
	NoVacuum bool

	// NoRefresh skips running systemd-sysext refresh after install.
	NoRefresh bool

	// CachedManifest, if non-nil, is used instead of fetching the manifest over HTTP.
	CachedManifest *manifest.Manifest
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
