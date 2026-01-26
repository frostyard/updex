package updex

// VersionInfo represents version information for a component.
type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Current   bool   `json:"current"`
	Protected bool   `json:"protected,omitempty"`
	Component string `json:"component,omitempty"`
}

// CheckResult represents the result of a check-new operation.
type CheckResult struct {
	Component       string `json:"component"`
	CurrentVersion  string `json:"current_version,omitempty"`
	NewestVersion   string `json:"newest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// UpdateResult represents the result of an update operation.
type UpdateResult struct {
	Component         string `json:"component"`
	Version           string `json:"version"`
	Downloaded        bool   `json:"downloaded"`
	Installed         bool   `json:"installed"`
	Error             string `json:"error,omitempty"`
	NextActionMessage string `json:"next_action_message,omitempty"`
}

// VacuumResult represents the result of a vacuum operation.
type VacuumResult struct {
	Component string   `json:"component"`
	Removed   []string `json:"removed"`
	Kept      []string `json:"kept"`
	Error     string   `json:"error,omitempty"`
}

// PendingResult represents the result of a pending check.
type PendingResult struct {
	Component        string `json:"component"`
	ActiveVersion    string `json:"active_version,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	Pending          bool   `json:"pending"`
}

// ComponentInfo represents component information.
type ComponentInfo struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	SourceType   string `json:"source_type"`
	TargetPath   string `json:"target_path"`
	InstancesMax int    `json:"instances_max"`
}

// ExtensionInfo represents discovered extension information.
type ExtensionInfo struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Error    string   `json:"error,omitempty"`
}

// DiscoverResult represents the complete discovery result.
type DiscoverResult struct {
	URL        string          `json:"url"`
	Extensions []ExtensionInfo `json:"extensions"`
}

// FeatureInfo represents feature information.
type FeatureInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Enabled       bool     `json:"enabled"`
	Masked        bool     `json:"masked,omitempty"`
	Source        string   `json:"source"`
	Transfers     []string `json:"transfers,omitempty"`
}

// FeatureActionResult represents the result of a feature enable/disable action.
type FeatureActionResult struct {
	Feature           string   `json:"feature"`
	Action            string   `json:"action"`
	Success           bool     `json:"success"`
	DropIn            string   `json:"drop_in,omitempty"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
	RemovedFiles      []string `json:"removed_files,omitempty"`
	DownloadedFiles   []string `json:"downloaded_files,omitempty"`
	DryRun            bool     `json:"dry_run,omitempty"`
	Unmerged          bool     `json:"unmerged,omitempty"`
}

// InstallResult represents the result of an install operation.
type InstallResult struct {
	Component         string `json:"component"`
	TransferFile      string `json:"transfer_file"`
	Version           string `json:"version,omitempty"`
	Installed         bool   `json:"installed"`
	Error             string `json:"error,omitempty"`
	NextActionMessage string `json:"next_action_message,omitempty"`
}

// RemoveResult represents the result of a remove operation.
type RemoveResult struct {
	Component         string   `json:"component"`
	RemovedFiles      []string `json:"removed_files,omitempty"`
	RemovedSymlink    bool     `json:"removed_symlink"`
	Unmerged          bool     `json:"unmerged"`
	Success           bool     `json:"success"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
}
