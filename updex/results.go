package updex

// CheckResult represents the result of a check operation for a single component.
type CheckResult struct {
	Component       string `json:"component"`
	CurrentVersion  string `json:"current_version,omitempty"`
	NewestVersion   string `json:"newest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

// UpdateResult represents the result of an update operation for a single component.
type UpdateResult struct {
	Component         string `json:"component"`
	Version           string `json:"version"`
	Downloaded        bool   `json:"downloaded"`
	Installed         bool   `json:"installed"`
	Error             string `json:"error,omitempty"`
	NextActionMessage string `json:"next_action_message,omitempty"`
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
