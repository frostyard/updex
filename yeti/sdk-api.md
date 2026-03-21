# SDK API Reference

## Client

### Construction

```go
client := updex.NewClient(updex.ClientConfig{
    Definitions: "",              // Custom config path (empty = systemd defaults)
    Verify:      false,           // GPG signature verification
    Verbose:     false,           // Debug output
    Progress:    reporter,        // reporter.Reporter for progress updates
    SysextRunner: nil,            // Mock runner for testing
})
```

`ClientConfig.Progress` defaults to `NoopReporter` if nil.
`ClientConfig.SysextRunner` is only used in tests to inject `sysext.MockRunner`.

## Methods

### Features

```go
func (c *Client) Features(ctx context.Context) ([]FeatureInfo, error)
```

Returns all configured features with their current status. Loads feature
configs from search paths, resolves drop-ins, and checks enabled state.

### EnableFeature

```go
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error)
```

Enables a feature by writing a drop-in at
`/etc/sysupdate.d/{name}.feature.d/00-updex.conf` with `Enabled=true`.

**Options:**
| Field | Type | Description |
|-------|------|-------------|
| `Now` | bool | Download extensions immediately after enabling |
| `DryRun` | bool | Preview changes without modifying filesystem |
| `NoRefresh` | bool | Skip `systemd-sysext refresh` after download |

**Validation:** Feature must exist and not be masked.

When `Now=true`: loads transfers for the feature, calls `installTransfer()`
for each, then refreshes sysext.

### DisableFeature

```go
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error)
```

Disables a feature by writing a drop-in with `Enabled=false`.

**Options:**
| Field | Type | Description |
|-------|------|-------------|
| `Now` | bool | Unmerge extensions and remove all files |
| `Remove` | bool | *Deprecated* — use `Now` instead |
| `Force` | bool | Allow removal of merged extensions (requires reboot) |
| `DryRun` | bool | Preview changes without modifying filesystem |
| `NoRefresh` | bool | Skip `systemd-sysext refresh` |

**Validation:** Checks merge state before removal; requires `Force=true`
if extensions are currently active.

With `Now=true`: unmerges extensions, removes symlinks from
`/var/lib/extensions`, and clears all versions from target directory.

### UpdateFeatures

```go
func (c *Client) UpdateFeatures(ctx context.Context, opts UpdateFeaturesOptions) ([]UpdateFeaturesResult, error)
```

Downloads and installs new versions for all enabled features.

**Options:**
| Field | Type | Description |
|-------|------|-------------|
| `NoRefresh` | bool | Skip `systemd-sysext refresh` after update |
| `NoVacuum` | bool | Skip removing old versions |

**Process:**
1. Load all features and transfers
2. Filter to enabled, non-masked features
3. For each transfer: fetch manifest, extract versions, check for updates
4. Download and install latest version
5. Update symlinks
6. Vacuum old versions (unless `NoVacuum`)
7. Refresh sysext (unless `NoRefresh`)

### CheckFeatures

```go
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error)
```

Read-only check for available updates across all enabled features.

**Options:** None (empty struct).

Returns per-component current version, newest available version, and
whether an update is available.

## Result Types

### FeatureInfo

Returned by `Features()`.

```go
type FeatureInfo struct {
    Name          string   `json:"name"`
    Description   string   `json:"description,omitempty"`
    Documentation string   `json:"documentation,omitempty"`
    Enabled       bool     `json:"enabled"`
    Masked        bool     `json:"masked,omitempty"`
    Source        string   `json:"source"`           // Config file path
    Transfers     []string `json:"transfers,omitzero"` // Associated component names
}
```

### FeatureActionResult

Returned by `EnableFeature()` and `DisableFeature()`.

```go
type FeatureActionResult struct {
    Feature           string   `json:"feature"`
    Action            string   `json:"action"`            // "enable" or "disable"
    Success           bool     `json:"success"`
    DropIn            string   `json:"dropIn,omitempty"`   // Path to created drop-in
    Error             string   `json:"error,omitempty"`
    NextActionMessage string   `json:"nextActionMessage,omitempty"`
    RemovedFiles      []string `json:"removedFiles,omitzero"`
    DownloadedFiles   []string `json:"downloadedFiles,omitzero"`
    DryRun            bool     `json:"dryRun,omitempty"`
    Unmerged          bool     `json:"unmerged,omitempty"`
}
```

### UpdateFeaturesResult / UpdateResult

Returned by `UpdateFeatures()`.

```go
type UpdateFeaturesResult struct {
    Feature string         `json:"feature"`
    Results []UpdateResult `json:"results"`
}

type UpdateResult struct {
    Component         string `json:"component"`
    Version           string `json:"version,omitempty"`
    Downloaded        bool   `json:"downloaded,omitempty"`
    Installed         bool   `json:"installed,omitempty"`
    Error             string `json:"error,omitempty"`
    NextActionMessage string `json:"nextActionMessage,omitempty"`
}
```

### CheckFeaturesResult / CheckResult

Returned by `CheckFeatures()`.

```go
type CheckFeaturesResult struct {
    Feature string        `json:"feature"`
    Results []CheckResult `json:"results"`
}

type CheckResult struct {
    Component       string `json:"component"`
    CurrentVersion  string `json:"currentVersion,omitempty"`
    NewestVersion   string `json:"newestVersion,omitempty"`
    UpdateAvailable bool   `json:"updateAvailable"`
}
```

## Internal Helpers

### getAvailableVersions

```go
func (c *Client) getAvailableVersions(ctx context.Context, transfer *config.Transfer) ([]string, *manifest.Manifest, error)
```

Fetches the remote manifest for a transfer and extracts available versions.
Returns sorted versions (newest first) and the manifest (for hash lookup
during download). Validates source type is `url-file`, applies
`MinVersion` filter.

### installTransfer

```go
func (c *Client) installTransfer(ctx context.Context, transfer *config.Transfer, noRefresh bool) (string, error)
```

Core install logic for a single transfer. Used by `EnableFeature` with
`Now=true`. Fetches manifest, selects newest version, downloads with hash
verification, decompresses, updates symlinks, links to
`/var/lib/extensions`, refreshes sysext, and vacuums.
