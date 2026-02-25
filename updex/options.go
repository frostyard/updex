package updex

// UpdateFeaturesOptions configures the UpdateFeatures operation.
type UpdateFeaturesOptions struct {
	// NoRefresh skips running systemd-sysext refresh after update.
	NoRefresh bool

	// NoVacuum skips removing old versions after update.
	NoVacuum bool
}

// CheckFeaturesOptions configures the CheckFeatures operation.
type CheckFeaturesOptions struct{}

// EnableFeatureOptions configures the EnableFeature operation.
type EnableFeatureOptions struct {
	// Now immediately downloads extensions after enabling.
	Now bool

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// Retry enables automatic retry on network failures.
	Retry bool

	// RetryCount is the number of retries when Retry is true (default 3).
	RetryCount int

	// NoRefresh skips running systemd-sysext refresh after download.
	NoRefresh bool
}

// DisableFeatureOptions configures the DisableFeature operation.
type DisableFeatureOptions struct {
	// Remove deletes downloaded files for this feature's transfers.
	// DEPRECATED: --now now includes this behavior.
	Remove bool

	// Now immediately removes files AND unmerges extensions.
	Now bool

	// Force allows removal of merged extensions (requires reboot).
	Force bool

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh.
	NoRefresh bool
}
