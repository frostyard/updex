# Internal Packages

## config — Configuration Parser

**Files:** `internal/config/feature.go`, `internal/config/transfer.go`

Loads and parses `.transfer` and `.feature` INI files from systemd-style
search paths with drop-in directory support and systemd specifier expansion.

### Key Types

**Transfer** — parsed transfer configuration:
- `Component` (string) — derived from filename (e.g., `myext` from `myext.transfer`)
- `FilePath` (string) — source config file path
- `Transfer` (TransferSection) — `MinVersion`, `ProtectVersion`, `Verify`, `InstancesMax`, `Features`, `RequisiteFeatures`
- `Source` (SourceSection) — `Type`, `Path`, `MatchPattern`, `MatchPatterns`
- `Target` (TargetSection) — `Type`, `Path`, `MatchPattern`, `MatchPatterns`, `CurrentSymlink`, `Mode`, `ReadOnly`

**Feature** — parsed feature configuration:
- `Name`, `FilePath`, `Description`, `Documentation`, `AppStream`
- `Enabled` (bool), `Masked` (bool)
- `Transfers` ([]string) — component names derived from `Features`/`RequisiteFeatures` mapping

### Key Functions

- `LoadTransfers(paths ...string)` — loads all `.transfer` files from search paths
- `LoadFeatures(paths ...string)` — loads all `.feature` files from search paths
- `FilterTransfersByFeatures(transfers, features)` — returns transfers matching enabled features
- `GetTransfersForFeature(transfers, featureName)` — returns transfers for a specific feature
- `expandSpecifiers(s)` — expands `%w` (VERSION_ID), `%a` (architecture), etc.

### Drop-in Support

For a file `foo.transfer`, settings can be overridden by files in
`foo.transfer.d/*.conf`. Drop-ins are loaded in alphabetical order;
later values override earlier ones. The SDK uses
`/etc/sysupdate.d/{name}.feature.d/00-updex.conf` to enable/disable
features.

---

## download — HTTP Downloads

**Files:** `internal/download/download.go`, `internal/download/decompress.go`

Downloads files from URLs with SHA256 hash verification, automatic
decompression, and atomic file writes.

### Key Functions

- `Download(ctx, url, destPath, expectedHash, reporter)` — main entry point:
  1. Creates temp file in destination directory
  2. Downloads with progress reporting
  3. Verifies SHA256 hash
  4. Decompresses if needed (detected from filename extension)
  5. Atomically renames to destination (falls back to copy for cross-device)

- `DecompressReader(r, filename)` — returns streaming decompression reader
  based on file extension (`.xz`, `.gz`, `.zst`/`.zstd`)

### Hash Verification

`HashVerifyReader` wraps an `io.Reader`, computing SHA256 incrementally
during read. After the full content is consumed, `Verify(expected)` checks
the computed hash matches.

---

## manifest — SHA256SUMS Fetching

**Files:** `internal/manifest/manifest.go`, `internal/manifest/gpg.go`

Fetches and parses SHA256SUMS manifests from remote URLs with optional
GPG signature verification.

### Key Types

**Manifest** — parsed manifest:
- `URL` (string) — source URL
- `Entries` (map[string]string) — filename → SHA256 hash

### Key Functions

- `Fetch(ctx, baseURL, verify)` — downloads `{baseURL}/SHA256SUMS`, parses
  entries, optionally verifies `{baseURL}/SHA256SUMS.gpg` signature
- `VerifyHash(filePath, expectedHash)` — verify a file's SHA256
- `VerifyHashReader(r, expectedHash)` — verify a stream's SHA256

### GPG Verification

When `verify=true`:
1. Downloads `SHA256SUMS.gpg` alongside the manifest
2. Loads system keyring from `/etc/systemd/import-pubring.gpg` or
   `/usr/lib/systemd/import-pubring.gpg`
3. Verifies signature using `golang.org/x/crypto/openpgp`

---

## sysext — systemd-sysext Integration

**Files:** `internal/sysext/manager.go`, `internal/sysext/runner.go`, `internal/sysext/mock_runner.go`

Manages systemd-sysext extension lifecycle: querying installed versions,
updating symlinks, vacuuming old versions, and triggering merge/unmerge/refresh.

### Key Interface

```go
type SysextRunner interface {
    Refresh(ctx) error
    Merge(ctx) error
    Unmerge(ctx) error
    List(ctx) (string, error)
}
```

`DefaultRunner` executes real `systemd-sysext` commands.
`MockRunner` records calls for testing and returns configured errors.

### Key Functions

- `GetInstalledVersions(transfer)` — lists versions in target path,
  identifies current version from symlink or newest file
- `GetActiveVersion(transfer)` — checks if extension is currently active
  (merged) via symlink or `/run/extensions`
- `UpdateSymlink(transfer, version)` — creates/updates `CurrentSymlink`
  pointing to the specified version
- `LinkToSysext(transfer, version)` / `UnlinkFromSysext(transfer)` —
  manages symlinks in `/var/lib/extensions`
- `Vacuum(transfer)` / `VacuumWithDetails(transfer)` — removes old
  versions keeping `InstancesMax`, protects `ProtectVersion`
- `RemoveAllVersions(transfer)` — removes all files and symlinks
- `Refresh(ctx, runner)` / `Merge(ctx, runner)` / `Unmerge(ctx, runner)` —
  delegates to `SysextRunner`
- `GetExtensionName(filename)` — extracts extension name by stripping
  version suffix and known extensions (`.raw`, `.raw.xz`, etc.)

### Extension Directory

Extensions are symlinked into `/var/lib/extensions/{name}` where
systemd-sysext discovers them. The symlink points to the actual file
in the transfer's target path.

---

## systemd — Timer/Service Management

**Files:** `internal/systemd/manager.go`, `internal/systemd/runner.go`, `internal/systemd/unit.go`

Generates and manages systemd timer+service units for scheduling automatic
updates via the `daemon` CLI commands.

### Key Types

```go
type TimerConfig struct {
    Name, Description string
    OnCalendar        string // e.g., "daily"
    Persistent        bool
    RandomDelaySec    string
}

type ServiceConfig struct {
    Name, Description string
    ExecStart         string
    Type              string // e.g., "oneshot"
}
```

### Key Functions

- `GenerateTimer(cfg)` / `GenerateService(cfg)` — produce unit file content
- `Manager.Install(timerCfg, serviceCfg)` — writes unit files, runs
  `daemon-reload`, enables and starts timer
- `Manager.Remove(name)` — stops timer, disables, removes files,
  runs `daemon-reload`
- `Manager.Exists(name)` — checks if timer or service file exists

### SystemctlRunner Interface

```go
type SystemctlRunner interface {
    DaemonReload(ctx) error
    Enable(ctx, unit) error
    Disable(ctx, unit) error
    Start(ctx, unit) error
    Stop(ctx, unit) error
    IsActive(ctx, unit) (bool, error)
    IsEnabled(ctx, unit) (bool, error)
}
```

Unit files are written to `/etc/systemd/system/`.

---

## version — Pattern Matching & Comparison

**Files:** `internal/version/pattern.go`

Parses version patterns with `@v` and other placeholders, extracts
versions from filenames, and compares versions semantically.

### Key Types

**Pattern** — compiled pattern with regex and template:
- Created via `ParsePattern(s)` which validates `@v` is present
- Placeholders are converted to regex capture groups

### Placeholders

| Placeholder | Meaning |
|-------------|---------|
| `@v` | Version (required) |
| `@u` | UUID |
| `@a` | GPT NoAuto flag |
| `@g` | GrowFileSystem flag |
| `@r` | Read-only flag |
| `@t` | Timestamp |
| `@m` | File mode |
| `@s` | File size |
| `@d` | Tries done |
| `@l` | Tries left |
| `@h` | SHA256 hash |

### Key Functions

- `ParsePattern(s)` — compiles pattern to regex, validates `@v` present
- `Pattern.ExtractVersion(filename)` — returns version string or empty
- `Pattern.Matches(filename)` — checks if filename matches pattern
- `Pattern.BuildFilename(version)` — constructs filename from template
- `ExtractVersionMulti(filename, patterns)` — tries multiple patterns
- `Compare(a, b)` — semantic comparison via `hashicorp/go-version`,
  string fallback; returns -1, 0, or 1
- `Sort(versions)` — sorts descending (newest first)

---

## testutil — Test Helpers

**Files:** `internal/testutil/httpserver.go`

Provides `NewHTTPServer(t, files)` which creates an `httptest.Server`
serving a map of path → content. Used by download and manifest tests.
