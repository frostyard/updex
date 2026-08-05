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

// FeatureInfo represents feature information.
type FeatureInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Enabled       bool     `json:"enabled"`
	Masked        bool     `json:"masked,omitempty"`
	Source        string   `json:"source"`
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
}
