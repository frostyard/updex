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
    Paths              RuntimePaths          // Instance-scoped filesystem paths (optional)
}

// RuntimePaths holds the filesystem paths an updex.Client consults at runtime.
// Zero values resolve to current production defaults at NewClient time.
type RuntimePaths struct {
    DefinitionRoots    []string // Roots for sysupdate.d directories; default: config.SearchRoots
    OSReleasePaths     []string // os-release files for specifier expansion; default: config.OSReleasePaths
    CatalogConfigRoots []string // Dirs for *.catalog files; default: catalog.ConfigRoots
    CatalogCacheDir    string   // Cache dir for catalog listings; default: catalog.CacheDir
    CatalogTargetPath  string   // Staging dir for catalog transfers; default: catalog.TargetPath
    SysextLinkDir      string   // Dir for systemd-sysext image links; default: sysext.SysextDir
    RunExtensionsDir   string   // Dir for merged sysext images; default: sysext.RunExtensionsDir
}

// DisableCatalogCache is a RuntimePaths.CatalogCacheDir sentinel that
// explicitly disables catalog listing caching for one client.
const DisableCatalogCache = "\x00"

func NewClient(cfg ClientConfig) *Client
```

`NewClient` resolves `cfg.Paths` once at construction: each zero field reads its corresponding package-level compatibility variable or production constant exactly once and takes a defensive copy of slices. After construction the client never consults those package variables again, so mutating `config.SearchRoots`, `catalog.ConfigRoots`, `catalog.CacheDir`, or `sysext.SysextDir` cannot redirect the client. This is the ADR-0011 invariant: all runtime dependencies, including merged sysext state, are captured immutably at construction.

Path-dependent supporting-package APIs have explicit variants for SDK use:
`config.Load*In` receives definition roots and, for transfers, os-release
paths; `catalog.LoadReposFrom`, `CachedListIn`, and `RenderTransferTo` receive
their paths directly; and sysext `*At` operations receive the captured sysext
directory. `sysext.GetActiveVersionIn` additionally receives the captured
merged-image directory. The original package functions remain compatibility
wrappers over their package variables or production constants.

Other fields: if `SysextRunner` is nil it defaults to `&sysext.DefaultRunner{}`; if `Progress` is nil it defaults to `reporter.NoopReporter{}`; if `HTTPClient` is nil a default `http.Client` with a 10-minute timeout, the standard 10-redirect limit, and an HTTPS-to-HTTP downgrade refusal is created. HTTP-to-HTTP and HTTPS-to-HTTPS redirects remain allowed. A caller-supplied `HTTPClient` is stored unchanged, including its redirect policy. `OnDownloadProgress` is called with the HTTP response content length (-1 if unknown) and must return a fresh `io.Writer` per attempt to avoid double-counting retried downloads.

## Methods

### Features

```go
func (c *Client) Features(ctx context.Context, opts ...FeaturesOptions) ([]FeatureInfo, error)
```

Lists all configured features with their enabled/masked status and associated transfers. `opts` is variadic for backward compatibility with the pre-component-scoping signature (`Features(ctx)`): only `opts[0]` is read when present, so callers may either omit `opts` entirely or pass a single `FeaturesOptions{}`.

**FeaturesOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `Component` | `string` | Scope to one named systemd-sysupdate component instead of the default union (see "Component scoping" below); `""` = default |

### Components

```go
func (c *Client) Components(ctx context.Context) ([]ComponentInfo, error)
```

Lists discovered systemd-sysupdate components (`sysupdate.<name>.d/` directories, see `config.DiscoverComponents`) — name, highest-priority existing source directory, and that component's own feature count. Does **not** include the legacy default `sysupdate.d/` "component"; call `Features` with the default (empty) `Component` to see the full union.

```go
type ComponentInfo struct {
    Name         string `json:"name"`
    SourceDir    string `json:"source_dir"`
    FeatureCount int    `json:"feature_count"`
}
```

### Component scoping

Every feature-related options struct (`FeaturesOptions`, `EnableFeatureOptions`, `DisableFeatureOptions`, `UpdateFeaturesOptions`, `CheckFeaturesOptions`) carries a `Component string` field. All SDK methods resolve their read/write domain through the unexported `Client.loadDomain(component string)` (decision recorded in [ADR-0001](../adr/0001-read-domain-resolution-via-loaddomain.md)):

1. `ClientConfig.Definitions` set → load exactly that one directory (as before component support existed); `Component` must be `""` here, otherwise `loadDomain` returns an error (`Definitions` and `Component` are mutually exclusive).
2. `Component` non-empty → load only that named component's own search paths (`config.LoadComponentFeatures`/`LoadComponentTransfers`).
3. Otherwise (the default) → the union of the legacy default `sysupdate.d/` directory and every discovered component (`config.LoadAllFeatures`/`LoadAllTransfers`). Any name collision between sources is logged through the client's reporter as a warning (component wins over the legacy default directory), not returned as an error.

In all three cases, transfers are filtered to sysext-shaped ones (`config.FilterSysextTransfers` / `IsSysextTransfer`): a `url-file` source to a `regular-file` target with no `PathRelativeTo`. This drops the non-sysext OS transfers (A/B partition, UKI) that share the legacy default directory on native images (decision recorded in [ADR-0002](../adr/0002-skip-non-sysext-transfers.md)).

### EnableFeature / DisableFeature

```go
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error)
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error)
```

Enable creates a drop-in file setting `Enabled=true`. With `Now: true`, it downloads extensions via the shared `installTransfer` pipeline. Disable creates a drop-in setting `Enabled=false`.

In dry-run mode, enable/disable skip writing drop-ins and skip sysext/filesystem mutations. `EnableFeature` with `Now: true` records associated transfer components as would-download entries without fetching manifests or resolving exact versions. `DisableFeature` with `Now: true` still checks active versions for force-safety, then records component-level would-remove entries instead of deleting files.

Both methods reject missing or masked features before writing drop-ins. The drop-in target directory depends on where the feature file was discovered (`config.ComponentOfPath(f.FilePath)`): a feature found under a `sysupdate.<name>.d/` component writes to `/etc/sysupdate.<name>.d/<feature>.feature.d/00-updex.conf` (via `config.EtcComponentDir(name)`); a feature from the legacy default directory or a `ClientConfig.Definitions` override keeps the original `/etc/sysupdate.d/<feature>.feature.d/00-updex.conf` path. Dry-run returns that would-be path but leaves `FeatureActionResult.DropIn` empty because no file was written.

**EnableFeatureOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `Now` | `bool` | Download extensions immediately after enabling |
| `DryRun` | `bool` | Preview without modifying filesystem |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` |
| `Component` | `string` | Scope to one named component; `""` = default union |

**DisableFeatureOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `Now` | `bool` | Unmerge and remove files immediately |
| `Force` | `bool` | Allow removal of currently merged extensions (requires reboot) |
| `DryRun` | `bool` | Preview without modifying filesystem |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` |
| `Component` | `string` | Scope to one named component; `""` = default union |

### UpdateFeatures

```go
func (c *Client) UpdateFeatures(ctx context.Context, opts UpdateFeaturesOptions) ([]UpdateFeaturesResult, error)
```

Downloads and installs the newest available version for each enabled feature's transfers. Delegates per-component work to the internal `installTransfer` pipeline (which handles download, legacy staging-symlink cleanup, sysext linking, and vacuum). Manifests are cached by source URL — transfers sharing the same source avoid redundant HTTP requests. Parsed source patterns are returned from version listing and reused by the install pipeline to avoid redundant pattern compilation. Refresh is batched — a single `systemd-sysext refresh` runs after all components are processed. With `DryRun: true`, manifests are fetched and versions are selected, but download, legacy cleanup, sysext linking, refresh, and vacuum deletion are skipped. Returns per-feature results with per-component status.

The manifest cache key is `Transfer.Source.Path` only. The first transfer to fetch a source determines whether that cached manifest was GPG-verified, so changes that require different verification/auth behavior per transfer must change the cache key or bypass caching.

Dry-run update results use the normal `UpdateResult` shape: `Downloaded=true` means the component would be downloaded, `Installed=false` means no install happened, and `RemovedVersions` is populated from `sysext.PlanVacuumAfterInstall` unless `NoVacuum` is true. The CLI still enforces root before calling this SDK method, but the SDK method itself is read-only in dry-run mode apart from remote manifest fetches.

Already-current components are detected by `sysext.GetInstalledVersions`: the selected newest version must be both present on disk and equal to the current version resolved from a legacy `CurrentSymlink` (or newest installed when no symlink exists). After current detection but before any no-op return, update removes the legacy staging symlink if the transfer defines one. A newer installed-but-not-current version is still treated as needing installation so the `/var/lib/extensions` link can be updated.

**UpdateFeaturesOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `DryRun` | `bool` | Preview downloads, installs, refreshes, and vacuum removals without modifying filesystem or sysext state; still fetches manifests and inspects local installed files |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` after updates |
| `NoVacuum` | `bool` | Skip removing old versions |
| `Component` | `string` | Scope to one named component; `""` = default union |

### CheckFeatures

```go
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error)
```

Checks for available updates without downloading. Manifests are cached by source URL, same as `UpdateFeatures`.

**CheckFeaturesOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `Component` | `string` | Scope to one named component; `""` = default union |

### CatalogList / CatalogAdd / CatalogRemove

```go
func (c *Client) CatalogList(ctx context.Context, opts CatalogListOptions) ([]CatalogEntry, error)
func (c *Client) CatalogAdd(ctx context.Context, name string, opts CatalogAddOptions) (*CatalogAddResult, error)
func (c *Client) CatalogRemove(ctx context.Context, name string, opts CatalogRemoveOptions) (*CatalogRemoveResult, error)
```

Catalog operations over the repos configured via `catalog.LoadRepos()`
(see the `catalog` package below and `docs/design/overview.md` "Catalogs"). All three
error when no catalogs are configured (with setup guidance) or when the
client has a `Definitions` override. Ownership checks are pinned by
[ADR-0003](../adr/0003-catalog-ownership-marker.md); the snapshot/rollback
and Lstat rules by
[ADR-0005](../adr/0005-transactional-writes-lstat-checks.md).

- `CatalogList` enumerates each repo via its `ListURL` (skipping repos
  without one, with a warning — unless explicitly selected via
  `opts.Repo`, which errors instead) and cross-references
  `config.LoadComponentFeatures(repo.Component)` to fill
  `Installed`/`Enabled`, which are set only when the matched feature's
  marker names this repo (`catalog.GeneratedFileRepo(f.FilePath)`), so
  repos sharing a `Component` don't inherit each other's status.
  `opts.Search` is a substring filter; search is just `CatalogList` with
  `Search` set.
- `CatalogAdd` validates the name, resolves the repo (explicit
  `opts.Repo`, else probes every repo's `FetchConf` and errors on
  multiple hits listing `repo/name` candidates), refuses to overwrite
  target files that are unmarked or marked by a different repo
  (`catalog.GeneratedFileRepo`), writes `RenderTransfer`/`RenderFeature`
  output to `config.EtcComponentDir(repo.Component)`, then calls
  `EnableFeature{Now: true, Component: repo.Component}`. Writes are
  snapshotted first and *every* failure after that point (the `MkdirAll`,
  either `os.WriteFile`, or the enable/download) restores the prior state
  exactly: a fresh add's files are deleted (with the drop-in dir and
  component dir removed if empty), a re-add's previous
  `.transfer`/`.feature`/`00-updex.conf` contents are rewritten. With
  `DryRun: true` it reports the target paths (conflict check still runs)
  and skips the writes and the enable.
- `CatalogRemove` validates the name and finds the owning repo: the
  configured repo whose /etc component dir holds a `<name>.feature` whose
  marker names that repo. Errors when none — "not a catalog-managed
  sysext" — or several. Before anything destructive it also requires the
  `<name>.transfer`, when present, to be marked by that repo, erroring
  out otherwise (`DisableFeature{Now}` would delete images described by a
  foreign transfer). It then calls
  `DisableFeature{Now: true, Force, Component}` and deletes the
  `.transfer`, the `.feature`, and only updex's own `00-updex.conf`
  drop-in; the `.feature.d` and component directories are removed only if
  they end up empty, so administrator drop-ins survive.

**CatalogListOptions:** `Repo`, `Search`, `NoCache` (bypass the listing
cache — see `catalog.CachedList`; the CLI flag is `--no-cache`).
**CatalogAddOptions:** `Repo`, `DryRun`, `NoRefresh`.
**CatalogRemoveOptions:** `Repo`, `Force`, `DryRun`, `NoRefresh`.

**CatalogEntry:** `Name`, `Repo`, `Installed`, `Enabled`.
**CatalogAddResult:** `Name`, `Repo`, `Component`, `TransferFile`,
`FeatureFile`, `DryRun`, `Enable *FeatureActionResult`.
**CatalogRemoveResult:** `Name`, `Repo`, `Component`, `RemovedFiles`,
`DryRun`, `Disable *FeatureActionResult`.

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
    Origin        string   `json:"origin"`
    OriginName    string   `json:"origin_name,omitempty"`
    Transfers     []string `json:"transfers,omitzero"`
}
```

`Origin`/`OriginName` say where the feature came from, derived from
`Source` alone by `updex.featureOrigin`. Kind and name are separate fields
so consumers match on the kind (`select(.origin=="catalog")`) without
having to disambiguate a catalog legitimately named `image` or `local`:

| `Origin` (const) | `OriginName` | Source |
|---|---|---|
| `catalog` (`FeatureOriginCatalog`) | catalog repo, e.g. `fedora` | file carries the `catalog.GeneratedMarker` header — wins over the root |
| `image` (`FeatureOriginImage`) | `config.ImageName()`, e.g. `ucore`; empty if os-release names none | `/usr/lib` |
| `local` (`FeatureOriginLocal`) | `etc`, `usr` (/usr/local/lib), or `run` | administered on this machine |
| `unknown` (`FeatureOriginUnknown`) | empty | loaded through `-C`/`--definitions` (whatever root that directory happens to sit under), or otherwise outside every search root |

`Features()` resolves `config.ImageName()` once per call rather than per
feature. The CLI renders this as the CATALOG column via `formatOrigin`:
bare name for catalogs, `kind:name` otherwise.

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
    Component         string   `json:"component"`
    Version           string   `json:"version"`
    Downloaded        bool     `json:"downloaded"`
    Installed         bool     `json:"installed"`
    DryRun            bool     `json:"dry_run,omitempty"`
    Error             string   `json:"error,omitempty"`
    NextActionMessage string   `json:"next_action_message,omitempty"`
    RemovedVersions   []string `json:"removed_versions,omitzero"`
}
```

For dry-run update results, `Downloaded=true` means the component would be downloaded, `Installed=false` means no install was performed, and `RemovedVersions` lists versions vacuum would remove if `NoVacuum` is false. For non-dry-run results, `Downloaded=true` means a new file was fetched and installed; already-current components still report `Installed=true` but `Downloaded=false`. Non-dry-run `RemovedVersions` is currently not populated because `installTransfer` calls `sysext.Vacuum` rather than `VacuumWithDetails`.

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

- `LoadFeatures(customPath string) ([]*Feature, error)` — Load all `.feature` files from `customPath`, or the legacy default search paths if empty. No component discovery.
- `LoadTransfers(customPath string) ([]*Transfer, error)` — Load all `.transfer` files from `customPath`, or the legacy default search paths if empty. No component discovery, no sysext-type filtering.
- `FilterTransfersByFeatures(transfers []*Transfer, features []*Feature) []*Transfer` — Filter transfers to those matching enabled features
- `GetTransfersForFeature(transfers []*Transfer, featureName string) []*Transfer` — Get transfers associated with a specific feature by membership in `Features` or `RequisiteFeatures`; this is association lookup, not full active-transfer filtering
- `GetEnabledFeatureNames(features []*Feature) []string`
- `IsFeatureEnabled(features []*Feature, name string) bool`

**Component discovery** (`config/component.go`; see `docs/design/overview.md` "Components" for the full design):

- `SearchRoots` — Package variable: `[]string{"/etc", "/run", "/usr/local/lib", "/usr/lib"}`, in priority order. Overridable in tests (same pattern as `sysext.SysextDir`; the exported-var pattern is recorded in [ADR-0009](../adr/0009-overridable-system-path-vars.md)).
- `SearchRootIndex(path string) (int, bool)` — Index into `SearchRoots` of the root containing `path` (most specific wins, whole-component match so `/usr/libfoo` misses `/usr/lib`), `(-1, false)` when outside all of them. Returns the index, not the directory, because tests override `SearchRoots` with temp dirs. Used by `updex.featureOrigin` to classify a feature's provenance.
- `OSReleasePaths` — Package variable: `[]string{"/etc/os-release", "/usr/lib/os-release"}`, first readable wins. Overridable in tests.
- `ImageName() string` — Identifier for the running OS image: first non-empty of `VARIANT_ID` (ublue-os images, Fedora variants), `IMAGE_ID` (frostyard/snosi images), `ID` (fallback); `""` if none. Order matters: on ucore `IMAGE_ID` is unset and `ID=fedora`, which would collide with the `fedora` catalog name, while `VARIANT_ID=ucore` is correct.
- `ComponentSearchPaths(name string) []string` — The four search-path directories for a component (`""` = legacy default `sysupdate.d/`).
- `EtcComponentDir(name string) string` — The `/etc` override directory for a component's drop-ins (`""` = `/etc/sysupdate.d`).
- `type Component struct { Name string; SearchPaths []string }` — `SearchPaths` lists only the directories that exist on disk, in priority order.
- `DiscoverComponents() ([]Component, error)` — Scan `SearchRoots` for `sysupdate.<name>.d/` directories (`[a-zA-Z0-9_-]+` names; dotted/empty names ignored), sorted by name. Does not include the legacy default component.
- `LoadComponentFeatures(name string) ([]*Feature, error)` / `LoadComponentTransfers(name string) ([]*Transfer, error)` — Load one named component (`""` = legacy default), following its own search-path precedence.
- `LoadAllFeatures(customPath string) ([]*Feature, []string, error)` / `LoadAllTransfers(customPath string) ([]*Transfer, []string, error)` — Load the union of the legacy default directory and every discovered component; returns collision-warning strings alongside the result. `customPath != ""` bypasses discovery and behaves like the plain `Load*(customPath)` functions (`LoadAllTransfers` additionally applies `FilterSysextTransfers` in this case). `LoadAllTransfers` always applies `FilterSysextTransfers` to every source before merging.
- `IsSysextTransfer(t *Transfer) bool` — `true` for a `url-file`-sourced transfer whose target is empty-or-`regular-file` with no `PathRelativeTo` set.
- `FilterSysextTransfers(transfers []*Transfer) []*Transfer` — Keep only `IsSysextTransfer` matches.
- `ComponentOfPath(path string) (name string, ok bool)` — Recover the component name from a loaded `Feature`/`Transfer`'s `FilePath` (its parent directory). `ok=false` for the legacy default directory or a `-C`/`Definitions` override directory.

### `catalog`

Sysext catalog primitives; no built-in repos (see `docs/design/overview.md` "Catalogs").

- `ConfigRoots` — Package variable: the four `*/updex/catalogs.d` directories scanned for `<name>.catalog` files, earlier roots winning per filename. Overridable in tests.
- `LoadRepos() ([]Repo, error)` — Load configured repos, sorted by name; returns `ErrNoCatalogs` when none exist.
- `RepoByName(repos []Repo, name string) (Repo, bool)`
- `type Repo struct { Name, SiteURL, ListURL, Component string; AllowInsecure bool }` — `Component` defaults to `catalog-<name>`; both names validated against `[a-zA-Z0-9_-]+`. Parsed `.catalog` files require absolute HTTPS `SiteURL`/`ListURL` values unless `AllowInsecure=yes`; the opt-in is intended only for trusted development and test endpoints.
- `List(ctx, *http.Client, Repo) ([]string, error)` — Enumerate sysexts via the repo's `ListURL` (GitHub contents API shape): top-level `dir` entries minus dotted names and `docs`/`LICENSES`. Sends `GITHUB_TOKEN` as a bearer token only to the `https://api.github.com` origin and strips authorization from redirects to other origins. Always live; no cache.
- `CachedList(ctx, *http.Client, Repo, CachedListOptions) ([]string, CacheResult, error)` — `List` behind a per-repo TTL+ETag cache in `CacheDir` (default `os.UserCacheDir()/updex`, empty disables). `CachedListOptions{TTL /* 0 → DefaultListCacheTTL (60 min) */, NoCache}`; `CacheResult{FromCache, Stale, Age}`. Validates `Repo.Name` (public API: the name becomes a cache filename). Within TTL: cache, zero network. Expired: conditional GET (`If-None-Match`; 304 bumps the timestamp, rate-limit-free on GitHub). Fetch failure with an entry present: stale served, `Stale: true` — except `context.Canceled`/`DeadlineExceeded`, which propagate. Entries are invalidated when the repo's `ListURL` changes; corrupt files are misses; writes are best-effort.
- `FetchConf(ctx, *http.Client, Repo, name) ([]byte, error)` — GET `<SiteURL>/<name>/<name>.conf`; 404 wraps `ErrNotFound`. Validates `name` first.
- `RenderTransfer(conf []byte, repo Repo, name string) ([]byte, error)` — Byte-preserving line transform ([ADR-0006](../adr/0006-byte-preserving-render-transfer.md)): prepend the `GeneratedMarker` header, inject `Features=<name>` after `[Transfer]` (appending the section if missing, replacing an existing `Features` key), drop `Target CurrentSymlink`, keep `%w`/`%a` specifiers unexpanded. Validates `[Source]`/`[Target]` presence via `ini.Load`.
- `RenderFeature(Repo, name) []byte` — `GeneratedMarker` header plus `[Feature]` stanza with `Description`, `Documentation=<SiteURL>/<name>/`, and `Enabled=false` (enabling goes through the standard drop-in).
- `GeneratedMarker` / `IsGenerated(data []byte) bool` / `IsGeneratedFile(path string) bool` — Ownership signal for generated files: the header `# Generated by updex catalog (repo: <name>); ...` ([ADR-0003](../adr/0003-catalog-ownership-marker.md)).
- `GeneratedRepo(data []byte) (repo string, ok bool)` / `GeneratedFileRepo(path string) (repo string, ok bool)` — Parse the generating repo out of the marker. `CatalogAdd`/`CatalogRemove` compare this against the acting repo, so neither a foreign file nor another catalog sharing the same `Component` can be overwritten or deleted.
- `ValidateSysextName(name string) error` — Rejects names that aren't a safe single filename/URL component (`^[a-zA-Z0-9_][a-zA-Z0-9._+-]*$`).

### `manifest`

- `Fetch(ctx context.Context, httpClient *http.Client, baseURL string, verify bool, opts ...Option) (*Manifest, error)` — Fetch and parse `SHA256SUMS` from URL. If `httpClient` is nil, a default client with a 30-second timeout is used. The `SHA256SUMS` GET and body read retry transient network failures and HTTP 5xx/429 up to 3 total attempts with exponential backoff; TLS/cert errors, unsupported protocols, and 4xx other than 429 fail immediately. The detached `SHA256SUMS.gpg` fetch used when `verify=true` is not retried. `WithRetryConfig(maxAttempts int, baseDelay time.Duration)` overrides retry bounds for tests or SDK consumers; `WithRetryNotify(func(attempt, maxAttempts int, reason error))` reports retry attempts
- `VerifyHash(filePath string, expectedHash string) error` — Verify a file's SHA256
- `VerifyHashReader(r io.Reader, expectedHash string) *HashVerifyReader` — Streaming hash verification

### `download`

The retry policy shared by `download` and `manifest` (`internal/retry`) is
recorded in [ADR-0008](../adr/0008-bounded-retry-no-resume.md).

- `Download(ctx context.Context, httpClient *http.Client, url, targetPath, expectedHash string, mode uint32, onProgress ProgressFunc, opts ...Option) error` — Download with hash verification (on compressed bytes) and auto-decompression. Uses atomic rename, and on every path fsyncs the file before renaming it into place: the verified temp file is synced before it is closed and renamed, a decompressed output file is synced before it is renamed, and on cross-device rename failure the copy through a temp file on the destination device is synced, chmodded, then renamed into place. A sync failure is returned wrapped and leaves the target path untouched. If `httpClient` is nil, a default client with a 10-minute timeout is used. Default mode: `0644` if `mode == 0`. GETs and response-body reads retry transient network failures and HTTP 5xx/429 up to 3 total attempts with exponential backoff; each retry re-requests the file from scratch and uses a fresh temp file. 4xx other than 429 and checksum mismatches fail immediately. `WithRetryConfig(maxAttempts int, baseDelay time.Duration)` overrides retry bounds for tests or SDK consumers; `WithRetryNotify(func(attempt, maxAttempts int, reason error))` reports retry attempts
- `ProgressFunc` — `func(contentLength int64) io.Writer` callback type for download progress. It may be called once per retry attempt, and should return a fresh independent writer each time to avoid double-counting
- `DecompressReader(r io.Reader, compressionType string) (io.ReadCloser, error)` — Returns a decompressing reader for `"xz"`, `"gz"`, `"zstd"`, or passthrough for `""`
- `StripCompressionSuffix(filename string) string` — Removes a trailing `.xz`/`.gz`/`.zst`/`.zstd` suffix (case-insensitive, longest suffix first). `Download` always stores files decompressed, so installed filenames are derived with this to keep the name consistent with the content

### `version`

- `ParsePattern(pattern string) (*Pattern, error)` — Parse `@v`-style patterns. Returns `ErrEmptyPattern` or `ErrMissingVersionPlaceholder` on invalid input
- `ParsePatterns(patternStrs []string) ([]*Pattern, error)` — Parse multiple patterns; returns all successfully parsed patterns and the first error encountered (callers proceed if at least one pattern parsed)
- `ExtractVersionParsed(filename string, patterns []*Pattern) (version, matchedPattern string, ok bool)` — Try pre-parsed patterns against a filename (preferred for loops)
- `Compare(v1, v2 string) int` — Version comparison (-1, 0, 1); uses dpkg-compatible ordering for Debian-style versions containing `:`, `~`, or `+` (semver would ignore everything after `+` as build metadata, collapsing dpkg-derived versions to equal), otherwise normalizes `v`/`V` prefixes and uses semantic comparison with string fallback
- `Sort(versions []string)` — Sort descending (newest first)

**`Pattern` methods:**
- `ExtractVersion(filename string) (string, bool)` — Extract version from a single filename
- `Matches(filename string) bool` — Test if filename matches the pattern
- `BuildFilename(version string) string` — Construct filename from a version string
- `Raw() string` — Return the original pattern string

### `sysext`

- `SysextRunner` interface — `Refresh()`, `Merge()`, `Unmerge()`, `LinkToSysext(*config.Transfer)` methods executed via `DefaultRunner` (real commands) or `MockRunner` (tests)
- `GetInstalledVersions(t *config.Transfer) ([]string, string, error)` — List installed + current version
- `GetActiveVersion(t *config.Transfer) (string, error)` — Get the version considered active by updex: first a legacy `CurrentSymlink`, then an image name in `RunExtensionsDir` (`/run/extensions`)
- `GetActiveVersionIn(t *config.Transfer, defaultDir, runExtensionsDir string) (string, error)` — Explicit-directory variant used by `updex.Client`; the sysext link directory (`/var/lib/extensions`) is only the fallback for locating a legacy `CurrentSymlink`, not evidence that an image is merged
- `SysextLinkName(t *config.Transfer) string` — Derive the sysext-visible link name from `Transfer.Component` plus the target pattern extension after stripping compression suffixes, e.g. `foo.transfer` and `foo_@v.raw.xz` produce `foo.raw`
- `RemoveLegacyCurrentSymlink(t *config.Transfer) error` — Remove a staging `CurrentSymlink` only when the transfer defines one; absent directives and missing symlink files are no-ops
- `LinkToSysext(t *config.Transfer) / UnlinkFromSysext(t *config.Transfer)` — Manage `/var/lib/extensions/<component>.<ext>` symlinks without requiring `CurrentSymlink`. `LinkToSysext` scans staged versioned files, selects the newest by `version.Compare`, and points the sysext-visible link at that file
- `PlanVacuumAfterInstall(t *config.Transfer, activeVersion string) ([]string, []string, error)` — Preview vacuum removals/kept versions after installing a version without deleting files
- `Vacuum(t *config.Transfer) / VacuumWithDetails(t *config.Transfer)` — Clean old versions while keeping the active symlink target and `ProtectVersion`
- `RemoveAllVersions(t *config.Transfer) ([]string, error)` — Remove all versions and current symlink for a component
- `GetExtensionName(filename string) string` — Extract extension name from filename (strips version and compression suffixes)
- `SysextDir` — Package variable: `/var/lib/extensions`
- `RunExtensionsDir` — Production merged-image state directory constant: `/run/extensions`

### `systemd`

- `NewManager() *Manager` — Create manager with default paths (`/etc/systemd/system`)
- `NewTestManager(unitPath string, runner SystemctlRunner) *Manager` — Create manager with custom paths and runner for testing
- `GenerateTimer(cfg *TimerConfig) string` — Generate systemd timer unit content
- `GenerateService(cfg *ServiceConfig) string` — Generate systemd service unit content
- `Manager.Install(timer, service) / Remove(name) / Exists(name)` — Unit lifecycle
- `SystemctlRunner` interface — `DaemonReload()`, `Enable(unit)`, `Disable(unit)`, `Start(unit)`, `Stop(unit)`, `IsActive(unit)`, `IsEnabled(unit)` methods executed via `DefaultSystemctlRunner` (real commands) or `MockSystemctlRunner` (tests)
