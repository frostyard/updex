package updex

// ListOptions configures the List operation.
type ListOptions struct {
	// Component filters results to a specific component.
	// If empty, all components are included.
	Component string

	// Version filters results to a specific version.
	// If empty, all versions are included.
	Version string
}

// CheckOptions configures the CheckNew operation.
type CheckOptions struct {
	// Component filters results to a specific component.
	// If empty, all components are included.
	Component string
}

// UpdateOptions configures the Update operation.
type UpdateOptions struct {
	// Component filters updates to a specific component.
	// If empty, all components are updated.
	Component string

	// Version specifies a specific version to install.
	// If empty, the newest available version is installed.
	Version string

	// NoVacuum skips removing old versions after update.
	NoVacuum bool

	// NoRefresh skips running systemd-sysext refresh after update.
	NoRefresh bool
}

// InstallOptions configures the Install operation.
type InstallOptions struct {
	// Component is the name of the extension to install.
	// This is required.
	Component string

	// NoRefresh skips running systemd-sysext refresh after install.
	NoRefresh bool
}

// VacuumOptions configures the Vacuum operation.
type VacuumOptions struct {
	// Component filters vacuum to a specific component.
	// If empty, all components are vacuumed.
	Component string
}

// PendingOptions configures the Pending operation.
type PendingOptions struct {
	// Component filters results to a specific component.
	// If empty, all components are included.
	Component string
}

// RemoveOptions configures the Remove operation.
type RemoveOptions struct {
	// Now unmerges the extension immediately.
	Now bool

	// NoRefresh skips running systemd-sysext refresh after removal.
	NoRefresh bool
}

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
