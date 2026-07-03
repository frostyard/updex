# updex Documentation

## Purpose

updex is a Go SDK and CLI for managing [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html) images. It replicates `systemd-sysupdate` functionality for `url-file` transfers, providing feature-based management of system extensions with version tracking, SHA256 verification, optional GPG signing, and automatic cleanup.

The project follows an **SDK-first design** for feature management: the core workflows live in public Go packages, and the CLI is mostly a thin wrapper that parses flags and formats output. The `daemon` command is the main exception: it imports the `systemd` package directly to install/remove timer units.

## Architecture

```
cmd/updex-cli/main.go          Entry point (frostyard/clix bootstrap)
cmd/updex/root.go               Cobra root command, global flags
cmd/updex/features.go           features list|enable|disable|update|check
cmd/updex/features_run.go       Run functions for feature subcommands
cmd/updex/daemon.go             daemon enable|disable|status (direct systemd timers)
cmd/updex/client.go             CLI → SDK client factory

updex/                          Public SDK (Client + methods)
  updex.go                      Client struct, NewClient()
  features.go                   Features(), EnableFeature(), DisableFeature(),
                                UpdateFeatures(), CheckFeatures(),
                                writeFeatureDropIn() helper
  install.go                    installTransfer() — complete install pipeline
                                (download, symlink, sysext link, refresh, vacuum)
                                Reuses parsed patterns from getAvailableVersions
  list.go                       getAvailableVersions() — returns versions,
                                manifest, and parsed patterns for caller reuse
  options.go                    Option structs for all operations
  results.go                    Result structs for all operations

config/                         .transfer and .feature INI file parsing,
                                search paths, drop-ins, and specifiers
download/                       HTTP download with SHA256 + decompression
manifest/                       SHA256SUMS manifest fetch/parse + GPG verify
version/                        Pattern matching (@v placeholder) + version compare
sysext/                         systemd-sysext operations (refresh/merge/vacuum)
systemd/                        systemd unit generation + systemctl management
internal/testutil/              HTTP test server helpers (module-internal)
```

### Package dependency flow

```
CLI (cmd/features*) → SDK (updex/) → config, manifest, download, version, sysext
                                  → sysext → config, version
CLI (cmd/daemon.go) → systemd (direct, bypasses SDK)
```

> Note: `cmd/updex/daemon.go` imports `systemd` directly rather than through the SDK layer. This is a known architectural deviation from the SDK-first pattern.

## Key Patterns

### SDK conventions

- All public SDK methods take `context.Context` as first parameter for cancellation
- Operations use dedicated option structs (e.g., `EnableFeatureOptions`, `UpdateFeaturesOptions`) to allow future expansion without breaking changes
- Return dedicated result structs with status fields + error
- `ClientConfig.HTTPClient` is reused for manifest fetches and downloads; if nil, `NewClient` creates one with a 10-minute timeout
- `ClientConfig.Progress` receives informational/warning/debug messages; `ClientConfig.OnDownloadProgress` is a separate download-byte callback
- `UpdateFeatures` and `CheckFeatures` cache fetched manifests by `Transfer.Source.Path` only. Future changes that mix verification policy or auth by transfer for the same source URL need to revisit that cache key.
- Error messages: lowercase, no trailing punctuation, wrapped with `fmt.Errorf("context: %w", err)`

### Testing patterns

- Mock interfaces for system commands: `sysext.SysextRunner`, `systemd.SystemctlRunner`
- `ClientConfig.SysextRunner` field for injecting mocks into the SDK client — `NewClient` stores the runner directly on the `Client` struct (does not mutate global state)
- `internal/testutil.NewTestServer()` creates `httptest.Server` with configurable manifests and file content
- `t.TempDir()` for filesystem operations, `t.Context()` for context

### CLI output

- Text tables by default, JSON with `--json` flag — both `--json` and `--dry-run` are provided by the `github.com/frostyard/clix` package, not defined in this repo
- `cmd/updex/client.go` always wires `clix.NewReporter()` and `newProgressBar`; there is no repo-defined `--quiet` flag in the current code
- Operations requiring filesystem changes call `requireRoot()` before entering the SDK. This currently includes dry-run variants of `features enable`, `features disable`, and `features update`, so dry-run is mutation-free but not rootless from the CLI.

### Dry-run behavior

- `UpdateFeaturesOptions.DryRun` is threaded through `UpdateFeatures` into `installTransfer`, which is the choke point before downloads, symlink updates, `/var/lib/extensions` linking, refresh, and vacuum deletion
- Update dry-runs still perform read-only work: load configs, fetch manifests, resolve versions, inspect installed files, and, unless `NoVacuum` is set, call `sysext.PlanVacuumAfterInstall` to populate `UpdateResult.RemovedVersions`
- In update dry-run results, `Downloaded=true` means "would download", `Installed=false` means no install occurred, and `DryRun=true` disambiguates the status for JSON consumers
- In non-dry-run update results, `Downloaded=true` means a new file was fetched and installed. `Installed=true` is also set for already-current components, so use `Downloaded` to distinguish "changed" from "already up to date".
- Enable/disable dry-runs are lighter previews: enabling with `--now` lists associated transfer components without manifest/version resolution, while disabling with `--now` performs active-version checks but records component-level "would remove" entries rather than enumerating every file

### Public API (Issue #13)

All core packages (`config`, `version`, `download`, `manifest`, `sysext`, `systemd`) are exported as public API at `github.com/frostyard/updex/<package>`. Only `internal/testutil` remains internal. This was an intentional decision: the types in these packages (e.g., `Transfer`, `Feature`, `Pattern`, `Manifest`) were designed with exported fields and are suitable for external consumption.

### Version and pattern conventions

- Every match pattern must contain `@v`; other `@` placeholders match UUIDs, flags, file metadata, and hashes but are not substituted when building target filenames
- `.transfer` `MatchPattern` fields may contain multiple space-separated alternatives; the first is preserved in `MatchPattern`, while all alternatives are available via `Patterns()`
- `%` specifiers in transfer values are expanded at parse time with a cached context per `LoadTransfers` call
- `version.Compare` uses `hashicorp/go-version` for normal semver-like versions, but routes Debian/dpkg-looking versions containing `:` or `~` through a dpkg-compatible comparator so epochs and tildes sort correctly

## Configuration

### Search paths (priority order)

1. `/etc/sysupdate.d/` (highest priority)
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

Only the first occurrence of a given filename is used. The `-C` flag overrides all search paths with a custom directory.

### File types

See [Configuration Reference](config-reference.md) for detailed format documentation.

- **`.feature`** files define features (name, description, enabled state)
- **`.transfer`** files define how components are downloaded and installed
- **`.feature.d/`** drop-in directories override feature settings (applied alphabetically)

### Key transfer settings

| Setting | Section | Default | Description |
|---------|---------|---------|-------------|
| `InstancesMax` | `[Transfer]` | `2` | Max versions to keep on disk |
| `ProtectVersion` | `[Transfer]` | — | Version that is never removed |
| `MinVersion` | `[Transfer]` | — | Minimum version to consider |
| `Verify` | `[Transfer]` | `false` | Require GPG signature verification |
| `Features` | `[Transfer]` | — | OR list: any enabled feature activates this transfer |
| `RequisiteFeatures` | `[Transfer]` | — | AND list: all must be enabled |
| `CurrentSymlink` | `[Target]` | — | Symlink name pointing to current version |

### GPG verification

When enabled, fetches `SHA256SUMS.gpg` (detached signature) and verifies against keyrings at:
1. `/etc/systemd/import-pubring.gpg`
2. `/usr/lib/systemd/import-pubring.gpg`

Uses `github.com/ProtonMail/go-crypto/openpgp` for signature verification. Supports both binary and armored keyring formats.

Only the main `SHA256SUMS` fetch has bounded retry behavior. The detached `.gpg` signature fetch is a single request in the current implementation.

### Systemd specifiers

Transfer file values support systemd-style `%` specifiers. See [Configuration Reference](config-reference.md#systemd-specifiers) for the full list.

## Data Flow

### Feature update (end-to-end)

1. Load all `.feature` and `.transfer` files from search paths
2. Filter transfers to those matching enabled features
3. For each transfer:
   - Fetch `SHA256SUMS` manifest from source URL (+ GPG verify if configured); transient network failures during request or body read and HTTP 5xx/429 are retried up to 3 attempts with exponential backoff, while TLS/cert errors, unsupported protocols, 4xx other than 429, and checksum mismatches fail immediately. Manifests are cached by source URL across transfers so that multiple transfers sharing the same source make only one HTTP request
   - The manifest cache key is only the source URL path. Verification is decided during the first fetch for that path; avoid relying on mixed per-transfer `Verify` settings for one shared source URL unless the cache behavior is changed.
   - Parse source patterns and extract available versions using pattern matching (`@v` placeholder); parsed patterns are returned to callers so `installTransfer` reuses them without re-parsing
   - Select newest version via `version.Sort` (semver where possible, Debian/dpkg ordering for versions with `:` or `~`, string fallback otherwise)
   - Skip if already installed (check target directory)
   - Download file, retrying the same transient request/body-read failures and HTTP 5xx/429 from scratch without range/resume requests, then verify SHA256 hash of compressed bytes during transfer
   - Decompress if needed (xz, gz, zstd — detected from filename)
   - Atomically rename to final path, update `CurrentSymlink`
   - Create symlink in `/var/lib/extensions/` pointing to extension
   - Vacuum old versions per `InstancesMax`; the active symlink target and `ProtectVersion` are always kept
4. Call `systemd-sysext refresh` to reload all extensions (unless `--no-refresh`). Callers batch this — `installTransfer` is called with `NoRefresh: true` per-component, and a single refresh runs at the end. With `--dry-run`, the same manifest/version resolution runs, but `installTransfer` returns before download; `UpdateFeatures` reports would-download/would-install results and read-only vacuum removals, then skips the final refresh.

### Enable/disable feature

- **Enable**: Creates drop-in at `/etc/sysupdate.d/<name>.feature.d/00-updex.conf` setting `Enabled=true`. With `--now`, also downloads extensions immediately.
- **Disable**: Creates drop-in setting `Enabled=false`. With `--now`, calls `Unmerge()`, removes symlinks from `/var/lib/extensions/`, and deletes all versioned files. `--force` required if extensions are currently active/merged (changes take effect after reboot).

### Auto-update daemon

- `updex daemon enable` installs `/etc/systemd/system/updex-update.timer` and `.service`, then enables and starts the timer
- The timer runs `daily`, is `Persistent=true`, and uses `RandomizedDelaySec=3600`
- The service command is `/usr/bin/updex features update --no-refresh`, so automatic downloads are staged and not refreshed/activated until a later refresh or reboot
- Unit installation refuses to overwrite existing timer/service files; callers must disable first

## CLI Commands

```
updex features list                     List all features with status (alias: updex feature)
updex features enable <name>            Enable a feature
  --now                                 Download extensions immediately
updex features disable <name>           Disable a feature
  --now                                 Unmerge and remove files immediately
  --force                               Allow removal of merged extensions
updex features update                   Download and install new versions
  --no-vacuum                           Skip removing old versions
  --dry-run                             Preview update work without filesystem/sysext changes
updex features check                    Check for available updates

updex daemon enable                     Install daily auto-update timer
updex daemon disable                    Remove auto-update timer
updex daemon status                     Show timer status

Global flags:
  -C, --definitions <path>              Custom path to config files
  --verify                              Enable GPG verification
  --no-refresh                          Skip systemd-sysext refresh
  --json                                Output as JSON (from clix)
  --dry-run                             Preview without modifying filesystem (from clix)
  --verbose                             Enable debug output (from clix)
```

Mutating commands enforce root before reading `--dry-run`, so examples that preview `features enable`, `features disable`, or `features update` may still need `sudo` when run through the CLI.

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/frostyard/clix` | CLI utilities (output formatting, reporters) |
| `github.com/frostyard/std` | Standard library extensions |
| `github.com/hashicorp/go-version` | Semantic version comparison |
| `github.com/schollz/progressbar/v3` | Download progress bars |
| `gopkg.in/ini.v1` | INI file parsing |
| `github.com/ulikunitz/xz` | XZ decompression |
| `github.com/klauspost/compress` | ZSTD decompression |
| `github.com/ProtonMail/go-crypto` | GPG signature verification (openpgp) |
