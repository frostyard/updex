package updex

// DaemonActionResult represents the result of enabling or disabling the
// automatic update daemon.
type DaemonActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	// DryRun reports that the daemon units and their systemd state were not
	// changed; Message describes the actions that a real run would take.
	DryRun bool `json:"dry_run,omitempty"`
	// UnitFiles holds the absolute paths of the unit files the operation
	// wrote, removed, or would write or remove (timer first), or, when
	// Packaged is set on an enable, the packaged files in use. Disable lists
	// only the files updex removed; a unit that the OS package ships is not
	// listed because disable never removes it.
	UnitFiles []string `json:"unit_files,omitzero"`
	// Packaged reports that the OS package ships the timer, so enable wrote
	// nothing and disable left the packaged units in place.
	Packaged bool `json:"packaged,omitempty"`
	// KeptFiles holds the unit files in /etc/systemd/system that disable
	// left in place because updex cannot prove that it wrote them.
	KeptFiles []string `json:"kept_files,omitzero"`
}

// DaemonStatusResult represents the automatic update daemon's current state.
type DaemonStatusResult struct {
	// Installed reports that a timer unit file exists, packaged or in /etc.
	Installed bool `json:"installed"`
	Enabled   bool `json:"enabled"`
	Active    bool `json:"active"`
	// Schedule is the timer's effective OnCalendar schedule, a comma-separated
	// list of its calendar triggers. It comes from the TimersCalendar property
	// systemd reports, which includes administrator drop-ins, or, when systemd
	// cannot report it, from the unit file in use and its drop-ins.
	Schedule string `json:"schedule,omitempty"`
	// Source is "packaged" when the timer file in use is the package's in
	// /usr/lib/systemd/system, or "etc" when it is in /etc/systemd/system
	// (where it overrides a packaged one).
	Source string `json:"source,omitempty"`
	// UnitFiles holds the paths of the timer and service files in use.
	UnitFiles []string `json:"unit_files,omitzero"`
	// NextRun is the timer's NextElapseUSecRealtime as systemd prints it;
	// empty when no run is scheduled.
	NextRun string `json:"next_run,omitempty"`
	// LastRun is the service's ExecMainExitTimestamp as systemd prints it;
	// empty when the service has not run since boot.
	LastRun string `json:"last_run,omitempty"`
	// LastResult is the service's Result for the last run ("success",
	// "exit-code", ...); empty when LastRun is empty.
	LastResult string `json:"last_result,omitempty"`
}

// CheckResult represents the result of a check operation for a single component.
type CheckResult struct {
	Component       string `json:"component"`
	CurrentVersion  string `json:"current_version,omitempty"`
	NewestVersion   string `json:"newest_version"`
	UpdateAvailable bool   `json:"update_available"`
	// Error is set when the component could not be checked (manifest fetch,
	// signature verification, pattern failure, or installed-version listing).
	// UpdateAvailable is always false in that case; other fields may be empty,
	// except NewestVersion may be set if the failure happens after reading the manifest.
	Error string `json:"error,omitempty"`
}

// UpdateResult represents the result of an update operation for a single component.
type UpdateResult struct {
	Component         string   `json:"component"`
	Version           string   `json:"version"`
	Downloaded        bool     `json:"downloaded"`
	Installed         bool     `json:"installed"`
	DryRun            bool     `json:"dry_run,omitempty"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
	RemovedVersions   []string `json:"removed_versions,omitzero"`
}

// UpdateFeaturesResult represents the result of updating all enabled features.
type UpdateFeaturesResult struct {
	Feature string         `json:"feature"`
	Results []UpdateResult `json:"results"`
}

// CheckFeaturesResult represents the result of checking all enabled features.
type CheckFeaturesResult struct {
	Feature string        `json:"feature"`
	Results []CheckResult `json:"results"`
}

// Feature origin kinds reported in FeatureInfo.Origin. Kind and name are
// kept separate so consumers can match on the kind without having to
// disambiguate a catalog legitimately named "image" or "local".
const (
	// FeatureOriginCatalog: added by 'updex catalog add'. OriginName is
	// the catalog repo, e.g. "fedora".
	FeatureOriginCatalog = "catalog"
	// FeatureOriginImage: shipped in /usr/lib by the OS image. OriginName
	// is the image identifier (see config.ImageName), e.g. "ucore".
	FeatureOriginImage = "image"
	// FeatureOriginLocal: administered on this machine. OriginName is the
	// search root: "etc", "usr" (/usr/local/lib), or "run".
	FeatureOriginLocal = "local"
	// FeatureOriginUnknown: outside every search root, i.e. loaded from a
	// --definitions override directory. OriginName is empty.
	FeatureOriginUnknown = "unknown"
)

// FeatureInfo represents feature information.
type FeatureInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Enabled       bool     `json:"enabled"`
	Masked        bool     `json:"masked,omitempty"`
	Source        string   `json:"source"`
	Origin        string   `json:"origin"`
	OriginName    string   `json:"origin_name,omitempty"`
	Transfers     []string `json:"transfers,omitzero"`
}

// CatalogEntry represents one sysext available from a configured catalog repo.
type CatalogEntry struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
}

// CatalogAddResult represents the result of adding a sysext from a catalog.
type CatalogAddResult struct {
	Name         string               `json:"name"`
	Repo         string               `json:"repo"`
	Component    string               `json:"component"`
	TransferFile string               `json:"transfer_file"`
	FeatureFile  string               `json:"feature_file"`
	DryRun       bool                 `json:"dry_run,omitempty"`
	Enable       *FeatureActionResult `json:"enable,omitempty"`
}

// CatalogRemoveResult represents the result of removing a catalog-managed sysext.
type CatalogRemoveResult struct {
	Name         string               `json:"name"`
	Repo         string               `json:"repo"`
	Component    string               `json:"component"`
	RemovedFiles []string             `json:"removed_files,omitzero"`
	DryRun       bool                 `json:"dry_run,omitempty"`
	Disable      *FeatureActionResult `json:"disable,omitempty"`
}

// FeatureActionResult represents the result of a feature enable/disable action.
type FeatureActionResult struct {
	Feature           string   `json:"feature"`
	Action            string   `json:"action"`
	Success           bool     `json:"success"`
	DropIn            string   `json:"drop_in,omitempty"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
	RemovedFiles      []string `json:"removed_files,omitzero"`
	DownloadedFiles   []string `json:"downloaded_files,omitzero"`
	DryRun            bool     `json:"dry_run,omitempty"`
	Unmerged          bool     `json:"unmerged,omitempty"`
	// RefreshError is set when every requested filesystem change succeeded
	// but the final `systemd-sysext refresh` failed. Success is false and
	// Error carries the same message; NextActionMessage says how to finish
	// activation (a manual refresh or a reboot). For a disable with Now the
	// preceding unmerge already ran, so until that refresh happens every
	// extension on the host stays unmerged.
	RefreshError string `json:"refresh_error,omitempty"`
}
