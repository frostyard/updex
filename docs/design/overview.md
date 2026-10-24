# updex Design Overview

## Purpose

updex is a Go SDK and CLI for managing [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html) images. It replicates `systemd-sysupdate` functionality for `url-file` transfers, providing feature-based management of system extensions with version tracking, SHA256 verification, optional GPG signing, and automatic cleanup.

The project follows an **SDK-first design**: core feature, catalog, component,
and daemon workflows live behind public `updex.Client` methods. The CLI is a
thin wrapper that authorizes privileged operations, parses flags, calls the
SDK, and formats structured results.

Public repository observability is indexed at `docs/metrics/README.md` (a real tree, per ADR-0012). That page links live CI, nightly compliance, Codecov, pull-request, AI-fix, and release evidence and excludes secrets, private prompts, vulnerability embargoes, and managed-host telemetry; the monthly acceptance metric consumed by `.github/auto-qa-tuning.json` is defined in `docs/specs/pr-acceptance-metric.md` (`docs/metrics.md` resolves to it), and the whole declare→review→gate→learn→observe loop is `docs/design/quality-loop.md`. `updex/public_metrics_contract_test.go` prevents the ACMM paths, the substantive signal contract, and the tuning reference from drifting apart.

## Architecture

```
cmd/updex-cli/main.go          Entry point (frostyard/clix bootstrap)
cmd/updex/root.go               Cobra root command, global flags
cmd/updex/features.go           features list|enable|disable|update|check
cmd/updex/features_run.go       Run functions for feature subcommands
cmd/updex/components.go         components (list discovered systemd-sysupdate components)
cmd/updex/catalog.go            catalog list|search|add|remove ([REPO/]NAME parsing,
                                --repo/--force flags, output formatting)
cmd/updex/daemon.go             daemon enable|disable|status SDK wrappers
cmd/updex/units.go              hidden `units --output-dir` release command (writes
                                the packaged timer and service; scripts/units.sh)
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
  install.go                    installTransfers() — a feature's install pipeline:
                                stageTransfer() per transfer (select, download,
                                verify), then linkStaged() links all or none
                                (snapshot + restore), then the vacuum
                                Reuses parsed patterns from getAvailableVersions
  rollback.go                   RollbackFeature(), the rollback record
                                (/var/lib/updex/rollback/<feature>, RuntimePaths.RollbackDir)
  lock.go                       ErrBusy, acquireLock() — the exclusive,
                                non-blocking flock every mutating operation holds
                                (RuntimePaths.LockPath, default /run/updex/lock)
  list.go                       getAvailableVersions() — returns versions,
                                manifest, and parsed patterns for caller reuse
  options.go                    Option structs for all operations (each
                                feature-related struct carries Component string)
  results.go                    Result structs for all operations
  daemon.go                     DaemonUnits() (the one source of the timer and
                                service text), EnableDaemon(), DisableDaemon(),
                                DaemonStatus() systemd lifecycle orchestration
  daemon_legacy.go              frozen ADR-0007 unit texts that disable recognizes
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
                                (VacuumWith / PlanVacuumWith: keeps linked, merged,
                                ProtectVersion, newest InstancesMax)
systemd/                        systemd timer/service generation + systemctl management
internal/retry/                 bounded retry policy shared by download/ and manifest/
                                (module-internal, ADR-0008)
internal/httpclient/            shared default *http.Client constructor (timeout +
                                HTTPS-to-HTTP downgrade refusal) used by updex.NewClient,
                                manifest.Fetch, and download.Download (module-internal)
internal/testutil/              HTTP test server helpers (module-internal)
```

### Package dependency flow

```
CLI (cmd/features*) ─┐
CLI (cmd/catalog.go) ├→ SDK (updex/) → config, catalog, manifest, download,
CLI (cmd/daemon.go) ─┘                version, sysext, systemd, imagecompat
```

## Key Patterns

### SDK conventions

- All public SDK methods take `context.Context` as first parameter for cancellation
- Operations use dedicated option structs (e.g., `EnableFeatureOptions`, `UpdateFeaturesOptions`) to allow future expansion without breaking changes
- Return dedicated result structs with status fields + error
- `ClientConfig.HTTPClient` is reused for manifest fetches and downloads; if nil, `NewClient` creates one with a 10-minute timeout, the standard 10-redirect limit, and an HTTPS-to-HTTP downgrade refusal
- `ClientConfig.Progress` receives informational/warning/debug messages; `ClientConfig.OnDownloadProgress` is a separate download-byte callback
- `ClientConfig.SystemdManager` injects the unit-file paths (`UnitPath`,
  `RunUnitPath`, `PackagedUnitPath`) and `SystemctlRunner` used by daemon SDK
  methods; nil selects the production manager (`/etc/systemd/system`,
  `/run/systemd/system`, `/usr/lib/systemd/system`). `NewTestManager` leaves
  `RunUnitPath` and `PackagedUnitPath` empty so a test never sees the host's
  runtime or packaged units
- `UpdateFeatures` and `CheckFeatures` cache fetched manifests by `Transfer.Source.Path` only. Future changes that mix verification policy or auth by transfer for the same source URL need to revisit that cache key.
- The feature SDK methods (`UpdateFeatures`, `CheckFeatures`, enable/disable with `Now`) use `config.GetTransfersForFeature`, which includes transfers where the feature appears in either `Features` or `RequisiteFeatures`. The more general `config.FilterTransfersByFeatures` implements full active-transfer logic, including standalone transfers and AND/OR feature requirements, but it is not the main path for current feature update/check workflows.
- Error messages: lowercase, no trailing punctuation, wrapped with `fmt.Errorf("context: %w", err)`

### Testing patterns

- Mock interfaces for system commands: `sysext.SysextRunner`, `systemd.SystemctlRunner`
- `ClientConfig.SysextRunner` field for injecting mocks into the SDK client — `NewClient` stores the runner directly on the `Client` struct (does not mutate global state). An injected runner must implement `sysext.PathSysextRunner` (`SysextRunner` plus `LinkToSysextAt`) to be usable for a real install: the client links only through that method with its captured `SysextLinkDir`, and refuses a `SysextRunner`-only runner with `updex.ErrLegacySysextRunner` before mutating anything rather than falling back to the package-global `sysext.SysextDir`. `sysext.MockRunner` implements it and records the directory it was given in `LinkToSysextAtDir`; `updex/install_link_test.go` pins the refusal.
- `ClientConfig.SystemdManager` for injecting a `systemd.NewTestManager`
  rooted at `t.TempDir()` with a mock `SystemctlRunner`; daemon SDK tests use
  this instead of touching real units or invoking `systemctl`
- `ClientConfig.Paths` (`RuntimePaths`) for supplying temp filesystem trees to a client at construction — the preferred approach for path isolation since ADR-0011; use `t.TempDir()` paths for `DefinitionRoots`, `SysextLinkDir`, `RunExtensionsDir`, `SysextHierarchies`, `CatalogConfigRoots`, etc. Independently configured clients can run in parallel without save/mutate/restore discipline.
- Package-level compatibility variables (`config.SearchRoots`, `catalog.ConfigRoots`, `catalog.CacheDir`, `sysext.SysextDir`) remain settable for tests that have not yet migrated; mutation before client construction still works because `NewClient` reads each global once at that point.
- Transfer parsing receives the client's captured os-release paths as well as
  definition roots, so `%w`/related specifier expansion cannot cross client
  boundaries.
- `internal/testutil.NewTestServer()` creates `httptest.Server` with configurable manifests and file content
- `t.TempDir()` for filesystem operations, `t.Context()` for context
- `tests/e2e/` builds and runs the real CLI subprocess for argument, exit-code, output, custom-config, and read-only HTTP checks in its dedicated PR job. `cmd/updex/integration_test.go` runs the full Cobra/clix command in-process so package search roots can point at temporary default component and catalog trees; these tests run in both the PR Unit Tests and Race Detection jobs. Keep mutating subprocess paths behind parser failures so CI does not require root.
- `sysext/link_test.go` pins the `/var/lib/extensions` link lifecycle through `LinkToSysextAt` (explicit dir, no global mutation): newest-by-version selection, replacing symlinks/dangling links/regular files, staging-dir symlinks ignored, `Target.Path` fallback, metadata rejection (component, patterns, `@v`), empty/missing/non-matching staging sets, a conflicting destination directory preserved on error, sysext-dir creation failure (parent is a regular file), and removal failure (read-only dir, skipped as root). Every failing case asserts no new symlink is left behind. It also pins that `DefaultRunner` implements `PathSysextRunner` and links into the explicit dir, not `SysextDir`, and (`TestLinkedVersionAt`) what the link read side reports: absent, a regular file, a dangling link, a foreign image name, relative and absolute targets, a compressed target name, and an unusable target pattern.
- `updex/status_test.go` pins the status read with `ClientConfig.Paths` temp trees (`statusCases`: link ahead of running, linked and not merged, in sync, dangling link, no link, `unknown`, metadata of another image) through both `Features` and `CheckFeatures`, the unreadable-merged-state paths, the JSON key shape, and the refresh warning for `UpdateFeatures`, `EnableFeature` and `DisableFeature` (other pending link listed and the mock refresh still called; nothing pending lists nothing; `NoRefresh` lists nothing; dry run says "would"; component scope still sees other components). `cmd/updex/features_status_test.go` runs the handlers over `setRuntimePaths` temp hierarchies (text columns, the pending footer, JSON). `tests/e2e/` cannot redirect the real binary's link directory or hierarchies, so its `TestE2E_FeaturesStatusJSONShape` pins only the reachable shape (keys present, empty values, no pending marker).
- CLI handler seams: `cmd/updex` exposes four package-level test seams so mutating handlers can run rootless in-process — `getEUID` (swap for `func() int { return 0 }` to pass `requireRoot`), `sysextRunner` (nil in production so `newClient` gets the SDK default; set to a `*sysext.MockRunner` to observe `Refresh`/`Unmerge`), `systemdManager` (nil in production; set to a temporary `systemd.NewTestManager` for daemon wrappers), and `runtimePaths` (zero in production so the SDK resolves its usual paths; `newClient` passes it as `ClientConfig.Paths`). `TestMain` in `cmd/updex/main_test.go` points `runtimePaths` at temporary `SysextHierarchies` and an absent `RunExtensionsDir` in one `os.MkdirTemp` tree, so no CLI test reads the host's `/usr/.systemd-sysext`, `/opt/.systemd-sysext`, or `/run/extensions` — the follow-up to the ADR-0015 merged-state detection that could otherwise make a `features disable --now` or `catalog remove` test depend on a real merged image (for example `docker`); a test that needs other paths calls `setRuntimePaths`, and `TestHandlerTestsDoNotReadHostMergedState` guards the seam. `cmd/updex/features_mutation_test.go` uses the first two with temporary `config.SearchRoots` (drop-ins land under `roots[0]`), a temporary `sysext.SysextDir`, and a fake HTTP source to cover `runFeaturesEnable`/`runFeaturesDisable` end-to-end: `--now`, `--force`, `--no-refresh`, `--component`, dry-run, and text/JSON result shapes, including a real JSON+silent download whose stdout must decode as exactly one result object. `cmd/updex/catalog_mutation_test.go` does the same for `runCatalogAdd`/`runCatalogRemove`, additionally pointing `catalog.ConfigRoots`, `catalog.CacheDir`, and `catalog.TargetPath` at temp dirs and serving `<name>/<name>.conf`, `SHA256SUMS`, and the image from one `httptest.Server` (configure two `.catalog` files against it to exercise `[REPO/]NAME` / `--repo` disambiguation); remove cases seed the post-add state through the SDK's `CatalogAdd`.

### CLI output

- Text tables by default, JSON with `--json` flag — both `--json` and `--dry-run` are provided by the `github.com/frostyard/clix` package, not defined in this repo
- `cmd/updex/client.go` always wires `clix.NewReporter()`, but enables `newProgressBar` only outside JSON and silent modes. Interactive bars and their completion newline write to stderr, preserving stdout for command results.
- Operations requiring filesystem changes call `requireRoot()` before entering the SDK. This currently includes dry-run variants of `features enable`, `features disable`, `features update`, `daemon enable`, and `daemon disable`, so dry-run is mutation-free but not rootless from the CLI.

### Dry-run behavior

- `UpdateFeaturesOptions.DryRun` is threaded through `UpdateFeatures` into `installTransfers`; `stageTransfer` is the choke point before downloads and legacy staging-symlink cleanup, and a dry run never reaches the link step, refresh, or vacuum deletion. A dry run takes no operation lock (see "Operation lock")
- Update dry-runs still perform read-only work: load configs, fetch manifests, resolve versions, inspect installed files, and, unless `NoVacuum` is set, call `sysext.PlanVacuumWith` (with the would-install version as the active version, the client's hierarchies for the merged version, and no `LinkDir`, because the link moves to the new version) to populate `UpdateResult.RemovedVersions`
- In update dry-run results, `Downloaded=true` means "would download", `Installed=false` means no install occurred, and `DryRun=true` disambiguates the status for JSON consumers
- In non-dry-run update results, `Downloaded=true` means a new file was fetched and installed. `Installed=true` is also set for already-current components, so use `Downloaded` to distinguish "changed" from "already up to date". A component that was already staged but whose systemd-sysext link had to be repointed also keeps `Downloaded=false`; it is distinguished by `NextActionMessage` (`Reboot required to activate changes`) and by the `Linked staged version <version>` progress message.
- Enable/disable dry-runs are lighter previews: enabling with `--now` lists associated transfer components without manifest/version resolution, while disabling with `--now` performs active-version checks but records component-level "would remove" entries rather than enumerating every file

### Public API (Issue #13)

All core packages (`config`, `version`, `download`, `manifest`, `sysext`, `systemd`) are exported as public API at `github.com/frostyard/updex/v2/<package>`. This was an intentional decision: the types in these packages (e.g., `Transfer`, `Feature`, `Pattern`, `Manifest`) were designed with exported fields and are suitable for external consumption. Three packages stay module-internal: `internal/retry`, the bounded retry policy shared by `download` and `manifest` ([ADR-0008](../adr/0008-bounded-retry-no-resume.md)); `internal/httpclient`, the shared default `*http.Client` constructor (timeout plus HTTPS-to-HTTP downgrade refusal) that `updex.NewClient`, `manifest.Fetch`, and `download.Download` each fall back to when their caller supplies no client; and `internal/testutil`, the HTTP test server helpers.

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
[ADR-0011](../adr/0011-capture-merged-sysext-state-per-client.md)) with the same
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
- Masked transfer files are symlinks to `/dev/null`; they are omitted from the loaded domain while still suppressing lower-priority definitions with the same filename.

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

Enabled by default to match systemd-sysupdate. Set `Verify=no` explicitly to opt out; the client's global `Verify` setting (the CLI's `--verify` flag) forces verification even for transfers that opt out. Transfers generated by `catalog add` honor `Verify=no` only while their owning repo's local `.catalog` sets `AllowUnsigned=yes`; `Client.verifyPolicy` (`updex/list.go`) recomputes this on every `getAvailableVersions` call (see "Signature policy is local" under Catalogs). When enabled, fetches `SHA256SUMS.gpg` (detached signature) and verifies against keyrings at:
1. `/etc/systemd/import-pubring.gpg`
2. `/usr/lib/systemd/import-pubring.gpg`

Uses `github.com/ProtonMail/go-crypto/openpgp` for signature verification. Supports both binary and armored keyring formats.

The main `SHA256SUMS` fetch and the detached `.gpg` signature fetch share the same bounded retry policy (ADR-0008): each GET and body read retries transient network failures and HTTP 5xx/429 under the retry settings resolved by `manifest.Fetch`, while keyring loading and signature checking run once after the fetch and are never retried. Manifest response bodies are read through a 4 MiB-plus-one-byte limit and detached signatures through a 1 MiB-plus-one-byte limit; crossing either boundary fails before parsing, keyring loading, or signature verification.

A 404 on `SHA256SUMS.gpg` wraps `manifest.ErrSignatureNotFound`;
`getAvailableVersions` then prefixes the error with the policy rule that
required the signature (forced, catalog not opted in, not configured, or
unloadable, or the transfer's own `Verify=yes`, which says the publisher must
provide `SHA256SUMS.gpg`). For an opted-in catalog transfer the message says the
*generated transfer* requires it and suggests re-running `catalog add`,
because a v2.0.1- or v2.0.2-rendered file lacks `Verify=` even when the catalog declares
`Verify=no`. Nothing ever retries without
verification after a signature failure.

When a transfer's effective setting is `Verify=false`, checksum authenticity depends
on the transport that supplied both the transfer definition and
`SHA256SUMS`. Catalog URL validation and the default client's redirect
downgrade guard prevent that transport from silently changing from HTTPS to
cleartext.

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
  `GITHUB_TOKEN` env honored as bearer token only for the trusted
  `https://api.github.com` origin and stripped on cross-origin redirects),
  `Component` (optional, default `catalog-<repo>`), `AllowInsecure`
  (optional, default `no`), and `AllowUnsigned` (optional, default `no`;
  see "Signature policy is local" below). `SiteURL` and `ListURL` must be absolute HTTPS
  URLs unless the definition explicitly sets `AllowInsecure=yes`. That
  escape hatch does not widen #319's token policy: an `http://` `ListURL`
  never receives `GITHUB_TOKEN`, even when explicitly allowed. Missing config
  → `catalog.ErrNoCatalogs`, surfaced by the SDK with setup guidance.
- **Signature policy is local** (#439,
  [ADR-0014](../adr/0014-local-catalog-signature-policy.md)). #394 stripped
  every catalog-supplied `Verify=` so remote metadata could not disable GPG
  verification, which broke catalogs that publish only `SHA256SUMS` (Fedora
  and community fedora-sysexts set `Verify=false` and ship no `.gpg`). The
  repo's local `AllowUnsigned=yes` now grants the catalog's `Verify=`
  permission to decide: `RenderTransferTo` re-emits it as a canonical
  `Verify=yes|no` (value parsed by ini.v1 `MustBool(true)`, the same rule
  `config` applies on load, so no disguised spelling can make render and load
  disagree), and without the opt-in emits nothing. Rendered content is not
  trusted on its own: `Client.verifyPolicy` maps a marker-bearing transfer to
  its owning repo and requires a signature unless that repo is currently
  configured with `AllowUnsigned=yes`, so revoking the opt-in takes effect
  on the next run, a hand-added `Verify=no` is ignored, and a deleted or
  unparseable owning `.catalog` fails closed. Only the owning repo's file
  is loaded (`catalog.LoadRepoFrom`, same first-root-wins precedence), so an
  unrelated broken `.catalog` cannot revoke another repo's opt-in. The
  transfer header is read once (`catalog.GeneratedFileHeader`); a read error
  or an unattributable marker requires a signature. The opt-in also applies
  only while the transfer's `Source.Path` is still `<SiteURL>/<name>` for
  the repo's current `SiteURL`, so a v2.0.0 render of an `AllowInsecure`
  `http://` catalog is not fetched unsigned over cleartext after the catalog
  moves to HTTPS. `LoadRepoFrom` finds files with `os.Lstat`, so a dangling
  higher-priority symlink fails to load instead of yielding to a lower
  root, as in `LoadReposFrom`. `AllowUnsigned=yes` is refused alongside
  `AllowInsecure=yes`. `ClientConfig.Verify` overrides
  everything. Transfers rendered by v2.0.1 or v2.0.2 carry no `Verify=` and
  need a re-add after opting in; v2.0.0 renders kept `Verify=false` and work
  again once the repo opts in.
- **Catalog transport is an integrity boundary.** A catalog whose repo sets
  `AllowUnsigned=yes` may declare `Verify=false`; in that case the HTTPS transport protecting
  the `.conf`, `SHA256SUMS`, and image is the only remote integrity boundary.
  The SDK-created HTTP client therefore rejects redirects from HTTPS to HTTP
  for every catalog, manifest, and image request while retaining the standard
  10-redirect limit. HTTP-to-HTTP and HTTPS-to-HTTPS redirects remain allowed.
  Caller-supplied `ClientConfig.HTTPClient` values are not modified. The
  `AllowInsecure=yes` configuration escape hatch is for explicitly trusted
  development and test endpoints and deliberately opts out of the URL-level
  cleartext prohibition; the default redirect client still refuses a
  downgrade that begins on HTTPS. Existing `http://` catalog files must add
  the opt-in or migrate to HTTPS.
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
  `CatalogAdd` snapshots the `.transfer`, `.feature`, its `00-updex.conf`
  drop-in, every staged entry matching the transfer target patterns, and the
  effective sysext link before install. Matching regular images are streamed
  to same-directory temporary backups rather than retained as `[]byte`, so
  heap use is bounded while temporary disk use scales with retained image
  size. The snapshots are removed after success or rollback. A matching staged
  entry or link destination that is neither a regular file nor a symlink is
  refused before enable/download can replace it. Every failure past the first
  definition write runs the same `rollback()` closure — including `MkdirAll`,
  either definition write, download, link mutation, vacuum, and the final
  refresh. Definition writes use
  `writeManagedFile`, and captured rollback contents use its
  mode-preserving variant: each creates a same-directory temporary regular
  file, syncs and closes it, then renames it over the managed path. A fresh
  add's definitions, staged image, and link are removed; a re-add restores
  previous definition bytes and permissions, staged images, and link target;
  restoration errors are joined with the original operation error. Then the
  drop-in dir and (fresh adds only) the component dir are
  `os.Remove`d, which no-ops when non-empty. Per-file atomic replacement
  prevents truncation and symlink-following races, while the snapshots
  prevent an enabled-but-broken state or mismatched old/new pair.
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
  an open-in-place write follow the link and create its target outside the
  component directory — a root-privileged write. The atomic writer also
  replaces the managed directory entry rather than following a symlink
  planted after the check. `CatalogRemove`
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
  the standard drop-in and the download reuses `installTransfers`. `CatalogAdd` and `CatalogRemove` take the operation lock themselves and call the unexported `enableFeature`/`disableFeature`, so the lock is never taken twice. All
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
- **Listing cache** (`catalog/cache.go`): `CatalogList` calls
  `catalog.CachedListIn` with the per-client cache directory the `Client`
  captured at construction (`c.paths.catalogCacheDir`), a per-repo
  TTL+ETag cache stored as `<catalogCacheDir>/list-<repo>.json`.
  `NewClient` resolves that directory once from
  `RuntimePaths.CatalogCacheDir`: an explicit path wins, the
  `DisableCatalogCache` sentinel disables caching (empty dir), and an
  unset field falls back to the package-global `catalog.CacheDir` read at
  that moment (default `os.UserCacheDir()/updex`; empty disables caching).
  Because the value is captured, mutating `catalog.CacheDir` afterwards
  does not redirect an existing client — a caller wanting a different
  directory passes `RuntimePaths.CatalogCacheDir` or builds a new client
  (the updex test helper redirects the global *before* constructing its
  client). `catalog.CachedList` is only the process-global convenience
  wrapper that forwards `catalog.CacheDir`; SDK internals do not use it.
  Entry:
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
  misses, write failures never fail the list operation but are reported via
  `CacheResult.WriteErr` (SDK warns). Only the ListURL enumeration is cached;
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
   - The manifest cache key is only the source URL path, but each cached `manifest.Manifest` carries `Verified`, and a transfer that requires verification (`ClientConfig.Verify` or `Verify=true`) never consumes an unverified cached manifest: it refetches with verification and the verified manifest replaces the cache entry (a verified manifest may serve unverified transfers, never the reverse). Mixed per-transfer `Verify` settings on one shared source therefore cost at most one extra fetch and can never downgrade verification.
   - Parse source patterns and extract available versions using pattern matching (`@v` placeholder); parsed patterns are returned to callers so `stageTransfer` reuses them without re-parsing. The candidate list is returned lexically sorted so that, with the stable `version.Sort`, selection stays deterministic even if two versions compare equal
   - Select newest version via `version.Sort` (semver where possible, Debian/dpkg ordering for versions with `:`, `~`, or `+`, string fallback otherwise)
   - Skip if already installed (check target directory)
   - Download file, retrying the same transient request/body-read failures and HTTP 5xx/429 from scratch without range/resume requests. Each attempt uses a new temp file and invokes `OnDownloadProgress` again, so progress writers must be attempt-local. The raw payload read from the server is capped at 16 GiB by default (`download.DefaultMaxDownloadSize`, twice `DefaultMaxDecompressedSize`, overridable per call with `WithMaxDownloadSize`): an over-limit `Content-Length` is rejected before any bytes are streamed, and the read itself is bounded with `io.LimitReader` in case `Content-Length` is absent or understated. Crossing the cap either way returns `download.ErrDownloadTooLarge`. SHA256 is verified against the compressed bytes before decompression.
   - Decompress if needed (xz, gz, zstd — detected from filename), with decompressed output capped at 8 GiB by default (`download.DefaultMaxDecompressedSize`, overridable per call with `WithMaxDecompressedSize`). Crossing the cap returns `download.ErrDecompressedTooLarge`, removes both compressed and decompressed temporary files, and leaves the target path untouched. The installed filename is derived from the target patterns via `buildTargetFilename`: the first pattern that produces a name without a compression suffix wins, and if every target pattern is a compressed variant the suffix is stripped, so the on-disk name always matches the decompressed content regardless of which source pattern matched
   - fsync the file before the rename on every path (the verified temp file, and the decompressed output when the download was compressed), so a crash after install cannot leave a zero-length or partial image behind the sysext link
   - Atomically rename to final path; on cross-device rename failure, copy to a temp file on the destination filesystem, sync it, chmod it, then rename
   - Remove any legacy `CurrentSymlink` in the target directory when the transfer defines one. The ordering in `stageTransfer` is load-bearing: (1) fetch available versions and select the newest candidate, (2) call `sysext.GetInstalledVersions` while any legacy `CurrentSymlink` still exists, (3) remove the legacy staging symlink, (4) only then return early if the selected version was already both installed and current. `GetInstalledVersions` can still use a legacy `CurrentSymlink` to distinguish "newest version is staged but not current" from "already current"; deleting that symlink first makes the newest staged file look current and can skip the required `/var/lib/extensions/<component>.<ext>` relink. Because cleanup runs before any already-current return, stale staging symlinks are removed even when no download is required. The already-current return also repairs the sysext link (`sysext.LinkIsCurrentAt`): when `<SysextLinkDir>/<component>.<ext>` is missing, dangling, not a symlink, or resolves to another image, the link step (`linkStaged`) relinks through the runner (`restored sysext link for <component>`) and still reports no download; a correct link is left untouched, and a `GetInstalledVersions` failure on this path is returned as `failed to inspect installed versions: …` rather than falling through into a download. A relink is reported to the caller separately from the download flag (an unexported `Relinked` field on the install outcome, so no exported result type changed): `UpdateFeatures` keeps `Downloaded=false` and `Installed=true`, sets `NextActionMessage` to `Reboot required to activate changes`, and prints `Linked staged version <version>` instead of `Version <version> already installed and current`. Without that hint the operator cannot tell that the image the host runs changed and needs a refresh or reboot — the case seen on 2026-10-23, where `systemd-sysupdate` advanced its own staging symlink to a newer image while the updex link still pointed at the older one.
   - Link step, after every transfer of the feature staged successfully (see "Feature transfers link together" below): create or replace `/var/lib/extensions/<component>.<ext>` pointing to the newest staged image path; the link name is derived from the transfer filename component and the target pattern extension with compression suffixes stripped. This is a hard error because `systemd-sysext refresh` cannot see the staged image without it. `LinkToSysextAt` replaces the link atomically — a temp symlink (`<link>.tmp-<pid>-<nanos>`) beside it renamed over the old one — so `systemd-sysext` never observes a moment with no link; a failed replacement removes the temp and leaves the old link as it was, and a directory at the link path is preserved (rename refuses it)
   - Vacuum old versions of each transfer, only after the feature's links succeeded (see "Retention" below). Non-dry-run `UpdateResult.RemovedVersions` is not populated because the install path calls `sysext.VacuumWith` without reporting its result; dry-run uses `PlanVacuumWith`
4. Call `systemd-sysext refresh` to reload all extensions (unless `--no-refresh`). The install pipeline never refreshes; a single refresh runs at the end of `UpdateFeatures` and `EnableFeature{Now}`. A failed refresh is never swallowed: `UpdateFeatures` returns `sysext refresh failed: …` (joined with the per-component aggregate error if any) while keeping the results populated, `EnableFeature{Now}` returns the same error with `FeatureActionResult.RefreshError`/`Error` set and `Success=false`. With `--dry-run`, the same manifest/version resolution runs, but `stageTransfer` returns before download; `UpdateFeatures` reports would-download/would-install results and read-only vacuum removals, then skips the final refresh.

### Feature transfers link together

ADR-0016: a feature's transfers must never end a run split between the new
and the old revision. `installTransfers` (`updex/install.go`) runs per feature
in two steps:

1. **Stage.** `stageTransfer` selects the newest version, and downloads and
   verifies it into the staging directory for every transfer of the feature.
   It never changes a link. Staging continues after a failure, so each
   transfer reports its own state.
2. **Link.** Only when every transfer staged, `linkStaged` snapshots each
   old link (`Lstat` + `Readlink`; an absent link is recorded as absent; a
   regular file at the link path is backed up as a hard link beside it,
   `.<name>.raw.updex-backup-<pid>-<nanos>`, so its data can be put back; a
   directory, FIFO or device there is refused, as is a regular file that
   cannot be backed up, before any link moves), then links the transfers
   through the client's runner one by one. When a link fails, it restores the
   links this call already moved with the same atomic temp-symlink-plus-rename
   that `sysext.LinkToSysextAt` uses (an absent link is removed again; a
   backed-up regular file is renamed back over the link), reports the failure
   joined with every restore failure (`errors.Join`), and keeps any backup a
   failed restore could not put back. Backups are removed once the feature
   linked.

When a stage fails, no link is touched, the images already downloaded stay on
disk (the next run finds them staged and current, and only relinks), and the
vacuum does not run. The failing transfer reports its own error (for example
`download failed: …`); every other transfer reports
`not linked: another transfer of feature <name> failed`, with `Version` and
`Downloaded` set from what was staged and `Installed=false`. After a failed
link step, all transfers report the failing transfer's error or the same
`not linked` text. `UpdateFeatures` returns `one or more components failed to
update` joined with every failed transfer's error, and other features
continue. A transfer whose link moved but whose restore failed reports
`failed to restore sysext link for <component>: …; the link may point at the
new version` instead of `not linked`. `EnableFeature{Now}` uses the same
helper and returns the first own error as `failed to download <component>: …`.

### Minimum image version (ADR-0016)

A sysext delta is valid only against a base that has what the delta omits, and
a host can run an old image for weeks. `installTransfers` therefore checks each
image whose link would move, between the stage step and the link step
(`gateMinImage`, `updex/imagegate.go`):

```
stage ──▶ for each transfer that would link: read the staged image
            (sysext.LinkTargetAt → imagecompat.ReadExtensionRelease)
          ├─ key absent ─────────────────────────────▶ link
          ├─ no EROFS filesystem found (ErrUnsupportedImage) ──▶ link   (fail open)
          ├─ other read error / LinkTargetAt fails / bootc fails ──▶ error,
          │                                 link nothing (fail closed)
          └─ key set ─ next-boot IMAGE_VERSION (once per operation)
                       ├─ new enough ────────────────▶ link
                       └─ too old / missing / not digits ▶ link nothing of the
                          feature, keep links and images, no vacuum,
                          report `needs image >= X` (not an error)
```

The check sits on the shared path, so it covers a fresh download and the
relink path alike: an image that was blocked stays staged, the next run finds
it already current and the link missing, and the check runs again, so it links
once the host image is new enough (a reboot into a newer deployment, or a new
staged one). `UpdateFeatures` and `EnableFeature{Now}` both go through it.
`RollbackFeature` does not: it links an older image on purpose, and an older
revision needs at most the base the host already runs.

**Fail open or closed, and why.**

- *Too old, or the version is missing or not numeric:* a normal outcome, not
  an error. The timer must not mark `updex-update.service` failed because a
  revision waits for a newer base; the feature stays on its old links.
- *`ErrUnsupportedImage` (no EROFS filesystem at all: a squashfs sysext,
  random bytes, an invalid GPT): fail open.* Only snosi sets the key and snosi
  ships EROFS. Failing closed would stop every sysext from another catalog
  (squashfs, say) from updating, for a key it cannot carry. Each candidate
  region is classed by the EROFS superblock magic (`0xE0F5E1E2` at offset
  1024) before go-erofs is asked, so "no EROFS found" is the only thing this
  error means.
- *An EROFS filesystem found, and anything else wrong with that candidate:*
  fail closed (an error, nothing linked). Once a candidate has the magic,
  every later problem is an error that does not match `ErrUnsupportedImage`:
  a failed EROFS open, a missing `usr/lib/extension-release.d`
  (`ErrNoExtensionRelease`), a release file that cannot be opened, inspected
  or read or a recovered parser panic (`ErrUnreadableRelease`), a compressed
  image whose `dump.erofs` fallback is missing, older than erofs-utils 1.8.5,
  or fails (`ErrErofsToolUnavailable`, `ErrUnreadableRelease`), a file over
  64 KiB (`ErrReleaseFileTooLarge`). Such an image is snosi-shaped and cannot
  be judged; a silent fallback to the booted os-release could be older than
  the staged image and link a revision that breaks the next boot.
- *A `sysext.LinkTargetAt` failure (no image the link step could link, for
  example a target pattern with a compression suffix):* fail closed. The gate
  must not depend on the link step failing later.
- *bootc installed but failing:* fail closed, and only for features whose
  image has the key. bootc runs once per operation, and never when no image
  has the key.
- *bootc not installed:* fall back to the booted `os-release` (not an
  image-based host).
- *Next boot is a queued rollback (`bootc status --json` reports
  `status.rollbackQueued: true`):* the rollback deployment's image version is
  the next-boot version (`SourceRollback`), ahead of the staged deployment. A
  queued rollback without a version is an error: the next boot is known to be
  the rollback, its version is not.
- *Next boot is a staged deployment without a version (`status.staged` is
  not null, but its `image` or `image.version` is):* fail closed
  (`imagecompat.ErrStagedWithoutVersion`, frostyard/updex#464). Only "no
  staged deployment" falls back to the booted `os-release`. The booted
  version can be newer than the staged one (a `bootc switch` to an older or
  another image), so a fallback could link a revision the next boot cannot
  run. bootc 1.16 fills `image.version` from the OCI **manifest annotation**
  `org.opencontainers.image.version`, not from the config label of the same
  name. **The gate therefore needs that annotation on the OS image**: on an
  image without it, every run that needs the check fails for the features
  with the key while an update is staged (the 2026-10-24 host test found
  this on every published snow image; snosi#1125 adds the annotation). After
  the reboot into the staged image the staged entry is gone, the booted
  `os-release` answers, and the feature links.

**Residual risks.** Two are accepted:

- When the staged deployment fails to finalize at shutdown, the host boots the
  old image, but the link is one the staged image allowed. The check cannot
  see that.
- While a feature is blocked, no vacuum runs for it, so each newer blocked
  revision stays staged (about 1 GB for incus) until the host image catches
  up.

`features check` and `features list` run the same gate read-only
(`stagedImageGate`) for a staged image that is newer than the linked one and
not held by a rollback, and show `needs image >= X`. They never download, so
they say nothing about an image that is only published.

### Refresh gate: the running image (ADR-0016)

The link gate above answers "may the next boot run this image". A
`systemd-sysext refresh` asks a different question: it merges into the base
the host runs **now**. On a bootc host `/etc/os-release` and
`/usr/lib/os-release` describe the booted deployment, so
`imagecompat.BootedImageVersion` reads the running image version from them,
without `bootc` and once per operation (`runningImage`, like `nextBootImage`).
A staged deployment that is newer than the booted one can therefore allow a
link the running base cannot run:

```
booted IMAGE_VERSION=20261001000000, staged deployment=20261101000000
sysext UPDEX_MIN_IMAGE_VERSION=20261022021319
link gate (next boot 20261101000000): new enough ──▶ link 2.0.0
refresh gate (running 20261001000000): too old ──▶ keep the link, skip the refresh
```

`refreshGate` (`updex/imagegate.go`) runs before every refresh updex starts:
`UpdateFeatures` without `NoRefresh`, `EnableFeature{Now}`,
`RollbackFeature{Now}`, and `DisableFeature{Now}`. It checks every sysext the
refresh would merge: the operation's own links (`ownPendingLinks`) and the
other pending activations of the host (`pendingActivationLinks`, whose
transfers are kept so the image can be found; deactivations merge nothing and
are skipped). A candidate is every transfer with a usable link
(`LinkedVersion != ""`) whose merged version is not known to equal it
(`RunningVersion != LinkedVersion`, including `unknown` and `""`, the
fail-closed rule). The display flag `TransferStatus.PendingReboot` and the
refresh warning stay narrower: an unknown merged version is neither reported
as pending nor listed in `PendingActivations`. A transfer whose merged or
linked state cannot be read is unsafe too, with the reason `cannot read the
state of <component>`. Each linked image
(`sysext.StagedImageAt` → `imagecompat.ReadExtensionRelease`) is judged by the
same fail-open/fail-closed rules as the link gate, compared with the running
version: key absent or `ErrUnsupportedImage` is safe; another read error is
unsafe; a key the running image does not meet, or a missing or non-numeric
running version, is unsafe. The link gate is unchanged, so a link the next
boot can run is still linked; only the immediate refresh waits.

What an unsafe result does differs by command, because only some of them can
leave the host consistent without a refresh:

- `UpdateFeatures` and `EnableFeature{Now}` keep their links and skip the
  refresh: `RefreshSkipped` (`refresh_skipped`) says `<component> needs image
  >= <value> (running image <running>)`, a warning goes to the journal, and
  the next action is a reboot. It is not an error, so the timer's replacement
  (`features update --no-refresh`, which never checks) and an interactive run
  behave the same way.
- `RollbackFeature{Now}` keeps the record and the links and skips the
  refresh, also with `RefreshSkipped`, and says the rollback takes effect at
  the next boot.
- `DisableFeature{Now}` cannot skip its refresh: it unmerges first, so a
  skipped refresh would leave every sysext unmerged. The check runs before
  the `Unmerge`, and an unsafe result refuses the whole command — no drop-in,
  no unmerge, no file removal, no refresh — and asks for a run without
  `--now` and a reboot.
- A dry run reports the same text for the first two and the same refusal for
  a disable, and refreshes nothing.

**Sandbox.** The service runs with `SandboxDirectives` (`ProtectSystem=full`,
`RestrictNamespaces=yes`, `SystemCallFilter=@system-service @mount`). Reading
the image needs only `open` and `pread`; `bootc status --json` runs inside the
same sandbox. bootc needs `@mount` there: for `status` it remounts `/sysroot`
read-write with `mount -o remount,rw` (bootc `open_dir_remount_rw`), and
`@system-service` alone makes seccomp kill that `mount` with `SIGSYS`, so the
gate failed closed on every timer run (frostyard/updex#463, host test of
2026-10-24 on snow with bootc 1.16.8 and the composefs backend). The remount
stays in the service's own mount namespace: `ProtectSystem=` gives the
service one, systemd does not propagate mounts from it back to the host, and
bootc (`ensure_self_unshared_mount_namespace`) sees that it is already in a
namespace other than PID 1's and does not `unshare` (which
`RestrictNamespaces=yes` refuses). It is the same remount that bootc makes in
its own namespace when an administrator runs `bootc status`. Why `@mount`
costs little: updex runs as root, can reach the systemd manager over
`AF_UNIX`, and links images that `systemd-sysext` merges into `/usr`, so the
sandbox contains bugs, not an attacker with code execution in updex. It does
widen the syscalls the whole service may make, not only bootc. updex itself
never calls `mount` and never mounts a sysext image; only the `bootc status`
subprocess remounts `/sysroot`, in the service's namespace. "Does not mount"
in the `imagecompat` section below means updex, not that subprocess. The host test found that no single `@mount`
syscall, and no subset of 7 of them, is enough, so the unit allows the group.
`TestSandboxRunsBootcStatus` (`systemd/sandbox_bootc_test.go`) runs
`bootc status --json` under every `SandboxDirectives` line with
`systemd-run --wait --pipe`; it runs only as root on a host with `bootc` and
`systemd-run`, and skips elsewhere (CI included). An operator repeats it with
`sudo go test -run TestSandboxRunsBootcStatus ./systemd/`. A bootc that the
sandbox blocks shows as the component's `Error` (fail closed).

### Retention (vacuum)

ADR-0016. After a feature's links succeeded, `sysext.VacuumWith` keeps, for
each transfer: the version that the sysext link `<SysextLinkDir>/<link name>`
names (`LinkedVersionAt`); the version merged now, read from the hierarchy
metadata (`mergedVersionInHierarchy`, ADR-0015, over the client's
`sysextHierarchies`; `UnknownVersion` protects nothing); `ProtectVersion=`;
`VacuumPaths.ProtectVersions` (Phase 4 passes the rejected rollback version
here); the legacy `CurrentSymlink` target; and the newest `InstancesMax`
(default 2) versions. Protected versions are kept in addition to the newest
`InstancesMax` (one among the newest counts as one of them), so a host that has linked a new revision but still runs an older one
keeps up to 3 images per transfer. A dangling link protects nothing. The
protected set is read before anything is deleted; a failure to read the link
or the merged state fails the vacuum with every image still on disk, and
`installTransfers` warns (`vacuum failed for <component>: …`) instead of
guessing. The package-level `Vacuum`, `VacuumAt`, `VacuumWithDetails`,
`PlanVacuumAfterInstall` and `PlanVacuumAfterInstallAt` remain compatibility
wrappers that do not know the link or the hierarchies.

### Rollback (ADR-0016)

`updex features rollback <name> [--to VERSION] [--now] [--clear]` is
`Client.RollbackFeature` (`updex/rollback.go`). It plans first and changes
nothing when any transfer of the feature blocks it:

- **Target.** Per transfer, the newest staged version older than the linked one
  (`sysext.StagedVersionsAt`, `LinkedVersionAt`, `version.Compare`), or `--to`
  for all. With several transfers each one goes to its own newest older version
  (a feature's transfers share a revision in practice, but the rule must not
  invent a common version when they differ). A missing link, a missing staged
  version, a target equal to or newer than the linked version: the error lists
  every blocking transfer.
- **Record.** `<RollbackDir>/<feature>`, JSON, `format` 1: `{"format":1,
  "feature":…,"created":…,"rejected":{"<component>":"<version>"}}`. One rejected
  version per transfer: the version rolled back from, or a higher one an earlier
  record already holds, so a second rollback never lowers the hold. JSON with an
  explicit `format` lets a later updex change the shape while an older one fails
  closed on it. Written with a temp file plus rename after `Lstat` checks of the
  directory and file; the feature name goes through `catalog.ValidateSysextName`
  first. The record names only the rolled-back feature; its hold covers every
  feature that shares a transfer with it: those features are held, and only the
  rolled-back feature advances the shared transfers (see "Update path").
- **Order.** Record first, then the links (`linkAllOrNone` with
  `sysext.LinkVersionAt`), then the optional refresh. A crash between the record
  and the links leaves a hold on the current link, which is harmless; the other
  order would leave a rolled-back link that the next update undoes. A link
  failure restores the links (Phase 1 snapshot/restore) and the previous record.
- **Update path.** A transfer can belong to several features (`Features=` OR,
  `RequisiteFeatures=` AND), so `installTransfers` reads not only the record of
  the feature it processes but every record that applies to its transfers
  (`collectRollbackHolds`): the record of each feature that names one of them,
  enabled or not, each read once per call. For a component that several records
  reject, the highest rejected version wins (`version.Compare`). An unreadable
  applicable record fails closed for that feature only (nothing downloaded or
  linked, an error on every transfer, naming the feature whose record failed).
  **Only the feature that wrote a record advances the transfers its record
  names.** When a record of another feature G applies, the whole feature is
  held: no download, no link, `UpdateResult.Held`, and `NextActionMessage`
  `held by rollback of grp: it advances only when grp updates (or run 'updex
  features rollback --clear grp')`. G's transfers link all or none and its
  record is deleted only after that link, so the hold keeps the rolled-back
  feature whole in every processing order and after any download or link
  failure in G; a sharing feature never links an image G staged in a failed
  run. The hold lasts while the record exists, also when G is disabled or
  masked, and updex never clears the record by itself. If two features that
  share a transfer both have a record, each holds the other until one record is
  cleared.
  For G's own record, a record still holds when any component it rejects has no
  available version newer than the rejected one (`rollbackHoldReason`). That
  covers every component the record names, also one outside the feature's
  transfers: its transfer is looked up in the loaded domain and its versions
  are read through the call's manifest cache. A component without a transfer in
  the domain, or whose versions cannot be read, holds too (fail closed); a
  version lookup that fails is an error for the feature, like an unreadable
  record. When the record still holds, the whole feature is held: no download,
  no link, so the stage step's "already staged, relink" branch can never point
  the link back at the newest staged image, which is the rejected one. Held is
  a result state (`UpdateResult.Held`, `RejectedVersion`, and
  `NextActionMessage`), not an error; a transfer held only because another
  component of the record has not advanced says so (`held by rollback of grp:
  beta has no version newer than 2.0.0`). A newer revision is staged and linked
  together as usual only when the feature's own record does not hold; the
  vacuum then runs with the rejected version in `VacuumPaths.ProtectVersions`,
  and afterwards the record is deleted when every version it rejects is behind
  a link now (`sysext.LinkedVersionAt`; a component of the record outside this
  feature's transfers is looked up in the loaded domain, and a record whose
  component has no transfer or no readable link stays).
  `deleteFinishedRollbackRecords` deletes only the processed feature's record,
  so a record is deleted only by an update of the feature that owns it.
  `EnableFeature{Now}` shares this path.
- **Status.** `TransferStatus.RollbackRejected` and `CheckResult.RollbackRejected`
  (JSON `rollback_rejected`), from the same `collectRollbackHolds`, for every
  feature that shares the transfer; `UpdateAvailable` is false while the newest
  version is not newer than the rejected one. `RollbackHeldBy` (JSON
  `rollback_held_by`) names why the record holds the transfer although it could
  advance, from the same decision the update path makes: the record component
  with no newer version for the feature's own record
  (`rollbackHoldReason`), or `<owner>: it advances only when <owner> updates
  (or run 'updex features rollback --clear <owner>')` for a record of another
  feature (`rollbackHolds.ownersOtherThan`). `UpdateAvailable` is false while
  it is set. `Features` reads manifests only when the feature's own record
  applies, because its hold needs the newest versions; a record of another
  feature is decided without them, and both status calls stay read-only. The
  CLI marks the transfer `held (rejected X)` or `held (by rollback of …)` in
  `features check` and with a `HELD BY ROLLBACK:` footer in `features list`.
- **`--now`** refreshes through the runner after listing the other pending
  activations; without it the rollback applies at the next boot. `--clear`
  deletes the record and changes no link; a missing record is success.
- **CLI.** Root is required except with `--dry-run`. The command takes the
  operation lock like every mutating command.

### Operation lock

Every mutating SDK operation (`UpdateFeatures`, `EnableFeature`,
`DisableFeature`, `CatalogAdd`, `CatalogRemove`, `EnableDaemon`,
`DisableDaemon`) holds an exclusive, non-blocking `flock` on
`RuntimePaths.LockPath` (default `/run/updex/lock`, `updex.DefaultLockPath`)
from its start until it returns (`acquireLock` in `updex/lock.go`, through
`Client.lockMutation`). Dry runs and read-only operations (`Features`,
`CheckFeatures`, `CatalogList`, `DaemonStatus`, `Components`) do not take it.
When the lock is taken, the operation changes nothing and returns an error
wrapping `updex.ErrBusy` (`another updex operation is running`); updex never
waits. `acquireLock` creates the parent directory (0755) and the file (0600),
`Lstat`s both first, refuses a symlink or non-regular file, and opens with
`O_NOFOLLOW` (ADR-0005). The kernel drops the lock when the process exits, so
there is no stale lock, and `/run` is cleared at boot. The CLI turns
`ErrBusy` into exit status 0 with one stderr line only for `features update`
when `INVOCATION_ID` is non-empty (systemd sets it for every service; the
packaged timer runs exactly `updex features update --no-refresh`, so it cannot
take a flag); in an interactive shell it is an error and the exit status is
non-zero. The lock covers `systemd-sysext refresh` as well. The sandboxed
service (`ProtectSystem=full`) can write `/run`.

### Reading a sysext's extension-release without mounting (`imagecompat/`)

ADR-0016 ("Minimum image version") needs one value from inside a downloaded
image: `UPDEX_MIN_IMAGE_VERSION` in `usr/lib/extension-release.d/extension-release.<name>`.
The obvious tools are wrong for it: the image is untrusted input, and
`systemd-dissect`, `mount` or a loop device would hand it to the kernel's
filesystem code as root before updex decides whether to link it. The
auto-update service also has `RestrictNamespaces=yes` (see
`systemd.SandboxDirectives`; its `@mount` exists only for bootc, see
"Sandbox" above). `imagecompat.ReadExtensionRelease` therefore reads the file
in pure Go, with plain `ReadAt` on the staged file:

1. `Lstat` and `O_NOFOLLOW` open; refuse anything that is not a regular file.
2. Look for a GPT header at LBA 1 for 512-byte sectors, then for 4096-byte
   sectors (`EFI PART`). A GPT is validated before it is trusted: header size,
   header CRC32, entry count (at most 256) and size (128 to 1024, multiple of
   8), the entry array inside the file, the array CRC32. Every used partition
   inside the file becomes a candidate (at most 16 are opened); no GUID filter,
   because snosi uses the root partition types but another builder may not.
   Without a GPT, the whole file is the candidate (a bare EROFS image).
3. Read each candidate that has the magic. An uncompressed filesystem is
   opened with `github.com/erofs/go-erofs` through an `io.SectionReader` (so
   the filesystem cannot read outside its partition); the first candidate
   that has `usr/lib/extension-release.d` wins. go-erofs refuses a compressed
   filesystem (`unsupported compressed filesystem`), so a compressed
   candidate is read with the erofs-utils `dump.erofs` program instead, which
   prints a file out of an image without mounting it.
4. Pick the file: the only regular `extension-release.*`, else the one named
   `extension-release.<name>`; read it with a 64 KiB cap and parse it with
   `ParseOSRelease`. Both readers apply these rules.

**Compression, decided from the superblock, not from an error message.** The
candidate is compressed when the EROFS superblock's `feature_incompat` (a
little-endian uint32 at byte 80) has `EROFS_FEATURE_INCOMPAT_LZ4_0PADDING`
(0x1) or `EROFS_FEATURE_INCOMPAT_COMPR_CFGS` (0x2), or when
`available_compr_algs` (a little-endian uint16 at byte 84, a union with
`lz4_max_distance`) is not zero. mkfs.erofs writes `feature_incompat=0x1,
available_compr_algs=0xffff` for lz4/lz4hc and `0x3, 0x8` for zstd, and
nothing for an uncompressed image. Reading the fields is deterministic and
does not depend on go-erofs' wording. An uncompressed image always takes the
pure-Go path: all frostyard sysexts are uncompressed, so the fallback only
runs for an image that would otherwise fail.

**The `dump.erofs` fallback.** `imagecompat.ErofsDumpRunner` is the seam.
`DefaultErofsDumpRunner` runs the real program found in `PATH`;
`updex.ClientConfig.ErofsDumpRunner` carries one runner per client, and the
link gate, the refresh gate and the status reads (`features list`,
`features check`) all pass it to `imagecompat.ReadExtensionRelease`. The
command is `dump.erofs --offset=<partition offset> --cat
--path=/usr/lib/extension-release.d/extension-release.<name> <image>`; the
name comes from `dump.erofs --offset=<n> --ls
--path=/usr/lib/extension-release.d <image>` first, so a directory or a
symbolic link named like the release file is never printed as the file.

The subprocess is bounded and cancellable:

- `exec.CommandContext` with the caller's context kills it, and
  `WaitDelay` (2 s) keeps a killed tool that still holds the pipe from hanging
  the caller; one command is also capped at two minutes, because the timer
  runs with no deadline of its own. No shell is used.
- The environment is minimal: `PATH` and `LC_ALL=C`, so the messages and the
  version line have one form whatever the caller's locale.
- Output limits: 64 KiB for `--cat` (the same as `MaxReleaseFileSize`,
  beyond which the read is `ErrReleaseFileTooLarge`), 256 KiB for `--ls`
  (`MaxErofsDumpListSize`), 4 KiB for `-V`. Over a bound fails closed.
- erofs-utils prints some failures on stderr and still exits 0 (a missing
  inode is `<E> erofs: read inode failed @ <path>`), so a stderr line that
  starts with `<E>` is an error; `<W>` lines are ignored.
- `dump.erofs -V` runs before the first use and must report at least
  `ErofsUtilsMinVersion` (1.8.5, which added `--cat`; `--offset` and `--ls`
  came with 1.8). An older or unparseable version fails closed with
  `dump.erofs <version> is too old; erofs-utils >= 1.8.5 is required to read
  compressed EROFS images`.
- Every name that `--ls` lists is validated before it reaches a path:
  printable ASCII, no `/`, at most 255 bytes; `../x` or `a/b` fails closed.
- `dump.erofs` opens the image path itself, so after the read the path must
  still name the file the caller opened (`os.SameFile`); a swap fails closed.

An image that is not GPT-with-EROFS or bare EROFS (for example squashfs)
returns `ErrUnsupportedImage`; the caller decides. A compressed EROFS image
is not an unsupported image: it is read through the fallback, and a host
without a new enough `dump.erofs` fails closed with `ErrErofsToolUnavailable`
rather than skipping the check. The image's SHA256 was verified earlier, but
both readers treat it as untrusted: a parser panic in go-erofs is turned into
an error and every fallback output is bounded and validated, so one bad image
cannot end the run for the other features.

`NextBootImageVersion` does not mount either: it runs `bootc status --json`
(bootc's `Host` schema: `status.rollback`, `status.staged` and
`status.booted` are `BootEntry` objects or null, and `image.version` is a
string or null) and takes, in order: `.status.rollback.image.version` when
`status.rollbackQueued` is true; else `.status.staged.image.version` when
`status.staged` is not null; else the booted `IMAGE_VERSION` from
`os-release`. A queued rollback or a staged deployment without an image
version is an error (`ErrStagedWithoutVersion` for the staged one), never a
fallback. A missing bootc falls back; a bootc that fails does not (fail
closed). `CheckMinImageVersion` compares digit strings as numbers
without conversion.

### Feature check (end-to-end)

`CheckFeatures` never writes; it resolves the newest available version exactly as the update path does, then compares it with the version the host actually runs.

- `CurrentVersion` comes from `sysext.LinkedVersionAt` when the systemd-sysext link `<SysextLinkDir>/<component>.<ext>` resolves: that link is what systemd-sysext merges, so it is authoritative. This matters because a second updater on the same host (for example `systemd-sysupdate`) can stage a newer image in `/var/lib/extensions.d` and move its own legacy `CurrentSymlink` to it without touching the link updex made; comparing against staging then reported `Up to date` while the host kept running the older image.
- When no usable link exists — absent, not a symlink, dangling, or naming an image the transfer's target patterns do not match — `CurrentVersion` keeps the staging-based value `sysext.GetInstalledVersions` reports (legacy `CurrentSymlink` target, else newest staged file). `GetInstalledVersions` itself stays staging-based: `stageTransfer` depends on that to recognise a staged image and skip its re-download.
- `UpdateAvailable` is `version.Compare(newest, CurrentVersion) > 0` over that value, so an older linked image against a newer available one reports an update, which the next `features update` resolves by relinking without a download.
- A link-inspection failure (a failed `Lstat`/`Readlink`, or an unusable target pattern) is reported as that component's `CheckResult.Error` (`failed to inspect sysext link: …`) with `NewestVersion` kept, the same partial-results shape as a manifest failure.

### Status: running and linked (ADR-0016)

Two different things can be called "the version of a sysext", and they differ for a while after every update:

- **running** — what systemd-sysext merged at its last merge or refresh. Only the `.systemd-sysext` metadata under the hierarchies (`/usr`, `/opt`; the client's `sysextHierarchies`) says so ([ADR-0015](../adr/0015-detect-merged-sysexts-from-hierarchy-metadata.md)). `sysext.MergedVersionIn` reads just that, so a legacy `CurrentSymlink` or an image in `/run/extensions` (both count as "active" in `GetActiveVersionWith`, which exists to guard destructive operations) does not count as merged. `unknown` is a valid value.
- **linked** — what `<SysextLinkDir>/<component>.<ext>` names (`sysext.LinkedVersionAt`). The next boot, or the next `systemd-sysext refresh`, merges it.

`updex/status.go` holds `TransferStatus` (both values per transfer), the pending rule, and the refresh-warning helpers. `Features` attaches a `TransferStatus` to every `FeatureInfo` (enabled or not); `CheckFeatures` copies the values into `CheckResult` (`running_version`, `linked_version`, `pending_reboot`) and keeps `current_version` as before (link, else staging). Unreadable state is local to one transfer: a warning in `Features`, that row's `Error` in `CheckFeatures`.

*Pending reboot* means linked is non-empty and differs from running. It is not asserted when running is `unknown` (no comparison is possible) or when the state could not be read. The CLI shows it as a `PENDING REBOOT:` footer under the `RUNNING`/`LINKED` columns of `features check` and `features list`.

The same helper drives the **refresh warning**: a refresh activates every pending link, not only the operation's own, so `UpdateFeatures` (without `--no-refresh`), `EnableFeature --now` and `DisableFeature --now` list the *other* pending transfers (those outside the operation's own transfers, matched by link name; both activations, a link that names a version not running, and deactivations, a merged sysext with no link, which the refresh unmerges) through the reporter before refreshing, and put them in `PendingActivations` on the result. With `--dry-run` the same list says what would be activated. The domain is the whole host even for a `--component` run, or a run under a `Definitions` override. `DisableFeature` reads it before `Unmerge`, which clears the merged state. See `docs/specs/sdk-api.md` ("Refresh warning") for the exact rules. The timer path (`--no-refresh`) never lists anything because it never refreshes.

### Enable/disable feature

`00-updex.conf` is the single drop-in updex owns; administrator drop-ins
always override and always survive (decision recorded in
[ADR-0004](../adr/0004-single-updex-drop-in.md)).

- **Enable**: Creates drop-in at `/etc/sysupdate.d/<name>.feature.d/00-updex.conf` (or `/etc/sysupdate.<component>.d/<name>.feature.d/00-updex.conf` for a component-scoped feature — see "Components" above) setting `Enabled=true`. With `--now`, also downloads extensions immediately. The write (`writeFeatureDropIn`, shared with disable) follows [ADR-0005](../adr/0005-transactional-writes-lstat-checks.md): the `<name>.feature.d/` directory is `os.Lstat`-checked and created only when absent — a symlink or a file at that path is refused (`drop-in directory … exists and is not a directory; remove it manually`) rather than descended into; the drop-in path is checked with `managedFileExists` (`updex/fsguard.go`), so a symlink there (dangling or live) is refused (`… is not a regular file …`) rather than written through; and the file is written as a fresh 0644 regular file via temp-file-plus-rename in the drop-in directory (`writeManagedFile`), so the write itself never follows a link that appears between check and write and a failure leaves no truncated file or temp debris. `CatalogAdd`'s follow-up `EnableFeature{Now}` surfaces the same errors and rolls back.
- **Disable**: Creates drop-in setting `Enabled=false` at the same scoped path, through the same guarded write. With `--now`, calls `Unmerge()`, removes symlinks from `/var/lib/extensions/`, and deletes all versioned files. Before removal, `DisableFeature` treats an image as active when `sysext.GetActiveVersionWith` finds it through any of three signals, in order (decision recorded in [ADR-0015](../adr/0015-detect-merged-sysexts-from-hierarchy-metadata.md)): (1) a legacy transfer `CurrentSymlink`; (2) the merged-state metadata systemd-sysext records in each captured `RuntimePaths.SysextHierarchies` entry (production default `/usr`, `/opt`) under `.systemd-sysext/`: the `origin` JSON for systemd 258 and newer (its `extensions.<name>.path` base name yields the version), and when `origin` is absent, malformed, or does not name the image, the older `extensions` name list is also checked (which yields `unknown` because it records no version); and (3) an entry in the client's captured `RuntimePaths.RunExtensionsDir` (production default `/run/extensions`, a systemd-sysext image *search* directory whose image is merged on the next refresh — it is not a merged-state record). The `/var/lib/extensions` link is not an active signal: it makes an image available for a future merge but does not prove the image is currently merged. `--force` is required when any active signal matches. The guard fails closed: when the active-state check returns an error (for example an unreadable hierarchy), the component is listed as `merge state unknown` and `--force` is required, with the error still reported as a warning. Forced removal reports that a reboot is required. The closing `systemd-sysext refresh` (re-merging the remaining extensions) is the one step that runs after `Unmerge()` has already detached everything: if it fails, `DisableFeature` returns `sysext refresh failed: …` with `RefreshError`/`Error` set, `Success=false`, `Unmerged=true` and `RemovedFiles` still recorded, and a `NextActionMessage` stating that all extensions are currently unmerged and a manual `systemd-sysext refresh` (or reboot) is required — the CLI prints that and exits non-zero instead of the reboot hint.

### Auto-update daemon

The daemon links new revisions but never activates them: a run is
`updex features update --no-refresh`, and a new revision takes effect at the
next boot. The decision is [ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md),
which supersedes [ADR-0007](../adr/0007-daemon-stages-never-activates.md).

- **One source for the unit text.** `updex.DaemonUnits()` returns the
  `systemd.TimerConfig` and `systemd.ServiceConfig`. `daemon enable` and the
  release build both render them with `systemd.GenerateTimer`/`GenerateService`,
  so the packaged files and the files `daemon enable` writes cannot drift
  (`updex/units_hook_contract_test.go` compares them byte for byte).
- **Packaged units.** The `frostyard-updex` package installs
  `/usr/lib/systemd/system/updex-update.timer` and `.service`, disabled (no
  maintainer script). The hidden `updex-cli units --output-dir DIR` command writes
  them, `scripts/units.sh` runs it as a GoReleaser `before.hooks` entry (next to
  `completions.sh` and `manpages.sh`; output in the gitignored `units/`), and
  `nfpms.contents` installs them with mode 0644. CI verifies them with
  `systemd-analyze verify` (`units` job in `test.yml`) and the nightly checks the
  `.deb` listing (`scripts/check-deb-units.sh`, `snapshot.yml`).
- **The units.** Service: the sandboxed `oneshot` (`SandboxDirectives`),
  `ExecStart=/usr/bin/updex features update --no-refresh`,
  `Wants=`/`After=network-online.target` and `ConditionACPower=true` (a host on
  battery skips the run; a host without a battery always runs), and
  `TimeoutStartSec=2h` (a backstop: systemd stops a stuck run, which releases
  `/run/updex/lock`; per-request timeouts and ADR-0008's bounded retries end a
  normal run long before). An offline host can still reach
  `network-online.target`; its downloads fail and the run exits non-zero, so the
  unit shows `failed` until the next successful run (ADR-0016: a failed feature
  fails the run). Timer:
  `OnCalendar=*-*-* 00/6:00:00`, `RandomizedDelaySec=3600s`, `Persistent=true`,
  `[Install] WantedBy=timers.target`. `systemd.ServiceConfig` gained `Wants`,
  `After` and `Conditions` (`[]string`; `Conditions` are whole directive lines),
  `TimeoutStartSec` (a string written after `ExecStart`),
  and both configs a `Comment` (first line `# <Comment>`); a config that sets none
  of them generates the same text as before.
- `updex daemon enable`: when the manager's `PackagedUnitPath` has
  `updex-update.timer`, it runs `systemctl enable` then `start` (the same as
  `enable --now`) and writes nothing; unit files already in `/etc/systemd/system`
  are reported as overriding the packaged ones. Otherwise (an updex binary
  installed without the package) it refuses when `/etc` unit files already exist,
  else writes both units there with the first-line marker
  `# Generated by updex daemon enable; updex daemon disable removes this file.`,
  reloads systemd, enables and starts the timer. Enabling a masked timer fails
  with systemctl's error.
- `updex daemon disable`: stops and disables the timer, removes only the
  `/etc/systemd/system` unit files that updex wrote, then reloads systemd. A file
  is updex's when its first line is the marker, or when its whole content equals
  one of the frozen ADR-0007 texts that earlier releases wrote without a marker
  (`updex/daemon_legacy.go`: the daily timer, and the sandboxed and unsandboxed
  service). The match is exact on purpose: an edited or differently shaped file is
  kept and named in `DaemonActionResult.KeptFiles` and the message, since deleting
  a file that updex cannot prove it wrote could remove an administrator's unit or
  one that the OS image ships in `/etc`. Every file is checked with `os.Lstat`; a
  symlink, directory or other non-regular file is refused and left alone
  ([ADR-0005](../adr/0005-transactional-writes-lstat-checks.md)). The packaged
  units are never touched. A disable with only packaged units stops and disables
  the timer; the opt-out that survives image updates is `systemctl mask`.
- `updex daemon status`: `Installed` means a timer file exists in either directory.
  `Source` is `etc` when the timer file in use is in `/etc/systemd/system` (it
  overrides the packaged one), else `packaged`; `UnitFiles` lists the files in use and `Schedule` is the timer's effective
  `OnCalendar` schedule: the `TimersCalendar` property that `systemctl show`
  reports, so administrator drop-ins such as
  `/etc/systemd/system/updex-update.timer.d/*.conf` and the reset meaning of an
  empty `OnCalendar=` count, or, when systemd cannot report it (a runner
  without `Show`, a failing query, no calendar trigger), the schedule computed
  from the timer file in use plus its drop-ins in `/etc`, `/run` and the
  packaged directory, by systemd's rules (`OnCalendar` appends inside `[Timer]`,
  an empty value clears the list, and a drop-in name in a higher-priority
  directory hides the same name below). The
  next run and last run come from `systemctl show -p PROPERTY --value`, one call
  per property through the `systemd.ShowRunner` interface (an optional runner
  interface, like `ContextSystemctlRunner`; a runner without it leaves them
  empty): the timer's `NextElapseUSecRealtime` (`NextRun`) and the service's
  `ExecMainExitTimestamp` (`LastRun`) and `Result` (`LastResult`), all as systemd
  prints them. An empty value, `n/a` and `0` mean none: `NextRun` is empty when
  nothing is scheduled, and `LastRun` and `LastResult` are empty when the service
  has not run since boot (`Result` is `success` for a service that never ran, so it
  is read only when `LastRun` is set). A failed query fails the command, as for the
  enabled and active queries.
- `--dry-run` on `daemon enable`/`daemon disable` passes `DryRun` into the SDK options: the context check and the installed/not-installed precondition still run, but no unit file is written or removed and no systemctl call is made. The `DaemonActionResult` carries `DryRun` and the absolute `UnitFiles` paths (timer then service), and the CLI prints the result's `Message` with a `[DRY RUN] ` prefix in text mode
- `Client.EnableDaemon`, `Client.DisableDaemon`, and `Client.DaemonStatus`
  own unit construction and lifecycle sequencing; the daemon CLI retains only
  root authorization, SDK invocation, and text/JSON formatting
- Unit installation (the `/etc` path, `Manager.Install`) refuses to overwrite existing timer/service files; callers must disable first. The existence check is `os.Lstat`-based per [ADR-0005](../adr/0005-transactional-writes-lstat-checks.md) (`systemd.unitFileState`): a symlink (dangling or live), directory, or other non-regular entry at either unit path is refused outright (`unit path … exists and is not a regular file; remove it manually`) rather than written through, and each unit is written as a fresh 0644 regular file via temp-file-plus-rename in the unit directory (`systemd.writeUnitFile`), so the write never follows a link that appears between check and write. `Manager.Exists` uses the same Lstat view and treats any occupied unit path — including a dangling symlink — as present, so `daemon enable` reports "already installed" instead of attempting a write the guard would reject, and `daemon status` never reports a planted entry as absent
- The service runs as root, so `DaemonUnits()` sets `systemd.ServiceConfig.Sandbox` and `GenerateService` appends the `systemd.SandboxDirectives` block to `[Service]`: `NoNewPrivileges=yes`, `ProtectSystem=full`, `ProtectHome=yes`, `PrivateTmp=yes`, `ProtectKernelTunables=yes`, `ProtectKernelModules=yes`, `ProtectKernelLogs=yes`, `ProtectControlGroups=yes`, `ProtectClock=yes`, `ProtectHostname=yes`, `RestrictRealtime=yes`, `RestrictSUIDSGID=yes`, `RestrictNamespaces=yes`, `LockPersonality=yes`, `MemoryDenyWriteExecute=yes`, `SystemCallArchitectures=native`, `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, `SystemCallFilter=@system-service @mount` (`@mount` lets `bootc status` remount `/sysroot` inside the service's own mount namespace for the minimum image version check, frostyard/updex#463; see "Sandbox" in the minimum image version section)
- `ProtectSystem=full` (not `strict`) was chosen so `/var` stays writable without a `ReadWritePaths=` list: the default `/var/lib/extensions.d` staging directory, the `/var/lib/extensions` link directory, and hand-written transfers with a `Target.Path` elsewhere under `/var` keep working; `/usr`, `/boot`, `/efi`, and `/etc` are read-only, which the `--no-refresh` staged path never writes. No `CapabilityBoundingSet=` is set. Other `GenerateService` callers keep the minimal unit unless they opt in
- In-flight cancellation: `EnableDaemon`, `DisableDaemon`, and `DaemonStatus` thread their `ctx` through every `systemd.Manager` operation they call (`Install`, `Remove`, `Enable`, `Start`, `IsEnabled`, `IsActive`, and the `daemon-reload` each triggers). `Manager` checks `ctx` before touching the runner at all, then prefers the runner's `ContextSystemctlRunner` methods (`DaemonReloadContext`, `EnableContext`, `DisableContext`, `StartContext`, `StopContext`, `IsActiveContext`, `IsEnabledContext`) when it implements that optional interface, falling back to the legacy `SystemctlRunner` methods otherwise so existing external runners stay source-compatible without gaining cancellation. `DefaultSystemctlRunner` implements both: its `*Context` methods run `systemctl` via `exec.CommandContext` and its legacy methods delegate to them with `context.Background()`. If `ctx` is canceled or expires while a `systemctl` subprocess is running, the process is killed immediately and the call returns promptly with an error matching `context.Canceled`/`context.DeadlineExceeded` (`errors.Is`) rather than the raw "signal: killed" process error

## CLI Commands

```
updex features list                     List all features with status (alias: updex feature)
                                         CATALOG column shows provenance: a catalog
                                         name, image:<id>, local:etc|usr|run, or unknown;
                                         RUNNING (merged now) and LINKED (next boot)
                                         hold one value per transfer, in TRANSFERS order
updex features enable <name>            Enable a feature
  --now                                 Download extensions immediately
updex features disable <name>           Disable a feature (without --now the
                                         extensions stay installed and merged)
  --now                                 Unmerge and remove files immediately
  --force                               Allow removal of merged extensions
updex features update                   Download and install new versions
  --no-vacuum                           Skip removing old versions
  --dry-run                             Preview update work without filesystem/sysext changes
updex features rollback <name>          Link every transfer to an older staged version and
                                         record the rejected one; update holds until a newer
                                         version is published
  --to <version>                        Link this staged version (all transfers)
  --now                                 Apply with a systemd-sysext refresh
  --clear                               Delete the rollback record, change no link
updex features check                    Check for available updates; CURRENT is the version
                                         the systemd-sysext link points to (staging
                                         fallback when there is no usable link);
                                         RUNNING is the merged version, LINKED the one the
                                         next boot merges (PENDING REBOOT line when they
                                         differ); a
                                         component that cannot be checked is reported
                                         with UPDATE=error (JSON `error`) and the
                                         command exits non-zero
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

updex daemon enable                     Enable the auto-update timer (packaged
                                         units; writes marked /etc units only
                                         without the package)
updex daemon disable                    Stop and disable the timer; remove only
                                         the /etc units updex wrote
updex daemon status                     Timer state, units in use, next run,
                                         last run and result

Global flags:
  -C, --definitions <path>              Custom path to config files (bypasses component
                                         discovery entirely; mutually exclusive with --component)
  --verify                              Enable GPG verification
  --no-refresh                          Skip systemd-sysext refresh
  --json                                Output as JSON (from clix)
  --dry-run                             Preview without modifying filesystem (from clix)
  --verbose                             Enable debug output (from clix)
```

`features rollback` is the exception: it requires root only without `--dry-run`. Other mutating commands enforce root before reading `--dry-run`, so examples that preview `features enable`, `features disable`, `features update`, `daemon enable`, or `daemon disable` may still need `sudo` when run through the CLI.

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
| `github.com/erofs/go-erofs` | EROFS reader for `imagecompat/` (Apache-2.0, pure Go, standard library only) |

## CI and Releases

`make ci` is the canonical credential-free local gate. It mirrors the
host-independent pull-request checks in fail-fast order: the shared
`make verify-static` checks (module tidiness, vet, formatting, golangci-lint
at the exact pin), the docs-integrity checker's fixture self-test
(`make test-docs-check`) and the docs-integrity gate itself
(`node scripts/check-docs.mjs`, the same two steps as the `docs-gate` job, so
`make ci` needs Node.js), all non-E2E package tests (with `-coverprofile`),
the `max(80.0, baseline - 0.5)` total statement-coverage gate
(`make test-coverage-check` (`scripts/test-coverage-check.sh`) then
`make coverage-check` (`scripts/check-coverage.sh`), with the committed
`.coverage-baseline`), the black-box `tests/e2e/` suite as a separate
non-coverage step, the same unit tests under the race detector, then Linux
amd64 and arm64 builds. The unit and race stages do not filter by test name,
so every hermetic package test runs; the E2E suite also retains its dedicated
workflow job. `make verify` is the non-mutating subset for read-only
reviewers (such as Snowcat `pr-review` workers): `verify-static` plus the
non-E2E tests, with nothing that formats or rewrites the checkout.

The credentialed `Release config` job in `.github/workflows/test.yml` closes
the remaining pre-merge release-surface gap: on pull requests, pushes to
`main`, and merge-queue branches it runs the same SHA-pinned GoReleaser Pro v2
action as the publishing workflows, with `args: check`, read-only repository
permission, and checkout credentials disabled. Trusted runs fail if
`GORELEASER_KEY` is unavailable, rather than passing without validation. Fork
and Dependabot pull requests cannot receive that secret, so their job explicitly records that
the configuration was not validated; the trusted merge-queue run supplies the
enforced pre-merge result.

`.github/workflows/pr-title.yml` (`amannn/action-semantic-pull-request`,
SHA-pinned, `pull_request` `opened|edited|synchronize|reopened` plus
`merge_group`, read-only permissions, no checkout) fails a pull request whose
title is not a Conventional Commit, and — because the repository squash-merges with the
"commit or PR title" default, under which a single-commit PR lands under its
commit's subject — also validates the lone commit's subject
(`validateSingleCommit: true`). The accepted types mirror `CONTRIBUTING.md`
and the `.goreleaser.yaml` changelog groups. `Conventional PR title` is one of
the contexts the default-branch ruleset requires, so the workflow also runs on
`merge_group`: there is no pull-request title to lint on a merge-queue branch,
so the validation step carries `if: github.event_name == 'pull_request'` and
the job succeeds with that step skipped. The guard is on the step, never on
the job — a required job excluded from the event never reports and stalls the
queue ([frostyard/core ADR-0042](https://github.com/frostyard/core/blob/main/docs/adr/0042-adopt-a-merge-queue-on-the-default-branch.md)).

`.github/workflows/release.yml` publishes tagged GoReleaser Pro releases and
packages, attests build provenance for `dist/checksums.txt` and every
distributable asset (`actions/attest-build-provenance`, SHA-pinned; workflow
permissions `id-token: write` and `attestations: write`; users verify with
`gh attestation verify <artifact> --repo frostyard/updex`, see README
"Installation"), then dispatches `event-type: build` to `frostyard/snosi` so the
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
