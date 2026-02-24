package updex

// UpdateOptions configures the Update operation.
type UpdateOptions struct {
	// Component filters updates to a specific component.
	// If empty, all enabled components are updated.
	Component string

	// NoRefresh skips running systemd-sysext refresh after update.
	NoRefresh bool
}

// EnableFeatureOptions configures the EnableFeature operation.
type EnableFeatureOptions struct {
	// Now immediately downloads extensions via systemd-sysupdate after enabling.
	Now bool

	// DryRun previews changes without modifying filesystem.
	DryRun bool

	// NoRefresh skips running systemd-sysext refresh after download.
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
}
