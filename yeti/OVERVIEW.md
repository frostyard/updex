# updex Documentation

## Purpose

updex is a Go SDK and CLI for managing [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html) images. It replicates `systemd-sysupdate` functionality for `url-file` transfers, providing feature-based management of system extensions with version tracking, SHA256 verification, optional GPG signing, and automatic cleanup.

The project follows an **SDK-first design** for feature management: the core workflows live in public Go packages, and the CLI is mostly a thin wrapper that parses flags and formats output. The `daemon` command is the main exception: it imports the `systemd` package directly to install/remove timer units because daemon lifecycle is systemd-unit management rather than feature update logic.

## Architecture

```
cmd/updex-cli/main.go          Entry point (frostyard/clix bootstrap)
cmd/updex/root.go               Cobra root command, global flags
cmd/updex/features.go           features list|enable|disable|update|check
cmd/updex/features_run.go       Run functions for feature subcommands
cmd/updex/components.go         components (list discovered systemd-sysupdate components)
cmd/updex/daemon.go             daemon enable|disable|status (direct systemd timers)
cmd/updex/client.go             CLI → SDK client factory

updex/                          Public SDK (Client + methods)
  updex.go                      Client struct, NewClient()
  features.go                   Features(), EnableFeature(), DisableFeature(),
                                UpdateFeatures(), CheckFeatures(),
                                writeFeatureDropIn() helper, lookupFeature() helper
  domain.go                     loadDomain() — resolves the feature/transfer
                                domain for every SDK method (Definitions
                                override vs. one component vs. the default
                                union); Components(), ComponentInfo, FeaturesOptions
  install.go                    installTransfer() — complete install pipeline
                                (download, symlink, sysext link, refresh, vacuum)
                                Reuses parsed patterns from getAvailableVersions
  list.go                       getAvailableVersions() — returns versions,
                                manifest, and parsed patterns for caller reuse
  options.go                    Option structs for all operations (each
                                feature-related struct carries Component string)
  results.go                    Result structs for all operations

config/                         .transfer and .feature INI file parsing,
                                search paths, drop-ins, and specifiers
config/component.go             systemd-sysupdate component discovery
                                (SearchRoots, ComponentSearchPaths,
                                DiscoverComponents, ComponentOfPath,
                                EtcComponentDir) — see "Components" below
download/                       HTTP download with SHA256 + decompression
manifest/                       SHA256SUMS manifest fetch/parse + GPG verify
version/                        Pattern matching (@v placeholder) + version compare
sysext/                         systemd-sysext runner, extension symlinks,
                                installed/active version discovery, vacuum planning
systemd/                        systemd timer/service generation + systemctl management
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
- The feature SDK methods (`UpdateFeatures`, `CheckFeatures`, enable/disable with `Now`) use `config.GetTransfersForFeature`, which includes transfers where the feature appears in either `Features` or `RequisiteFeatures`. The more general `config.FilterTransfersByFeatures` implements full active-transfer logic, including standalone transfers and AND/OR feature requirements, but it is not the main path for current feature update/check workflows.
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

- `UpdateFeaturesOptions.DryRun` is threaded through `UpdateFeatures` into `installTransfer`, which is the choke point before downloads, legacy staging-symlink cleanup, `/var/lib/extensions` linking, refresh, and vacuum deletion
- Update dry-runs still perform read-only work: load configs, fetch manifests, resolve versions, inspect installed files, and, unless `NoVacuum` is set, call `sysext.PlanVacuumAfterInstall` to populate `UpdateResult.RemovedVersions`
- In update dry-run results, `Downloaded=true` means "would download", `Installed=false` means no install occurred, and `DryRun=true` disambiguates the status for JSON consumers
- In non-dry-run update results, `Downloaded=true` means a new file was fetched and installed. `Installed=true` is also set for already-current components, so use `Downloaded` to distinguish "changed" from "already up to date".
- Enable/disable dry-runs are lighter previews: enabling with `--now` lists associated transfer components without manifest/version resolution, while disabling with `--now` performs active-version checks but records component-level "would remove" entries rather than enumerating every file

### Public API (Issue #13)

All core packages (`config`, `version`, `download`, `manifest`, `sysext`, `systemd`) are exported as public API at `github.com/frostyard/updex/<package>`. Only `internal/testutil` remains internal. This was an intentional decision: the types in these packages (e.g., `Transfer`, `Feature`, `Pattern`, `Manifest`) were designed with exported fields and are suitable for external consumption.

### Version and pattern conventions

- Every match pattern must contain `@v`; other `@` placeholders match UUIDs, flags, file metadata, and hashes but are not substituted when building target filenames
- `.transfer` `MatchPattern` fields may contain multiple space-separated alternatives; the first is preserved in `MatchPattern`, while all alternatives are available via `Patterns()`
- `%` specifiers are expanded at parse time for `Source.MatchPattern`, `Target.MatchPattern`, and `Transfer.ProtectVersion` with a cached context per `LoadTransfers` call. `Source.Path`, `Target.Path`, and `CurrentSymlink` are not currently specifier-expanded.
- `version.Compare` uses `hashicorp/go-version` for normal semver-like versions, but routes Debian/dpkg-looking versions containing `:`, `~`, or `+` through a dpkg-compatible comparator so epochs and tildes sort correctly. `+` is routed because semver ignores everything after it as build metadata, which collapses dpkg-derived versions like `1+7.2-debian13-<timestamp>` (epoch encoded as `+` in filename-safe sysext image names) to equal precedence

## Configuration

### Search paths (priority order)

1. `/etc/sysupdate.d/` (highest priority)
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

Only the first occurrence of a given filename is used. The `-C` flag overrides all search paths with a custom directory.

### Components (`config/component.go`)

A systemd-sysupdate "component" (sysupdate.d(5) "Components") is a named
grouping of `.transfer`/`.feature` files under `sysupdate.<name>.d/`,
searched across the same four roots (`config.SearchRoots`, a package var —
overridable in tests the same way `sysext.SysextDir` is) with the same
priority order as the legacy default `sysupdate.d/` directory. This exists
because native OS images now put A/B partition and UKI transfers in the
default directory (see "Non-sysext transfers" below), and package-versioned
sysext transfers must not share that single systemd-sysupdate version-lock
scope — moving a sysext's files to its own `sysupdate.<name>.d/` gives it an
independent versioning scope. `<name>` must match `[a-zA-Z0-9_-]+`;
dotted/empty names are ignored (not valid components).

Key `config` functions:

- `DiscoverComponents()` — scans `SearchRoots` for `sysupdate.<name>.d/`
  directories, returns them sorted by name (does **not** include the legacy
  default component; `SearchPaths` on each result lists only the
  directories that actually exist, in priority order).
- `ComponentSearchPaths(name)` — the four search-path directories for a
  component (`""` = legacy default). `LoadComponentFeatures(name)` /
  `LoadComponentTransfers(name)` load exactly one component this way.
- `LoadAllFeatures(customPath)` / `LoadAllTransfers(customPath)` — the
  **default read domain**: union of the legacy default directory and every
  discovered component. A name collision (feature or transfer name defined
  by more than one source) resolves to the most specific source — a named
  component beats the legacy default directory, and among colliding
  components the alphabetically last one wins — and is returned as a
  warning string (not an error) for the caller to log. `customPath != ""`
  bypasses discovery entirely and behaves like plain
  `LoadFeatures`/`LoadTransfers(customPath)` (mirrors the `-C`/`--definitions`
  override semantics: one explicit flat directory, no component concept).
- `IsSysextTransfer(t)` / `FilterSysextTransfers(transfers)` — see
  "Non-sysext transfers" below. `LoadAllTransfers` always applies this
  filter; the plain `LoadTransfers`/`LoadComponentTransfers` loaders do not.
- `ComponentOfPath(path)` — recovers the component name from a loaded
  `Feature.FilePath`'s parent directory (`false` for the legacy default or a
  `-C` override directory). `EtcComponentDir(name)` is the inverse: the
  `/etc` override directory to write to for a given component.

`updex.Client.loadDomain(component string)` in `updex/domain.go` is the
single place every SDK method resolves its read domain from, in this order:
`ClientConfig.Definitions` set → that one directory verbatim (`component`
must be empty, else an error — the two are mutually exclusive); `component`
non-empty → `LoadComponentFeatures`/`LoadComponentTransfers(component)`;
otherwise → `LoadAllFeatures("")`/`LoadAllTransfers("")`, with any collision
warnings routed through `c.warn` (the client's reporter). Every
`*FeatureOptions`/`FeaturesOptions` struct carries `Component string` for
this — extend an options struct for new component-scoped operations, never
add package-level flag state to the SDK (see uber-go/CLAUDE.md conventions).

`updex.Client.writeFeatureDropIn` uses `config.ComponentOfPath(f.FilePath)`
to pick the drop-in directory: a feature discovered under a component writes
to `EtcComponentDir(name)` (`/etc/sysupdate.<name>.d/<feature>.feature.d/`);
everything else (legacy default or `-C` override) keeps the original
`/etc/sysupdate.d/<feature>.feature.d/` path. Because `LoadComponentFeatures`
reads drop-ins from the same component-scoped search paths on read, writes
and reads always agree on scope without any extra bookkeeping.

`updex.Client.Components(ctx)` (SDK) / `updex components` (CLI) list
discovered components — name, highest-priority existing source directory,
and that component's own feature count (not counting union collisions) —
via `config.DiscoverComponents` + `LoadComponentFeatures` per component. It
does not include the legacy default component; use `Features` with the
default (empty) `Component` to see the full union, including anything still
defined there.

### Non-sysext transfers

The legacy default `sysupdate.d/` directory on native (bootc A/B) images
also carries the OS's own transfers, which are not sysext-shaped and which
`config.FilterSysextTransfers` (used by `LoadAllTransfers`) silently drops
rather than erroring on:

- **A/B root partitions**: `[Target] Type=partition` (`MatchPartitionType=root`
  / `root-verity`), `Path=auto`.
- **UKI**: `[Target] Type=regular-file`, `Path=/EFI/Linux`,
  `PathRelativeTo=boot` — the `PathRelativeTo` key (parsed into
  `TargetSection.PathRelativeTo`) is the discriminator that separates this
  from a genuine sysext regular-file target, since both have
  `Type=regular-file`.

`IsSysextTransfer(t)` requires `Source.Type == "url-file"`, `Target.Type`
empty-or-`"regular-file"`, and `Target.PathRelativeTo == ""`. Empty
`Target.Type` is treated as `regular-file` (not filtered) to match every
existing sysext `.transfer` fixture in this repo, which never sets `Type=`
explicitly in `[Target]`.

### File types

See [Configuration Reference](config-reference.md) for detailed format documentation.

- **`.feature`** files define features (name, description, enabled state)
- **`.transfer`** files define how components are downloaded and installed
- **`.feature.d/`** drop-in directories override feature settings (applied alphabetically)
- Masked feature files are symlinks to `/dev/null`. `LoadFeatures` still returns a masked feature entry, with `Enabled=false` and `Masked=true`, so list output can show it as masked while mutating SDK calls reject it.

### Key transfer settings

| Setting | Section | Default | Description |
|---------|---------|---------|-------------|
| `InstancesMax` | `[Transfer]` | `2` | Max versions to keep on disk |
| `ProtectVersion` | `[Transfer]` | — | Version that is never removed |
| `MinVersion` | `[Transfer]` | — | Minimum version to consider |
| `Verify` | `[Transfer]` | `false` | Require GPG signature verification |
| `Features` | `[Transfer]` | — | OR list: any enabled feature activates this transfer |
| `RequisiteFeatures` | `[Transfer]` | — | AND list: all must be enabled |
| `CurrentSymlink` | `[Target]` | — | Optional legacy staging symlink; when present, update removes it |

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

1. Load all `.feature` and `.transfer` files: by default the union of the legacy default directory and every discovered component (`Client.loadDomain`, see "Components" above), or a single scope when `--component`/`-C` narrows it. Non-sysext transfers (A/B partition, UKI) are filtered out of the default union before this point.
2. Filter transfers to those matching enabled features
3. For each transfer:
   - Fetch `SHA256SUMS` manifest from source URL (+ GPG verify if configured); transient network failures during request or body read and HTTP 5xx/429 are retried up to 3 attempts with exponential backoff, while TLS/cert errors, unsupported protocols, 4xx other than 429, and checksum mismatches fail immediately. Manifests are cached by source URL across transfers so that multiple transfers sharing the same source make only one HTTP request
   - The manifest cache key is only the source URL path. Verification is decided during the first fetch for that path; avoid relying on mixed per-transfer `Verify` settings for one shared source URL unless the cache behavior is changed.
   - Parse source patterns and extract available versions using pattern matching (`@v` placeholder); parsed patterns are returned to callers so `installTransfer` reuses them without re-parsing. The candidate list is returned lexically sorted so that, with the stable `version.Sort`, selection stays deterministic even if two versions compare equal
   - Select newest version via `version.Sort` (semver where possible, Debian/dpkg ordering for versions with `:`, `~`, or `+`, string fallback otherwise)
   - Skip if already installed (check target directory)
   - Download file, retrying the same transient request/body-read failures and HTTP 5xx/429 from scratch without range/resume requests. Each attempt uses a new temp file and invokes `OnDownloadProgress` again, so progress writers must be attempt-local. SHA256 is verified against the compressed bytes before decompression.
   - Decompress if needed (xz, gz, zstd — detected from filename). The installed filename is derived from the target patterns via `buildTargetFilename`: the first pattern that produces a name without a compression suffix wins, and if every target pattern is a compressed variant the suffix is stripped, so the on-disk name always matches the decompressed content regardless of which source pattern matched
   - Atomically rename to final path; on cross-device rename failure, copy to a temp file on the destination filesystem, sync it, chmod it, then rename
   - Remove any legacy `CurrentSymlink` in the target directory when the transfer defines one. Current-version detection still reads the legacy symlink first so an installed-but-not-current newer image is not hidden by cleanup; cleanup then runs before any already-current return, so stale staging symlinks are removed even when no download is required.
   - Create or replace `/var/lib/extensions/<component>.<ext>` pointing to the newest staged image path; the link name is derived from the transfer filename component and the target pattern extension with compression suffixes stripped. This is a hard error because `systemd-sysext refresh` cannot see the staged image without it
   - Vacuum old versions per `InstancesMax`; the active symlink target and `ProtectVersion` are always kept. Non-dry-run `UpdateResult.RemovedVersions` is not populated because the install path calls `sysext.Vacuum`, while dry-run uses `PlanVacuumAfterInstall`
4. Call `systemd-sysext refresh` to reload all extensions (unless `--no-refresh`). Callers batch this — `installTransfer` is called with `NoRefresh: true` per-component, and a single refresh runs at the end. With `--dry-run`, the same manifest/version resolution runs, but `installTransfer` returns before download; `UpdateFeatures` reports would-download/would-install results and read-only vacuum removals, then skips the final refresh.

### Enable/disable feature

- **Enable**: Creates drop-in at `/etc/sysupdate.d/<name>.feature.d/00-updex.conf` (or `/etc/sysupdate.<component>.d/<name>.feature.d/00-updex.conf` for a component-scoped feature — see "Components" above) setting `Enabled=true`. With `--now`, also downloads extensions immediately.
- **Disable**: Creates drop-in setting `Enabled=false` at the same scoped path. With `--now`, calls `Unmerge()`, removes symlinks from `/var/lib/extensions/`, and deletes all versioned files. `--force` required if extensions are currently active/merged (changes take effect after reboot).

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
  --component <name>                    Scope any features subcommand above to one
                                         named component (default: default-dir + every
                                         discovered component); persistent flag on
                                         `updex features`, mutually exclusive with -C

updex components                        List discovered systemd-sysupdate components
                                         (name, source dir, feature count)

updex daemon enable                     Install daily auto-update timer
updex daemon disable                    Remove auto-update timer
updex daemon status                     Show timer status

Global flags:
  -C, --definitions <path>              Custom path to config files (bypasses component
                                         discovery entirely; mutually exclusive with --component)
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
