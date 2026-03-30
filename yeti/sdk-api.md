# SDK API Reference

The `updex` package (`github.com/frostyard/updex/updex`) is the primary public API. All operations go through the `Client` struct.

## Client

```go
type Client struct { /* unexported fields */ }

type ClientConfig struct {
    Definitions        string                // Custom config file path (overrides search paths)
    Verify             bool                  // Enable GPG signature verification
    Verbose            bool                  // Enable debug output
    Progress           reporter.Reporter     // Progress reporter (optional)
    SysextRunner       sysext.SysextRunner   // Mock runner for tests (optional)
    OnDownloadProgress download.ProgressFunc // Download progress callback (optional)
    HTTPClient         *http.Client          // Shared HTTP client (optional)
}

func NewClient(cfg ClientConfig) *Client
```

`NewClient` stores the provided `SysextRunner` directly on the `Client` struct. If `SysextRunner` is nil, it defaults to `&sysext.DefaultRunner{}`. If `Progress` is nil, it defaults to `reporter.NoopReporter{}`. `OnDownloadProgress` is passed through to `download.Download` calls — when non-nil, it is called with the HTTP response content length (-1 if unknown) and should return an `io.Writer` that receives downloaded bytes for progress tracking (return nil to skip progress for that download). If `HTTPClient` is nil, a default `http.Client` with a 10-minute timeout is created and reused for all manifest fetches and file downloads, enabling HTTP keep-alive connection reuse. The client does not mutate global package state.

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

Enable creates a drop-in file setting `Enabled=true`. With `Now: true`, it downloads extensions via the shared `installTransfer` pipeline. Disable creates a drop-in setting `Enabled=false`.

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
| `Force` | `bool` | Allow removal of currently merged extensions (requires reboot) |
| `DryRun` | `bool` | Preview without modifying filesystem |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` |

### UpdateFeatures

```go
func (c *Client) UpdateFeatures(ctx context.Context, opts UpdateFeaturesOptions) ([]UpdateFeaturesResult, error)
```

Downloads and installs the newest available version for each enabled feature's transfers. Delegates per-component work to the internal `installTransfer` pipeline (which handles download, symlink, sysext linking, and vacuum). Manifests are cached by source URL — transfers sharing the same source avoid redundant HTTP requests. Parsed source patterns are returned from version listing and reused by the install pipeline to avoid redundant pattern compilation. Refresh is batched — a single `systemd-sysext refresh` runs after all components are processed. Returns per-feature results with per-component status.

**UpdateFeaturesOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` after updates |
| `NoVacuum` | `bool` | Skip removing old versions |

### CheckFeatures

```go
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error)
```

Checks for available updates without downloading. Manifests are cached by source URL, same as `UpdateFeatures`. `CheckFeaturesOptions` is currently empty.

## Result Types

### FeatureInfo

```go
type FeatureInfo struct {
    Name          string   `json:"name"`
    Description   string   `json:"description,omitempty"`
    Documentation string   `json:"documentation,omitempty"`
    Enabled       bool     `json:"enabled"`
    Masked        bool     `json:"masked,omitempty"`
    Source        string   `json:"source"`
    Transfers     []string `json:"transfers,omitzero"`
}
```

### FeatureActionResult

```go
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
```

> **Note:** Slice fields use `omitzero` (Go 1.24+) — they are omitted from JSON when nil/empty. Scalar fields use `omitempty` for the same effect on zero values.

### UpdateFeaturesResult / UpdateResult

```go
type UpdateFeaturesResult struct {
    Feature string         `json:"feature"`
    Results []UpdateResult `json:"results"`
}

type UpdateResult struct {
    Component         string `json:"component"`
    Version           string `json:"version"`
    Downloaded        bool   `json:"downloaded"`
    Installed         bool   `json:"installed"`
    Error             string `json:"error,omitempty"`
    NextActionMessage string `json:"next_action_message,omitempty"`
}
```

### CheckFeaturesResult / CheckResult

```go
type CheckFeaturesResult struct {
    Feature string        `json:"feature"`
    Results []CheckResult `json:"results"`
}

type CheckResult struct {
    Component       string `json:"component"`
    CurrentVersion  string `json:"current_version,omitempty"`
    NewestVersion   string `json:"newest_version"`
    UpdateAvailable bool   `json:"update_available"`
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

- `Fetch(ctx context.Context, httpClient *http.Client, baseURL string, verify bool) (*Manifest, error)` — Fetch and parse `SHA256SUMS` from URL. If `httpClient` is nil, a default client with a 30-second timeout is used
- `VerifyHash(filePath string, expectedHash string) error` — Verify a file's SHA256
- `VerifyHashReader(r io.Reader, expectedHash string) *HashVerifyReader` — Streaming hash verification

### `download`

- `Download(ctx context.Context, httpClient *http.Client, url, targetPath, expectedHash string, mode uint32, onProgress ProgressFunc) error` — Download with hash verification (on compressed bytes) and auto-decompression. Uses atomic rename (falls back to atomic copy on cross-device). If `httpClient` is nil, a default client with a 10-minute timeout is used. Default mode: `0644` if `mode == 0`. `onProgress` is called with the response content length; the returned `io.Writer` receives downloaded bytes. Pass nil to disable progress tracking
- `ProgressFunc` — `func(contentLength int64) io.Writer` callback type for download progress
- `DecompressReader(r io.Reader, compressionType string) (io.ReadCloser, error)` — Returns a decompressing reader for `"xz"`, `"gz"`, `"zstd"`, or passthrough for `""`

### `version`

- `ParsePattern(pattern string) (*Pattern, error)` — Parse `@v`-style patterns. Returns `ErrEmptyPattern` or `ErrMissingVersionPlaceholder` on invalid input
- `ParsePatterns(patternStrs []string) ([]*Pattern, error)` — Parse multiple patterns; returns all successfully parsed patterns and the first error encountered (callers proceed if at least one pattern parsed)
- `ExtractVersionParsed(filename string, patterns []*Pattern) (version, matchedPattern string, ok bool)` — Try pre-parsed patterns against a filename (preferred for loops)
- `Compare(v1, v2 string) int` — Semver comparison (-1, 0, 1); normalizes by stripping `v`/`V` prefix; falls back to string comparison
- `Sort(versions []string)` — Sort descending (newest first)

**`Pattern` methods:**
- `ExtractVersion(filename string) (string, bool)` — Extract version from a single filename
- `Matches(filename string) bool` — Test if filename matches the pattern
- `BuildFilename(version string) string` — Construct filename from a version string
- `Raw() string` — Return the original pattern string

### `sysext`

- `SysextRunner` interface — `Refresh()`, `Merge()`, `Unmerge()`, `LinkToSysext(*config.Transfer)` methods executed via `DefaultRunner` (real commands) or `MockRunner` (tests)
- `GetInstalledVersions(t *config.Transfer) ([]string, string, error)` — List installed + current version
- `GetActiveVersion(t *config.Transfer) (string, error)` — Get version currently active in systemd-sysext (checks current symlink and `/run/extensions`)
- `UpdateSymlink(targetDir, symlinkName, targetFile string) error`
- `LinkToSysext(t *config.Transfer) / UnlinkFromSysext(t *config.Transfer)` — Manage `/var/lib/extensions` symlinks
- `Vacuum(t *config.Transfer) / VacuumWithDetails(t *config.Transfer)` — Clean old versions
- `RemoveAllVersions(t *config.Transfer) ([]string, error)` — Remove all versions and current symlink for a component
- `GetExtensionName(filename string) string` — Extract extension name from filename (strips version and compression suffixes)
- `SysextDir` — Constant: `/var/lib/extensions`

### `systemd`

- `NewManager() *Manager` — Create manager with default paths (`/etc/systemd/system`)
- `NewTestManager(unitPath string, runner SystemctlRunner) *Manager` — Create manager with custom paths and runner for testing
- `GenerateTimer(cfg *TimerConfig) string` — Generate systemd timer unit content
- `GenerateService(cfg *ServiceConfig) string` — Generate systemd service unit content
- `Manager.Install(timer, service) / Remove(name) / Exists(name)` — Unit lifecycle
- `SystemctlRunner` interface — `DaemonReload()`, `Enable(unit)`, `Disable(unit)`, `Start(unit)`, `Stop(unit)`, `IsActive(unit)`, `IsEnabled(unit)` methods executed via `DefaultSystemctlRunner` (real commands) or `MockSystemctlRunner` (tests)
