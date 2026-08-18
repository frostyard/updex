# updex Design Overview

## Purpose

updex is a Go SDK and CLI for managing [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html) images. It replicates `systemd-sysupdate` functionality for `url-file` transfers, providing feature-based management of system extensions with version tracking, SHA256 verification, optional GPG signing, and automatic cleanup.

The project follows an **SDK-first design** for feature management: the core workflows live in public Go packages, and the CLI is mostly a thin wrapper that parses flags and formats output. The `daemon` command is the main exception: it imports the `systemd` package directly to install/remove timer units because daemon lifecycle is systemd-unit management rather than feature update logic.

Public repository observability is indexed at `docs/metrics/README.md`. That canonical page links live CI, nightly compliance, Codecov, pull-request, AI-fix, and release evidence; defines the monthly acceptance metric consumed by `.github/auto-qa-tuning.json`; and excludes secrets, private prompts, vulnerability embargoes, and managed-host telemetry. `updex/public_metrics_contract_test.go` prevents the ACMM path, substantive signal contract, and tuning reference from drifting apart.

## Architecture

```
cmd/updex-cli/main.go          Entry point (frostyard/clix bootstrap)
cmd/updex/root.go               Cobra root command, global flags
cmd/updex/features.go           features list|enable|disable|update|check
cmd/updex/features_run.go       Run functions for feature subcommands
cmd/updex/components.go         components (list discovered systemd-sysupdate components)
cmd/updex/catalog.go            catalog list|search|add|remove ([REPO/]NAME parsing,
                                --repo/--force flags, output formatting)
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
  catalog.go                    CatalogList(), CatalogAdd(), CatalogRemove() —
                                orchestrate catalog/ primitives plus
                                EnableFeature/DisableFeature reuse

catalog/                        Sysext catalog primitives (no built-in repos):
                                *.catalog INI repo config (ConfigRoots,
                                LoadRepos, ErrNoCatalogs), List() via GitHub
                                contents API, FetchConf(), RenderTransfer()
                                (Features= injection + CurrentSymlink drop,
                                byte-preserving), RenderFeature()
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
CLI (cmd/catalog.go) → SDK (updex/catalog.go) → catalog, config
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
- `ClientConfig.Paths` (`RuntimePaths`) for supplying temp filesystem trees to a client at construction — the preferred approach for path isolation since ADR-0010; use `t.TempDir()` paths for `DefinitionRoots`, `SysextLinkDir`, `RunExtensionsDir`, `CatalogConfigRoots`, etc. Independently configured clients can run in parallel without save/mutate/restore discipline.
- Package-level compatibility variables (`config.SearchRoots`, `catalog.ConfigRoots`, `catalog.CacheDir`, `sysext.SysextDir`) remain settable for tests that have not yet migrated; mutation before client construction still works because `NewClient` reads each global once at that point.
- Transfer parsing receives the client's captured os-release paths as well as
  definition roots, so `%w`/related specifier expansion cannot cross client
  boundaries.
- `internal/testutil.NewTestServer()` creates `httptest.Server` with configurable manifests and file content
- `t.TempDir()` for filesystem operations, `t.Context()` for context
- `tests/e2e/` builds and runs the real CLI subprocess for argument, exit-code, output, custom-config, and read-only HTTP checks. `cmd/updex/integration_test.go` runs the full Cobra/clix command in-process so package search roots can point at temporary default component and catalog trees; keep mutating subprocess paths behind parser failures so CI does not require root.
- `sysext/link_test.go` pins the `/var/lib/extensions` link lifecycle through `LinkToSysextAt` (explicit dir, no global mutation): newest-by-version selection, replacing symlinks/dangling links/regular files, staging-dir symlinks ignored, `Target.Path` fallback, metadata rejection (component, patterns, `@v`), empty/missing/non-matching staging sets, a conflicting destination directory preserved on error, sysext-dir creation failure (parent is a regular file), and removal failure (read-only dir, skipped as root). Every failing case asserts no new symlink is left behind. It also pins that `DefaultRunner` implements `PathSysextRunner` and links into the explicit dir, not `SysextDir`.
- CLI handler seams: `cmd/updex` exposes two package-level test seams so mutating handlers can run rootless in-process — `getEUID` (swap for `func() int { return 0 }` to pass `requireRoot`) and `sysextRunner` (nil in production so `newClient` gets the SDK default; set to a `*sysext.MockRunner` to observe `Refresh`/`Unmerge` without executing systemd-sysext). `cmd/updex/features_mutation_test.go` uses both with temporary `config.SearchRoots` (drop-ins land under `roots[0]`), a temporary `sysext.SysextDir`, and a fake HTTP source to cover `runFeaturesEnable`/`runFeaturesDisable` end-to-end: `--now`, `--force`, `--no-refresh`, `--component`, dry-run, and text/JSON result shapes. `cmd/updex/catalog_mutation_test.go` does the same for `runCatalogAdd`/`runCatalogRemove`, additionally pointing `catalog.ConfigRoots`, `catalog.CacheDir`, and `catalog.TargetPath` at temp dirs and serving `<name>/<name>.conf`, `SHA256SUMS`, and the image from one `httptest.Server` (configure two `.catalog` files against it to exercise `[REPO/]NAME` / `--repo` disambiguation); remove cases seed the post-add state through the SDK's `CatalogAdd`. Keep JSON-shape assertions on paths that do not download — a live download also renders the terminal progress bar on stdout (#299).

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
searched across the same four roots (`config.SearchRoots` — a package var
whose value is captured immutably by each client at `NewClient` via
`ClientConfig.Paths.DefinitionRoots`; see
[ADR-0010](../adr/0010-instance-scoped-runtime-paths.md)) with the same
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
single place every SDK method resolves its read domain from (decision
recorded in [ADR-0001](../adr/0001-read-domain-resolution-via-loaddomain.md)),
in this order:
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
rather than erroring on (decision recorded in
[ADR-0002](../adr/0002-skip-non-sysext-transfers.md)):

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

See [Configuration Reference](../specs/config-reference.md) for detailed format documentation.

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
| `Verify` | `[Transfer]` | `true` | Require GPG signature verification; set false to opt out |
| `Features` | `[Transfer]` | — | OR list: any enabled feature activates this transfer |
| `RequisiteFeatures` | `[Transfer]` | — | AND list: all must be enabled |
| `CurrentSymlink` | `[Target]` | — | Optional legacy staging symlink; when present, update removes it |

### GPG verification

Enabled by default to match systemd-sysupdate. Set `Verify=no` explicitly to opt out; the client's global `Verify` setting (the CLI's `--verify` flag) forces verification even for transfers that opt out. When enabled, fetches `SHA256SUMS.gpg` (detached signature) and verifies against keyrings at:
1. `/etc/systemd/import-pubring.gpg`
2. `/usr/lib/systemd/import-pubring.gpg`

Uses `github.com/ProtonMail/go-crypto/openpgp` for signature verification. Supports both binary and armored keyring formats.

Only the main `SHA256SUMS` fetch has bounded retry behavior. The detached `.gpg` signature fetch is a single request in the current implementation. Manifest response bodies are read through a 4 MiB-plus-one-byte limit and detached signatures through a 1 MiB-plus-one-byte limit; crossing either boundary fails before parsing, keyring loading, or signature verification.

### Systemd specifiers

Transfer file values support systemd-style `%` specifiers. See [Configuration Reference](../specs/config-reference.md#systemd-specifiers) for the full list.

## Catalogs (`catalog/`, `updex/catalog.go`)

`updex catalog list|search|add|remove` consumes sysext catalogs like
fedora-sysexts (https://fedora-sysexts.github.io/, served at
extensions.fcos.fr): per sysext, a stable GitHub release tagged `<name>`
publishes `<name>.conf` (a genuine sysupdate transfer file with
`MatchPattern=<name>-@v-%w-%a.raw`) and an aggregate `SHA256SUMS` covering
all versions/Fedora releases/arches; `<SiteURL>/<name>/<file>` redirects to
the right release asset for the `.conf`, `SHA256SUMS`, and `.raw` alike.

Design decisions (verified with the user, 2026-08):

- **No built-in repos.** These catalogs only apply to ucore/Fedora-atomic
  systems, so repos come exclusively from `<name>.catalog` INI files across
  `catalog.ConfigRoots` (`/etc/updex/catalogs.d` > `/run/...` >
  `/usr/local/lib/...` > `/usr/lib/...`; earlier root wins per filename;
  package var, test-overridable). Keys: `SiteURL` (required), `ListURL`
  (optional GitHub contents API endpoint for list/search only;
  `GITHUB_TOKEN` env honored as bearer token), `Component` (optional,
  default `catalog-<repo>`). Missing config → `catalog.ErrNoCatalogs`,
  surfaced by the SDK with setup guidance.
- **`RenderTransfer` is a security-constrained line transform**
  ([ADR-0006](../adr/0006-byte-preserving-render-transfer.md)), not an INI
  round-trip: it prepends the `GeneratedMarker` ownership header, injects
  `Features=<name>` right after `[Transfer]`, requires `Source.Type=url-file`
  and the configured `<SiteURL>/<name>/` source, and requires basename-only
  source and target patterns. It rewrites the target to a regular `0644` file
  under trusted `catalog.TargetPath` (default `/var/lib/extensions.d`), dropping catalog-provided `Path`,
  `PathRelativeTo`, `Mode`, and `CurrentSymlink` values so remote metadata
  cannot redirect a root-owned write. The complete `[Source]` and `[Target]`
  bodies are reconstructed after validation so alternate valid INI syntax
  cannot bypass field stripping; other sections stay verbatim.
  `%w`/`%a` specifiers deliberately stay **unexpanded** in the written
  `.transfer` — expansion happens at config load time — so the file keeps
  tracking the running Fedora release across OS upgrades. `ini.Load` validates
  both sections and their security-sensitive fields before transformation.
- **Ownership via repo-scoped `GeneratedMarker`**
  ([ADR-0003](../adr/0003-catalog-ownership-marker.md); PR #137 review, both
  rounds — first that name-in-component was too weak a signal, then that a
  bare marker prefix does not separate repos): every generated file starts
  with `# Generated by updex catalog (repo: <name>); ...`, and
  `catalog.GeneratedRepo`/`GeneratedFileRepo` parse the repo back out.
  `CatalogAdd` refuses to overwrite a file that is unmarked *or* marked by
  a different repo; `CatalogRemove` only claims a sysext whose
  `<etc component dir>/<name>.feature` marker names that repo, and skips a
  `.transfer` it does not own. Together this makes both a
  `Component=docker` override over a hand-managed component and two
  catalogs sharing one `Component` safe.
- **Name validation**: `catalog.ValidateSysextName`
  (`^[a-zA-Z0-9_][a-zA-Z0-9._+-]*$`) runs at the top of
  `CatalogAdd`/`CatalogRemove` and inside `FetchConf`; `CachedList`
  re-validates `Repo.Name` against `repoNamePattern` because it is public
  API reachable with a hand-built `Repo` that never went through
  `LoadRepos`. Traversal-shaped values therefore never reach
  `filepath.Join` (definition paths or cache filenames) or URLs.
- **Transactional add**
  ([ADR-0005](../adr/0005-transactional-writes-lstat-checks.md)):
  `CatalogAdd` snapshots the `.transfer`,
  `.feature`, and its `00-updex.conf` drop-in (`fileSnapshot` in
  `updex/catalog.go`) before writing, and every failure past that point
  runs the same `rollback()` closure — including the `MkdirAll` and the
  two `os.WriteFile` calls, which matters because `os.WriteFile`
  truncates on open, so a failing write can destroy a working definition
  by itself (third review round). Restore semantics: a fresh add's files
  are removed, a re-add's previous contents are rewritten; then the
  drop-in dir and (fresh adds only) the component dir are `os.Remove`d,
  which no-ops when non-empty. No enabled-but-broken state, no destroyed
  working definition, no mismatched old/new pair.
  `fileSnapshot` tracks `existed` (stat succeeded) separately from
  `captured` (contents read): a path that exists but cannot be read — a
  directory in the way, an unreadable file — is left strictly alone by
  `restore`, with a warning, because removing it would destroy state the
  snapshot cannot rebuild (Copilot review). Likewise `managedFileExists`
  returns stat failures other than not-exist as errors instead of reading
  them as "absent", so a stat error can never skip the ownership guard in
  `CatalogAdd`/`CatalogRemove` or silently under-report removed files.
- **Definitions are regular files, never symlinks**
  ([ADR-0005](../adr/0005-transactional-writes-lstat-checks.md); fourth
  review round):
  `managedFileExists` and `snapshotFile` use `os.Lstat`, and anything
  present that is not a regular file is an error. `os.Stat` reports a
  *dangling* symlink as absent, which skipped the ownership check and let
  the following `os.WriteFile` follow the link and create its target
  outside the component directory — a root-privileged write. `CatalogRemove`
  validates both the `.feature` and `.transfer` paths this way before
  `DisableFeature{Now}`, so a symlink cannot produce a half-completed
  teardown either.
- **Ownership checked before destruction**: `CatalogRemove` validates the
  `.transfer`'s marker/repo *before* calling `DisableFeature{Now}` and
  errors out on a mismatch, pointing at `updex features disable`. That
  teardown removes images and links described by whatever transfer claims
  the feature, so a post-hoc check would have already destroyed state
  belonging to a definition it then refuses to delete (third review
  round; reachable when an admin hand-replaces a generated `.transfer`
  and keeps the generated `.feature`).
- **Drop-in preservation**
  ([ADR-0004](../adr/0004-single-updex-drop-in.md)): removal deletes only
  `updexDropInName`
  (`00-updex.conf`, the single file updex writes, shared with
  `writeFeatureDropIn`) out of `<name>.feature.d`; administrator files
  like `50-local.conf` survive and keep the directory alive.
- **Provenance respects `-C`**: `updex.featureOrigin` takes a
  `definitionsOverride` flag, because a `--definitions` directory may sit
  under a search root (`-C /etc/my-defs`) where containment alone would
  report `local:etc` or even `image`. Such files were not discovered
  through the search paths at all, so they are `unknown`. The catalog
  marker still wins over the flag — it is recorded in the file rather
  than inferred from its location (fourth review round).
- **Listing status is repo-scoped**: `CatalogList` marks
  `Installed`/`Enabled` only when `GeneratedFileRepo(f.FilePath)` names
  the listing repo (`f.FilePath` is the highest-priority file, i.e. the
  `/etc` one catalog add writes). Two repos sharing a `Component` no
  longer report each other's installs, and a hand-written or
  `/usr/lib`-shipped same-named feature is not reported as catalog-added.
- **After `add`, nothing knows about catalogs.** The generated `.feature`
  is `Enabled=false`; `CatalogAdd` then calls
  `EnableFeature{Now, Component: repo.Component}` so enabling goes through
  the standard drop-in and the download reuses `installTransfer`. All
  `features` operations and the daemon manage the sysext via the normal
  union domain. `CatalogRemove` is the only catalog-aware teardown:
  `DisableFeature{Now, Force}` (unmerge + image/link removal, `--force`
  when merged) then deletes `<name>.transfer`/`<name>.feature`/
  `<name>.feature.d` from `config.EtcComponentDir(repo.Component)` and the
  directory itself if empty.
- **Repo disambiguation**: bare names are probed against every repo
  (`FetchConf` 404 → `catalog.ErrNotFound` distinguishes "not here" from
  transport errors); multiple hits error listing `repo/name` candidates.
  CLI accepts `REPO/NAME` or the persistent `--repo` flag
  (`splitCatalogArg`, errors when both are given and conflict).
- Catalog operations reject a `Definitions` override (component-scoped,
  same conflict as `--component` + `-C`).
- **Listing cache** (`catalog/cache.go`): `CatalogList` uses
  `catalog.CachedList`, a per-repo TTL+ETag cache stored as
  `<catalog.CacheDir>/list-<repo>.json` (`CacheDir` defaults to
  `os.UserCacheDir()/updex`; empty disables caching; package var,
  test-overridable — the updex test helper always redirects it). Entry:
  `{list_url, etag, fetched_at, names}`; `list_url` mismatch invalidates.
  Flow: age < TTL (default 60 min, `DefaultListCacheTTL`) → serve cache,
  zero network; expired → conditional GET with `If-None-Match` (GitHub's
  304 costs no rate limit → bump `fetched_at`); live fetch failure with an
  existing entry → serve stale with `CacheResult.Stale` (SDK warns), except
  `context.Canceled`/`DeadlineExceeded`, which propagate rather than
  reporting success for an aborted call;
  `NoCache` (CLI `--no-cache` on list/search; named to avoid colliding
  with the global `--no-refresh` sysext flag) always fetches live and
  rewrites the cache. Reads/writes are best-effort — corrupt entries are
  misses, write failures ignored. Only the ListURL enumeration is cached;
  `FetchConf` and Installed/Enabled state are always live.
- `config.EtcComponentDir` now derives from `SearchRoots[0]` (still `/etc`
  in production) so catalog/drop-in write paths are exercisable in tests
  that override `SearchRoots`.

## Data Flow

### Feature update (end-to-end)

1. Load all `.feature` and `.transfer` files: by default the union of the legacy default directory and every discovered component (`Client.loadDomain`, see "Components" above), or a single scope when `--component`/`-C` narrows it. Non-sysext transfers (A/B partition, UKI) are filtered out of the default union before this point.
2. Filter transfers to those matching enabled features
3. For each transfer:
   - Fetch `SHA256SUMS` manifest from source URL (+ GPG verify if configured); transient network failures during request or body read and HTTP 5xx/429 are retried up to 3 attempts with exponential backoff, while TLS/cert errors, unsupported protocols, 4xx other than 429, and checksum mismatches fail immediately (retry policy recorded in [ADR-0008](../adr/0008-bounded-retry-no-resume.md)). Manifests are cached by source URL across transfers so that multiple transfers sharing the same source make only one HTTP request
   - The manifest cache key is only the source URL path. Verification is decided during the first fetch for that path; avoid relying on mixed per-transfer `Verify` settings for one shared source URL unless the cache behavior is changed.
   - Parse source patterns and extract available versions using pattern matching (`@v` placeholder); parsed patterns are returned to callers so `installTransfer` reuses them without re-parsing. The candidate list is returned lexically sorted so that, with the stable `version.Sort`, selection stays deterministic even if two versions compare equal
   - Select newest version via `version.Sort` (semver where possible, Debian/dpkg ordering for versions with `:`, `~`, or `+`, string fallback otherwise)
   - Skip if already installed (check target directory)
   - Download file, retrying the same transient request/body-read failures and HTTP 5xx/429 from scratch without range/resume requests. Each attempt uses a new temp file and invokes `OnDownloadProgress` again, so progress writers must be attempt-local. SHA256 is verified against the compressed bytes before decompression.
   - Decompress if needed (xz, gz, zstd — detected from filename). The installed filename is derived from the target patterns via `buildTargetFilename`: the first pattern that produces a name without a compression suffix wins, and if every target pattern is a compressed variant the suffix is stripped, so the on-disk name always matches the decompressed content regardless of which source pattern matched
   - Atomically rename to final path; on cross-device rename failure, copy to a temp file on the destination filesystem, sync it, chmod it, then rename
   - Remove any legacy `CurrentSymlink` in the target directory when the transfer defines one. The ordering in `installTransfer` is load-bearing: (1) fetch available versions and select the newest candidate, (2) call `sysext.GetInstalledVersions` while any legacy `CurrentSymlink` still exists, (3) remove the legacy staging symlink, (4) only then return early if the selected version was already both installed and current. `GetInstalledVersions` can still use a legacy `CurrentSymlink` to distinguish "newest version is staged but not current" from "already current"; deleting that symlink first makes the newest staged file look current and can skip the required `/var/lib/extensions/<component>.<ext>` relink. Because cleanup runs before any already-current return, stale staging symlinks are removed even when no download is required.
   - Create or replace `/var/lib/extensions/<component>.<ext>` pointing to the newest staged image path; the link name is derived from the transfer filename component and the target pattern extension with compression suffixes stripped. This is a hard error because `systemd-sysext refresh` cannot see the staged image without it
   - Vacuum old versions per `InstancesMax`; the active symlink target and `ProtectVersion` are always kept. Non-dry-run `UpdateResult.RemovedVersions` is not populated because the install path calls `sysext.Vacuum`, while dry-run uses `PlanVacuumAfterInstall`
4. Call `systemd-sysext refresh` to reload all extensions (unless `--no-refresh`). Callers batch this — `installTransfer` is called with `NoRefresh: true` per-component, and a single refresh runs at the end. With `--dry-run`, the same manifest/version resolution runs, but `installTransfer` returns before download; `UpdateFeatures` reports would-download/would-install results and read-only vacuum removals, then skips the final refresh.

### Enable/disable feature

`00-updex.conf` is the single drop-in updex owns; administrator drop-ins
always override and always survive (decision recorded in
[ADR-0004](../adr/0004-single-updex-drop-in.md)).

- **Enable**: Creates drop-in at `/etc/sysupdate.d/<name>.feature.d/00-updex.conf` (or `/etc/sysupdate.<component>.d/<name>.feature.d/00-updex.conf` for a component-scoped feature — see "Components" above) setting `Enabled=true`. With `--now`, also downloads extensions immediately.
- **Disable**: Creates drop-in setting `Enabled=false` at the same scoped path. With `--now`, calls `Unmerge()`, removes symlinks from `/var/lib/extensions/`, and deletes all versioned files. Before removal, `DisableFeature` treats an image as active when its version matches either a legacy transfer `CurrentSymlink` or an entry in the client's captured `RuntimePaths.RunExtensionsDir` (production default `/run/extensions`, systemd-sysext's merged-image snapshot). The `/var/lib/extensions` link is not an active signal: it makes an image available for a future merge but does not prove the image is currently merged. `--force` is required when either active signal matches; forced removal reports that a reboot is required.

### Auto-update daemon

The daemon stages updates but never activates them (decision recorded in
[ADR-0007](../adr/0007-daemon-stages-never-activates.md)).

- `updex daemon enable` installs `/etc/systemd/system/updex-update.timer` and `.service`, then enables and starts the timer
- The timer runs `daily`, is `Persistent=true`, and uses `RandomizedDelaySec=3600`
- The service command is `/usr/bin/updex features update --no-refresh`, so automatic downloads are staged and not refreshed/activated until a later refresh or reboot
- Unit installation refuses to overwrite existing timer/service files; callers must disable first

## CLI Commands

```
updex features list                     List all features with status (alias: updex feature)
                                         CATALOG column shows provenance: a catalog
                                         name, image:<id>, local:etc|usr|run, or unknown
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

updex catalog list                      List sysexts from configured catalogs
updex catalog search <term>             Substring search across catalogs
updex catalog add [REPO/]NAME           Fetch conf, write .transfer/.feature into the
                                         catalog's component, enable + download now
updex catalog remove [REPO/]NAME        DisableFeature --now + delete generated files
  --force                               Allow removal of merged extensions
  --repo <name>                         Persistent flag on `updex catalog`, equivalent
                                         to the REPO/ prefix (error if they conflict)

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

## CI and Releases

`.github/workflows/pr-title.yml` (`amannn/action-semantic-pull-request`,
SHA-pinned, `pull_request` `opened|edited|synchronize|reopened`, read-only
permissions, no checkout) fails a pull request whose title is not a
Conventional Commit, and — because the repository squash-merges with the
"commit or PR title" default, under which a single-commit PR lands under its
commit's subject — also validates the lone commit's subject
(`validateSingleCommit: true`). The accepted types mirror `CONTRIBUTING.md`
and the `.goreleaser.yaml` changelog groups. It is a plain status, not a
required check: `main` has no branch protection or ruleset today.

`.github/workflows/release.yml` publishes tagged GoReleaser Pro releases and
packages, then dispatches `event-type: build` to `frostyard/snosi` so the
component release promptly fans out into an image rebuild
([frostyard/core ADR-0013](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md)).
The tag-only trigger is the dispatch guard; the step must not check for a
default-branch ref because tag runs use `refs/tags/<tag>`.
`updex/release_workflow_contract_test.go` parses the workflow and pins that
trigger-to-dispatch contract.

`.github/workflows/snapshot.yml` runs after successful `Tests` workflows on
`main` and publishes a GoReleaser Pro nightly under the singleton `dev` tag.
GoReleaser's `nightly.keep_single_release` deletes and recreates that release,
so concurrent runs race while uploading the same artifact names and fail with
GitHub HTTP 422 `already_exists`. Workflow-level concurrency group
`goreleaser-nightly` allows only one publisher at a time and cancels stale runs
so the newest successful test result is the release that survives.
