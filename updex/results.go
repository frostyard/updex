package updex

// UpdateResult represents the result of an update operation.
type UpdateResult struct {
	Component         string `json:"component"`
	Downloaded        bool   `json:"downloaded"`
	Installed         bool   `json:"installed"`
	Error             string `json:"error,omitempty"`
	NextActionMessage string `json:"next_action_message,omitempty"`
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
