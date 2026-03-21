# SDK API Reference

The `updex` package (`github.com/frostyard/updex/updex`) is the primary public API. All operations go through the `Client` struct.

## Client

```go
type Client struct { /* unexported fields */ }

type ClientConfig struct {
    Definitions  string              // Custom config file path (overrides search paths)
    Verify       bool                // Enable GPG signature verification
    Verbose      bool                // Enable debug output
    Progress     reporter.Reporter   // Progress reporter (optional)
    SysextRunner sysext.SysextRunner // Mock runner for tests (optional)
}

func NewClient(cfg ClientConfig) *Client
```

If `SysextRunner` is provided, `NewClient` calls `sysext.SetRunner()` to inject it globally.

## Methods

### Features

```go
func (c *Client) Features(ctx context.Context) ([]FeatureInfo, error)
```

Lists all configured features with their enabled/masked status and associated transfers.

### EnableFeature / DisableFeature

```go
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error)
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error)
```

Enable creates a drop-in file setting `Enabled=true`. Disable creates one setting `Enabled=false`.

**EnableFeatureOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `Now` | `bool` | Download extensions immediately after enabling |
| `DryRun` | `bool` | Preview without modifying filesystem |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` |

**DisableFeatureOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `Now` | `bool` | Unmerge and remove files immediately |
| `Remove` | `bool` | Deprecated alias for `Now` |
| `Force` | `bool` | Allow removal of currently merged extensions |
| `DryRun` | `bool` | Preview without modifying filesystem |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` |

### UpdateFeatures

```go
func (c *Client) UpdateFeatures(ctx context.Context, opts UpdateFeaturesOptions) ([]UpdateFeaturesResult, error)
```

Downloads and installs the newest available version for each enabled feature's transfers. Returns per-feature results with per-component status.

**UpdateFeaturesOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` after updates |
| `NoVacuum` | `bool` | Skip removing old versions |

### CheckFeatures

```go
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error)
```

Checks for available updates without downloading. `CheckFeaturesOptions` is currently empty.

## Result Types

### FeatureInfo

```go
type FeatureInfo struct {
    Name          string
    Description   string
    Documentation string
    Enabled       bool
    Masked        bool
    Source        string   // Path to .feature file
    Transfers     []string // Associated transfer component names
}
```

### FeatureActionResult

```go
type FeatureActionResult struct {
    Feature           string
    Action            string   // "enable" or "disable"
    Success           bool
    DropIn            string   // Path to created drop-in file
    Error             string
    NextActionMessage string   // User guidance (e.g., "run update to download")
    RemovedFiles      []string
    DownloadedFiles   []string
    DryRun            bool
    Unmerged          bool
}
```

### UpdateFeaturesResult / UpdateResult

```go
type UpdateFeaturesResult struct {
    Feature string
    Results []UpdateResult
}

type UpdateResult struct {
    Component         string
    Version           string
    Downloaded        bool
    Installed         bool
    Error             string
    NextActionMessage string
}
```

### CheckFeaturesResult / CheckResult

```go
type CheckFeaturesResult struct {
    Feature string
    Results []CheckResult
}

type CheckResult struct {
    Component       string
    CurrentVersion  string
    NewestVersion   string
    UpdateAvailable bool
}
```

## Supporting Packages

### `config`

- `LoadFeatures(customPath string) ([]*Feature, error)` — Load all `.feature` files
- `LoadTransfers(customPath string) ([]*Transfer, error)` — Load all `.transfer` files
- `FilterTransfersByFeatures(transfers []*Transfer, features []*Feature) []*Transfer` — Filter transfers to those matching enabled features
- `GetTransfersForFeature(transfers []*Transfer, featureName string) []*Transfer` — Get transfers for a specific feature
- `GetEnabledFeatureNames(features []*Feature) []string`
- `IsFeatureEnabled(features []*Feature, name string) bool`

### `manifest`

- `Fetch(ctx context.Context, baseURL string, verify bool) (*Manifest, error)` — Fetch and parse `SHA256SUMS` from URL
- `VerifyHash(filePath string, expectedHash string) error` — Verify a file's SHA256
- `VerifyHashReader(r io.Reader, expectedHash string) *HashVerifyReader` — Streaming hash verification
- `SetKeyringPaths(paths []string)` — Override GPG keyring locations

### `download`

- `Download(ctx context.Context, url, targetPath, expectedHash string, mode uint32) error` — Download with hash verification and auto-decompression

### `version`

- `ParsePattern(pattern string) (*Pattern, error)` — Parse `@v`-style patterns
- `ExtractVersionMulti(filename string, patternStrs []string) (version, matchedPattern string, ok bool)` — Try multiple patterns
- `Compare(v1, v2 string) int` — Semver comparison (-1, 0, 1)
- `Sort(versions []string)` — Sort descending (newest first)

### `sysext`

- `Refresh() / Merge() / Unmerge()` — `systemd-sysext` commands
- `SetRunner(r SysextRunner) func()` — Inject mock runner (returns cleanup)
- `GetInstalledVersions(t *config.Transfer) ([]string, string, error)` — List installed + current version
- `UpdateSymlink(targetDir, symlinkName, targetFile string) error`
- `LinkToSysext(t *config.Transfer) / UnlinkFromSysext(t *config.Transfer)` — Manage `/var/lib/extensions` symlinks
- `Vacuum(t *config.Transfer) / VacuumWithDetails(t *config.Transfer)` — Clean old versions

### `systemd`

- `GenerateTimer(cfg *TimerConfig) string` — Generate systemd timer unit content
- `GenerateService(cfg *ServiceConfig) string` — Generate systemd service unit content
- `Manager.Install(timer, service) / Remove(name) / Exists(name)` — Unit lifecycle
