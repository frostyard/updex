# SDK API Reference

The `updex` package (`github.com/frostyard/updex/v2/updex`) is the primary public API. All operations go through the `Client` struct.

## Library (SDK) Usage

Create a module for this example:

```bash
mkdir updex-quickstart
cd updex-quickstart
go mod init example.com/updex-quickstart
go get github.com/frostyard/updex/v2/updex
```

Save as `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/frostyard/updex/v2/updex"
)

func main() {
    client := updex.NewClient(updex.ClientConfig{Verify: true})
    ctx := context.Background()

    // List all features (union of the legacy default directory and every
    // discovered systemd-sysupdate component). Use FeaturesOptions{Component: "docker"}
    // to scope to one component instead.
    features, err := client.Features(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, feature := range features {
        fmt.Printf("%s: enabled=%v (%s)\n", feature.Name, feature.Enabled, feature.Description)
    }

    components, err := client.Components(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, component := range components {
        fmt.Printf("%s: %s (%d features)\n", component.Name, component.SourceDir, component.FeatureCount)
    }

    daemon, err := client.DaemonStatus(ctx, updex.DaemonStatusOptions{})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("daemon installed=%v active=%v\n", daemon.Installed, daemon.Active)

    enabled, err := client.EnableFeature(ctx, "docker", updex.EnableFeatureOptions{Now: true})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(enabled.NextActionMessage)

    // A failed check still returns partial results with per-component errors.
    checks, err := client.CheckFeatures(ctx, updex.CheckFeaturesOptions{})
    if err != nil {
        log.Printf("check incomplete: %v", err)
    }
    for _, feature := range checks {
        for _, check := range feature.Results {
            if check.Error != "" {
                fmt.Printf("%s: could not check: %s\n", check.Component, check.Error)
                continue
            }
            if check.UpdateAvailable {
                fmt.Printf("%s: %s → %s\n", check.Component, check.CurrentVersion, check.NewestVersion)
            }
        }
    }

    updates, err := client.UpdateFeatures(ctx, updex.UpdateFeaturesOptions{})
    if err != nil {
        log.Fatal(err)
    }
    for _, feature := range updates {
        for _, update := range feature.Results {
            fmt.Printf("%s: version %s (downloaded=%v)\n", update.Component, update.Version, update.Downloaded)
        }
    }

    _, err = client.DisableFeature(ctx, "docker", updex.DisableFeatureOptions{Now: true, Force: true})
    if err != nil {
        log.Fatal(err)
    }
}
```

Run `go run .` from the module directory. Enable, update and disable change
system state: run the full example only on a configured test system with
permission to manage extensions.

## Client

```go
type Client struct { /* unexported fields */ }

type ClientConfig struct {
    Definitions        string                // Custom directory of .transfer/.feature files (overrides search paths)
    Verify             bool                  // Enable GPG signature verification
    Verbose            bool                  // Enable debug output
    Progress           reporter.Reporter     // Progress reporter (optional)
    SysextRunner       sysext.SysextRunner   // Mock runner for tests (optional)
    BootcRunner        imagecompat.BootcRunner // Runs `bootc status --json` for the minimum image version check; nil = the real bootc (optional; tests inject a fake)
    ErofsDumpRunner    imagecompat.ErofsDumpRunner // Reads a compressed EROFS image with `dump.erofs`; nil = the real dump.erofs in PATH (optional; tests inject a fake)
    SystemdManager     *systemd.Manager       // Unit manager and runner for daemon operations (optional)
    OnDownloadProgress download.ProgressFunc // Download progress callback (optional)
    HTTPClient         *http.Client          // Shared HTTP client (optional)
    Paths              RuntimePaths          // Instance-scoped filesystem paths (optional)
}

// RuntimePaths holds the filesystem paths an updex.Client consults at runtime.
// Zero values resolve to current production defaults at NewClient time.
type RuntimePaths struct {
    DefinitionRoots    []string // Roots for sysupdate.d directories; default: config.SearchRoots
    OSReleasePaths     []string // os-release files for specifier expansion and image naming; default: config.OSReleasePaths
    CatalogConfigRoots []string // Dirs for *.catalog files; default: catalog.ConfigRoots
    CatalogCacheDir    string   // Cache dir for catalog listings; "" = default catalog.CacheDir; DisableCatalogCache = off
    CatalogTargetPath  string   // Staging dir for catalog transfers; default: catalog.TargetPath
    SysextLinkDir      string   // Dir for systemd-sysext image links; default: sysext.SysextDir
    RunExtensionsDir   string   // systemd-sysext runtime image search dir (merged on next refresh); default: sysext.RunExtensionsDir
    SysextHierarchies  []string // Hierarchies whose .systemd-sysext metadata records merged images; default: sysext.MergedHierarchies
    LockPath           string   // Lock file every mutating operation holds (flock); default: updex.DefaultLockPath (/run/updex/lock)
    RollbackDir        string   // Directory of per-feature rollback records; default: updex.DefaultRollbackDir (/var/lib/updex/rollback)
}

// DisableCatalogCache is a RuntimePaths.CatalogCacheDir sentinel that
// explicitly disables catalog listing caching for one client.
const DisableCatalogCache = "\x00"

func NewClient(cfg ClientConfig) *Client
```

`NewClient` resolves `cfg.Paths` once at construction: each zero field reads its corresponding package-level compatibility variable or production constant exactly once and takes a defensive copy of slices. After construction the client never consults those package variables again, so mutating `config.SearchRoots`, `catalog.ConfigRoots`, `catalog.CacheDir`, or `sysext.SysextDir` cannot redirect the client. This is the ADR-0011 invariant as extended by ADR-0015: all runtime dependencies, including merged sysext state, are captured immutably at construction.
Use `Paths` to give two clients different filesystem trees safely in one process.

Path-dependent supporting-package APIs have explicit variants for SDK use:
`config.Load*In` receives definition roots and, for transfers, os-release
paths; `catalog.LoadReposFrom`, `CachedListIn`, and `RenderTransferTo` receive
their paths directly; and sysext `*At` operations receive the captured sysext
directory. `sysext.GetActiveVersionWith` receives the captured default directory, runtime image search directory, and merged-state hierarchies (`ActiveStatePaths`); `sysext.GetActiveVersionIn` remains for the two-signal case without hierarchy metadata. The original package functions remain compatibility
wrappers over their package variables or production constants.

An injected `SysextRunner` is the one place that invariant depends on the
caller. The client links through `sysext.PathSysextRunner`
(`SysextRunner` plus `LinkToSysextAt(*config.Transfer, string) error`),
handing it the captured `SysextLinkDir`. A runner that implements only the
original four-method `SysextRunner` cannot be told a directory, so the client
does **not** fall back to `sysext.SysextDir` for it: every operation that may
link refuses such a runner with `updex.ErrLegacySysextRunner` (testable with
`errors.Is`) **before** it mutates anything —
`CatalogAdd` before it writes a definition, `EnableFeature` with `Now` before
it writes the drop-in, and the feature install pipeline (`installTransfers`)
before it removes a legacy symlink or downloads. Dry runs never link and stay available to any runner.
The `SysextRunner` interface itself is unchanged, so existing implementations
still compile; adding `LinkToSysextAt` is what makes one usable for a real
install. `sysext.DefaultRunner` and `sysext.MockRunner` both implement it.

`sysext.DefaultRunner.Refresh` runs the sequence of
[ADR-0019](../adr/0019-apply-merged-sysusers-and-tmpfiles-on-a-live-refresh.md),
chosen by the major version in `systemd-sysext --version`. Systemd 260 and
newer, or a version updex cannot read, get `systemd-sysext refresh
--no-reload`, `systemd-sysusers`, `systemd-tmpfiles --create --graceful`, then
`systemd-sysext refresh --always-refresh=yes`: the forced remount is what
makes a reload happen when the image set did not change since the first
command. Systemd 255 to 259 get the same with a plain last refresh. Older
than 255 gets one plain refresh, then sysusers and tmpfiles; it has no reload
in a refresh, so it gets the accounts after the merge. `systemd-tmpfiles`
gets `--graceful` from systemd 254, and `--create` alone before that, because
an unknown option would create no paths. A first-refresh
failure returns at once as `systemd-sysext refresh failed: …` and runs
nothing else. A sysusers failure is a refresh failure
(`systemd-sysusers failed after refresh: …`, joined with a last-refresh
failure) and the sequence still finishes; when the last refresh succeeded,
the error also wraps `sysext.ErrActivationIncomplete`. A tmpfiles failure is
one warning line on stderr, never an error.

Other fields: if `SysextRunner` is nil it defaults to `&sysext.DefaultRunner{}`; if `SystemdManager` is nil it defaults to `systemd.NewManager()` for `/etc/systemd/system` (packaged units: `/usr/lib/systemd/system`) and the real `systemctl`; if `Progress` is nil it defaults to `reporter.NoopReporter{}`; if `HTTPClient` is nil a default `http.Client` with a 10-minute timeout, the standard 10-redirect limit, and an HTTPS-to-HTTP downgrade refusal is created via `internal/httpclient.New` — the same constructor `manifest.Fetch` and `download.Download` fall back to when their own `httpClient` parameter is nil, so the refusal is enforced consistently everywhere a caller supplies no client. HTTP-to-HTTP and HTTPS-to-HTTPS redirects remain allowed. A caller-supplied `HTTPClient` keeps its redirect policy, but updex does not use the client itself: it stores a shallow copy whose `Transport` wraps the given transport (`http.DefaultTransport` when the client has none) and keeps `Timeout`, `CheckRedirect` and `Jar`. The wrapper records, for one `UpdateFeatures` run, whether a request got an HTTP response, which the unreachable-source rule needs (see [Unreachable sources and `ErrSourcesUnreachable`](#unreachable-sources-and-errsourcesunreachable)); the caller's client is not changed. `OnDownloadProgress` is called with the HTTP response content length (-1 if unknown) and must return a fresh `io.Writer` per attempt to avoid double-counting retried downloads.

### Operation lock and `ErrBusy`

```go
const DefaultLockPath = "/run/updex/lock"
var ErrBusy = errors.New("another updex operation is running")
```

Every mutating operation — `UpdateFeatures`, `EnableFeature`, `DisableFeature`,
`CatalogAdd`, `CatalogRemove`, `EnableDaemon` and `DisableDaemon` — holds an
exclusive, non-blocking `flock` on `RuntimePaths.LockPath` for its whole run
([ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md)). A
zero `LockPath` resolves once, in `NewClient`, to `DefaultLockPath`. Dry runs
(`DryRun: true`) and the read-only operations (`Features`, `CheckFeatures`,
`CatalogList`, `DaemonStatus`, `Components`) never take it. `CatalogAdd` and
`CatalogRemove` take it once and run the enable/disable steps without taking
it again.

When another open file description holds the lock, the operation returns at
once, changes nothing, and returns an error that wraps `ErrBusy`
(`errors.Is(err, updex.ErrBusy)`); updex never waits. `UpdateFeatures` and
`CatalogAdd`/`CatalogRemove`/the daemon methods return a nil result;
`EnableFeature` and `DisableFeature` return a `FeatureActionResult` whose
`Error` is set. The lock file is created with mode 0600 and its parent
directory with mode 0755 when missing. The path is checked with `os.Lstat`
and opened with `O_NOFOLLOW`: a symlink, a directory or any other non-regular
file is refused with an error that is not `ErrBusy`. The kernel releases the
lock when the process exits. The CLI maps `ErrBusy` to exit status 0 with one
stderr line only for `updex features update` under systemd (`INVOCATION_ID`
set); everywhere else it is an error.

### Unreachable sources and `ErrSourcesUnreachable`

```go
var ErrSourcesUnreachable = errors.New("no update source is reachable")
```

`UpdateFeatures` wraps `ErrSourcesUnreachable`
(`errors.Is(err, updex.ErrSourcesUnreachable)`) only when the whole run failed
because no source could be reached
([ADR-0018](../adr/0018-skip-an-update-run-when-every-source-is-unreachable.md)):
there is at least one transfer error; every transfer error except the
collateral `not linked: another transfer of feature <name> failed` is a
network failure; no HTTP request of the run got a response of any status;
the run's own context is not cancelled; and no refresh failed. A network
failure is a `*net.DNSError`,
`syscall.ECONNREFUSED`/`EHOSTUNREACH`/`ENETUNREACH`/`ENETDOWN`/`ETIMEDOUT`
anywhere in the chain, or a `net.Error` whose `Timeout()` is true. A dial
error counts only through one of these causes: a dial that fails for a local
reason (`EMFILE`, `EACCES`, `ENOBUFS`) is not unreachable. TLS and x509 errors, HTTP error
statuses, verification failures, hash mismatches, disk errors and
"no versions available" are not unreachable. When the rule holds, the first
joined error is `one or more components failed to update: no update source is
reachable`, the transfer errors stay in the chain after it, and the results
slice is unchanged. Every other failure keeps the shape above.

The rule reads a per-operation tracker that `UpdateFeatures` puts in its
context: the transport wrapper of the client's HTTP copy records each attempt
and each response. A request outside `UpdateFeatures` carries no tracker. The wrapper forwards
`CloseIdleConnections`, and `CancelRequest` to a base transport that is not an
`*http.Transport`, so a client timeout still reaches a legacy transport. The
CLI maps the sentinel to exit status 0 only for `updex features update` under
systemd: after the normal output and the run's usual progress and warning
lines, it adds the one skip line; an interactive run returns the error, and
`--json` output is unchanged.

## Methods

### Daemon lifecycle

```go
func (c *Client) EnableDaemon(ctx context.Context, opts EnableDaemonOptions) (*DaemonActionResult, error)
func (c *Client) DisableDaemon(ctx context.Context, opts DisableDaemonOptions) (*DaemonActionResult, error)
func (c *Client) DaemonStatus(ctx context.Context, opts DaemonStatusOptions) (*DaemonStatusResult, error)
```

`DaemonUnits() (*systemd.TimerConfig, *systemd.ServiceConfig)` returns the
configuration of the auto-update timer and service: the one source of the unit
text for `EnableDaemon` and for the release build, which writes the packaged
files `/usr/lib/systemd/system/updex-update.{timer,service}` with
`systemd.GenerateTimer`/`GenerateService` (`updex units`, `scripts/units.sh`).
The service is the sandboxed `oneshot` that runs
`/usr/bin/updex features update --no-refresh` with `Wants=`/`After=`
`network-online.target`, `ConditionACPower=true` and `TimeoutStartSec=2h`
(`systemd.ServiceConfig.TimeoutStartSec`; empty writes no line); the timer is
`OnCalendar=*-*-* 00/6:00:00`, `RandomizedDelaySec=3600s`, `Persistent=true`,
`WantedBy=timers.target`. The sandbox's `SystemCallFilter=@system-service @mount` allows `@mount` only so that `bootc status --json` can remount `/sysroot` inside the service's own mount namespace (frostyard/updex#463). Units that an older `updex daemon enable` wrote to `/etc/systemd/system` keep the old filter until `updex daemon disable` and `updex daemon enable` rewrite them. Every call returns new values.

`EnableDaemon`: when the manager's `PackagedUnitPath` (production
`/usr/lib/systemd/system`) has `updex-update.timer`, it enables and starts the
timer (`systemctl enable`, then `start`, the same as `enable --now`) and writes
no file; `Packaged` is set and `UnitFiles` are the packaged files; unit files in
the `/etc` unit directory are reported in the message as overriding them, and
enabling again is not an error. Otherwise it installs `DaemonUnits()` into the
unit directory (`/etc/systemd/system`) without overwriting occupied paths, each
file starting with the marker line
`# Generated by updex daemon enable; updex daemon disable removes this file.`,
reloads systemd, then enables and starts the timer, and returns the
"already installed" error when `/etc` unit files exist.
`DisableDaemon` stops and disables the timer, removes the unit files in the
`/etc` unit directory that updex wrote, and reloads systemd. A file is updex's
when its first line is the marker, or its whole content equals one of the
frozen unit texts that earlier releases wrote without a marker; any other file
is kept and listed in `KeptFiles` and in the message, and a directory or other
non-regular path is refused (`os.Lstat`, ADR-0005). A symlink to `/dev/null` is
the one exception: it is a systemd mask (`systemctl mask`), which updex never
removes. Disable classifies it as masked, leaves it in place, and reports it in
`MaskedFiles` and in the message with the command that undoes it, for example
`; updex-update.timer is masked (run 'systemctl unmask updex-update.timer' to
undo)`; with both units masked, one command names both (`systemctl unmask updex-update.timer updex-update.service`). Every other symlink is still refused. A masked timer still counts as
installed, so disable stops and disables it without the "timer not installed"
error. Packaged units are never touched. It returns "timer not installed"
when neither directory has the timer. Every step is attempted, and stop,
disable, removal and reload failures are contextualized and joined;
`DisableDaemon` returns that error with no success result.
`DaemonStatus` reports a missing timer without querying systemctl. For an
installed timer (a timer file in either directory) it reports enabled/active
state, `Source` (`etc` when the timer file in use is in the `/etc` unit
directory, else `packaged`), `Masked` (`true` when that timer file is a symlink
to `/dev/null`; such a timer never fires, so `Schedule` is empty),
`UnitFiles` in use, `Schedule` (the timer's
effective `OnCalendar` schedule, comma-separated), and the next and last run
from `systemctl show -p
PROPERTY --value`, one call per property: `NextRun` (timer
`NextElapseUSecRealtime`), `LastRun` (service `ExecMainExitTimestamp`) and
`LastResult` (service `Result`, read only when `LastRun` is set), as systemd
prints them. `Schedule` comes from the timer's `TimersCalendar` property, so it
includes administrator drop-ins (`/etc/systemd/system/updex-update.timer.d/`,
`/run/systemd/system/updex-update.timer.d/`, and the same under
`/usr/lib/systemd/system`) and honours an empty `OnCalendar=` that resets the
list. When `Show` is unsupported, fails, or reports no calendar trigger,
`Schedule` is computed from the timer file in use and those drop-ins by
systemd's rules, with drop-in names in higher-priority directories hiding the
same name below and the remaining files applied sorted by name; files are read
with `os.Lstat` and a bounded read, and non-regular files are skipped. A
schedule that cannot be read leaves `Schedule` empty; it never fails the
status. An empty value, `n/a` and `0` give an empty string: `NextRun` is
empty when nothing is scheduled; `LastRun` and `LastResult` are empty when the
service never ran since boot. A runner that does not implement
`systemd.ShowRunner` leaves them empty without an error. If a query fails,
`DaemonStatus` returns that failure with context rather than reporting a
successful false state; the CLI therefore fails instead of rendering an
inaccurate status. All three methods
reject an already canceled context before filesystem or systemctl work, and
the `ctx` they receive is threaded all the way through every `systemd.Manager`
operation they call (`Install`, `Stop`, `Disable`, `Enable`, `Start`,
`IsEnabled`, `IsActive`, `Show` and the `daemon-reload`): if `ctx` is
canceled or expires while one of those systemctl subprocesses is running,
`DefaultSystemctlRunner` kills it immediately (`exec.CommandContext`) and the
call returns promptly with an error matching `context.Canceled` or
`context.DeadlineExceeded` (`errors.Is`) instead of the raw process-kill
error. A `SystemctlRunner` that also implements `ContextSystemctlRunner`
(`DaemonReloadContext`, `EnableContext`, `DisableContext`, `StartContext`,
`StopContext`, `IsActiveContext`, `IsEnabledContext`) gets this in-flight
cancellation; `Manager` falls back to the legacy, non-cancelable
`SystemctlRunner` methods for a runner that only implements those, so
existing external `SystemctlRunner` implementations remain source-compatible.

`EnableDaemonOptions.DryRun` and `DisableDaemonOptions.DryRun` preview the
operation: the context check and the installed/not-installed precondition
still run, so a dry run reports the same error as a real run (a disable dry
run also reports the refusal of a non-regular unit path), but no unit file is
written or removed and no systemctl call is made. A dry-run result has
`DryRun` set, `UnitFiles` populated, and a `Message` that describes the actions
a real run would take; the CLI prints it with a `[DRY RUN] ` prefix. A real run
also fills `UnitFiles`, so JSON consumers see the same paths either way (for
disable, the files that are or would be removed; for an enable with packaged
units, the packaged files in use).
Actions return:

```go
type DaemonActionResult struct {
    Success   bool     `json:"success"`
    Message   string   `json:"message"`
    DryRun    bool     `json:"dry_run,omitempty"`
    UnitFiles []string `json:"unit_files,omitzero"`
    Packaged  bool     `json:"packaged,omitempty"`   // the package ships the units
    KeptFiles []string `json:"kept_files,omitzero"`  // /etc files disable kept
    MaskedFiles []string `json:"masked_files,omitzero"` // /etc files disable kept: systemd masks (symlinks to /dev/null)
}

type DaemonStatusResult struct {
    Installed  bool     `json:"installed"`
    Enabled    bool     `json:"enabled"`
    Active     bool     `json:"active"`
    Schedule   string   `json:"schedule,omitempty"` // effective OnCalendar, "a, b"; empty for a mask
    Source     string   `json:"source,omitempty"`      // "packaged" or "etc"
    UnitFiles  []string `json:"unit_files,omitzero"`
    NextRun    string   `json:"next_run,omitempty"`
    LastRun    string   `json:"last_run,omitempty"`
    LastResult string   `json:"last_result,omitempty"`
    Masked     bool     `json:"masked,omitempty"` // the timer file in use is a symlink to /dev/null
}
```

The CLI keeps root authorization at its boundary, then forwards the Cobra
context and formats these result types. SDK callers inject
`ClientConfig.SystemdManager` when they need a non-production unit path or a
mock `SystemctlRunner`.

### Features

```go
func (c *Client) Features(ctx context.Context, opts ...FeaturesOptions) ([]FeatureInfo, error)
```

Lists all configured features with their enabled/masked status and associated transfers. `opts` is variadic for backward compatibility with the pre-component-scoping signature (`Features(ctx)`): only `opts[0]` is read when present, so callers may either omit `opts` entirely or pass a single `FeaturesOptions{}`.

Each `FeatureInfo` also carries `TransferStatus`, one `TransferStatus` per transfer in `Transfers` order (see [TransferStatus](#transferstatus)): the version merged now (`RunningVersion`) and the version the `/var/lib/extensions` link names (`LinkedVersion`). `Features` reads both for every feature, enabled or not. A transfer whose state cannot be read keeps the value that could be read, carries the failure in `TransferStatus.Error`, and produces a reporter warning; the listing never fails for it.

`Transfers` and `TransferStatus` list every transfer **associated** with the feature (`config.GetTransfersForFeature`: the feature is in `Features` or in `RequisiteFeatures`). The rollback hold shown in `TransferStatus.RollbackRejected` and `RollbackHeldBy` is computed, as `UpdateFeatures` computes it, over the transfers that are **active** with the feature enabled (see [Activation rule](#activation-rule)): an associated transfer that is not active is shown with its versions only, without a rollback hold, and brings no record in. The hold is computed for a disabled feature as if it were enabled.

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

### Activation rule

([ADR-0017](../adr/0017-apply-the-feature-activation-rule-in-every-feature-path.md); `updex/activation.go`, `config.CheckTransferActivation`.) A feature command acts on a transfer only when it is **active** (systemd `sysupdate.d(5)`): `Features=` is empty or at least one listed feature is enabled (OR), and every feature in `RequisiteFeatures=` is enabled (AND). A feature is enabled when it is in the loaded domain, enabled and not masked; a name that is not loaded is never enabled (a `Component` scope or a `Definitions` override can therefore make a requisite feature outside it count as missing). `splitFeatureTransfers(features, transfers, name, forceEnabled)` takes the transfers associated with `name`, computes the enabled set of the loaded domain with `name` forced to `forceEnabled`, and returns the active transfers and the inactive ones with the reason (`InactiveTransfer`). The state forced for `name` is **enabled** for `UpdateFeatures`, `CheckFeatures`, `EnableFeature{Now}`, `RollbackFeature` and the hold computation in `Features`, and **disabled** for `DisableFeature{Now}`.

An inactive transfer is never downloaded, linked, vacuumed or rolled back, is not part of the refresh gate's own links (`pendingActivationLinks` lists it like any other sysext on the host), and its rollback records do not apply to the feature. A transfer with neither list belongs to no feature and is not touched by any feature command.

### EnableFeature / DisableFeature

```go
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error)
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error)
```

Enable creates a drop-in file setting `Enabled=true`. With `Now: true`, it downloads extensions via the same `installTransfers` pipeline as `UpdateFeatures`, so the feature's transfers link together (all or none; the first own failure is returned as `failed to download <component>: …`); a component whose newest image is already staged and current is not re-downloaded, but its sysext link is restored if it is missing, dangling, or points at another image (`Linked staged version <version> for <component>` — reported through the progress reporter, not the result). Disable creates a drop-in setting `Enabled=false`. Without `Now`, that is all it does: the staged images and the `/var/lib/extensions` links stay and the extensions stay merged (`features update` and the daemon skip disabled features, so they do not remove anything). The result then has no `RemovedFiles` and `NextActionMessage` is `Feature '<name>' disabled. Its extensions stay installed and merged; run 'updex features disable <name> --now' to remove them.`

In dry-run mode, enable/disable skip writing drop-ins and skip sysext/filesystem mutations. `EnableFeature` with `Now: true` records the active transfer components as would-download entries without fetching manifests or resolving exact versions. `DisableFeature` with `Now: true` still checks active versions for force-safety, then records component-level would-remove entries instead of deleting files.

**`EnableFeature{Now}` and the activation rule.** Only the transfers that are active with the feature enabled (the loaded domain's enabled set plus `name`) are downloaded and linked, through the same `installTransfers` call. Every other associated transfer is reported in `FeatureActionResult.Inactive` (also in a dry run), with no download and no link, and `NextActionMessage` ends `; not downloaded, waiting for another feature: <component> waits for feature(s) <names>` (or `needs one of features: <names>`). So `enable maild --now` leaves a transfer `Features=maild` `RequisiteFeatures=lodge` waiting while `lodge` is disabled, and a later `enable lodge --now` installs it. When no associated transfer is active, nothing is installed and no refresh runs. `pendingActivationLinks` and `refreshGateFor` get the active transfers as the operation's own links.

**`DisableFeature{Now}` and the activation rule.** The feature is forced disabled, and each associated transfer is checked. A transfer that is **still active** without the feature, because another enabled feature in its `Features=` activates it, is **kept**: no unlink, no `RemoveAllVersionsAt`, and it is not in the merged-state `--force` check. It is reported in `FeatureActionResult.Kept` (`KeptTransfer{Component, ActiveThrough}`: the other enabled features of its `Features=`) and in `NextActionMessage`. Only the transfers that become inactive are removed (including one that waited already: its leftover images go). When nothing is left to remove, there is no unmerge, no refresh and no refresh-gate preflight; only the drop-in is written, and `NextActionMessage` says `No extension files were removed.` plus the kept transfers. A transfer that has `name` only in `RequisiteFeatures=` is never kept: without `name` it misses a requisite. `CatalogRemove` calls the lock-free `disableFeature`; a catalog transfer is rendered with `Features=<name>` as its only `Features=` entry (`catalog.RenderTransferTo`), so it is never kept.

**Refresh failures are reported, not swallowed.** When `Now: true` (and not `NoRefresh`/`DryRun`), both methods finish with `systemd-sysext refresh`. That call is `sysext.DefaultRunner.Refresh`, the sequence of [ADR-0019](../adr/0019-apply-merged-sysusers-and-tmpfiles-on-a-live-refresh.md): a `systemd-sysusers` failure counts as a refresh failure, and a `systemd-tmpfiles --create` failure is only a warning on stderr. If that refresh fails after every filesystem change succeeded, the result still records what was done (`DropIn`, `DownloadedFiles`, or `Unmerged`/`RemovedFiles`), but `Success` is `false`, `RefreshError` and `Error` both carry `sysext refresh failed: …`, `NextActionMessage` says how to finish activation, and the method returns that error. The message follows the error: when it wraps `sysext.ErrActivationIncomplete` (the images merged, only `systemd-sysusers` failed), enable says `… downloaded and merged, but activation did not finish (the sysext accounts may be missing); reboot to finish it`, disable says `… removed; the remaining extensions are merged, but activation did not finish (the sysext accounts may be missing); reboot to finish it`, and rollback says `… the extensions are merged, but activation did not finish (the sysext accounts may be missing); reboot to finish it`, because a plain `systemd-sysext refresh` does not run `systemd-sysusers`. For the same reason, every other enable or rollback refresh failure says the merge state is not known and advises a reboot (#479); disable keeps the `systemd-sysext refresh` (or reboot) advice, because the remaining extensions were active before and their accounts exist. For `DisableFeature` this matters most: `Unmerge()` already detached every extension on the host, and a failed refresh does not prove what is merged now (a first refresh without a reload may have merged the rest before the last one failed), so the remaining extensions may stay unmerged — only a refresh error that is not `ErrActivationIncomplete` says so. The CLI prints the completed steps, then `Error: …` and the next action, and exits non-zero.

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

Downloads and installs the newest available version for each enabled feature's **active** transfers (see [Activation rule](#activation-rule)). A transfer that is not active, for example `Features=maild` `RequisiteFeatures=lodge` while `lodge` is disabled, or one whose `Features=` has no enabled feature (a run where only `lodge` is enabled), is neither downloaded nor linked nor vacuumed: it is listed in `UpdateFeaturesResult.Inactive` and the reporter gets one `<feature>/<component>: inactive: <reason>` line for it. A feature whose associated transfers are all inactive still gets an `UpdateFeaturesResult`, with empty `Results` and `Inactive` set. The enabled features' active transfers are the run's own transfers for the refresh gate. Delegates each feature to the internal `installTransfers` pipeline: `stageTransfer` per transfer (version lookup, legacy staging-symlink cleanup, download, verification), then `linkStaged` for the whole feature, then the vacuum.

**A feature's transfers link together** ([ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md)). Every transfer of a feature is staged first. If one fails (version lookup, download, verification), no transfer of the feature is linked: every old link stays, the images already downloaded stay on disk for the next run (which relinks them without downloading again), and the vacuum does not run. The failing transfer's `UpdateResult.Error` is its own error; every other transfer of the feature gets `not linked: another transfer of feature <name> failed`, with `Version` and `Downloaded` showing what is staged and `Installed=false`. If a link fails part-way through the link step, the links already moved are restored (an absent link is removed again; a regular file that the link replaced was backed up as a hard link beside it in the snapshot pass and is renamed back over the link), the failing transfer reports the link error and the others report `not linked: …`. A regular file at a link path is backed up before any link moves, so a later failure never loses it; a directory, FIFO or device there, and a regular file updex cannot back up, is refused in that same pass with nothing changed. A restore failure is an error, never only a warning: `linkStaged` returns it joined with the link error, a transfer whose link had moved reports `failed to restore sysext link for <component>: …; the link may point at the new version` instead of `not linked`, and the backup it could not put back stays on disk. Backups are removed after the feature links. Other features continue; the method returns `one or more components failed to update` joined with every failed transfer's error. When no source could be reached at all, that first error wraps `ErrSourcesUnreachable` instead (see [Unreachable sources and `ErrSourcesUnreachable`](#unreachable-sources-and-errsourcesunreachable)).

**Retention.** After a feature's links succeeded, `sysext.VacuumWith` removes old staged images but keeps, per transfer: the linked version, the merged version (hierarchy metadata, ADR-0015), `ProtectVersion=`, the legacy `CurrentSymlink` target and the newest `InstancesMax` (default 2). A host that linked a new revision and still runs an older one keeps up to 3. A vacuum that cannot read the link or the merged state deletes nothing and reports a warning (`vacuum failed for <component>: …`) while the update still succeeds.

**Lock.** Except with `DryRun`, `UpdateFeatures` holds the operation lock (see [Operation lock and `ErrBusy`](#operation-lock-and-errbusy)) and returns an error wrapping `ErrBusy` when another operation holds it. Manifests are cached by source URL — transfers sharing the same source avoid redundant HTTP requests. Parsed source patterns are returned from version listing and reused by the install pipeline to avoid redundant pattern compilation. Refresh is batched — a single `systemd-sysext refresh` runs after all components are processed. If that final refresh fails, the per-feature results are still returned as recorded (a component that was downloaded and linked keeps `Installed=true`; it is staged but not activated) and the method returns `sysext refresh failed: …` — joined with `one or more components failed to update` when a component also failed — so callers and the CLI (non-zero exit, `--json` still emits the array) never mistake an unactivated update for a completed one. With `NoRefresh: true` (the daemon path) no refresh is attempted. With `DryRun: true`, manifests are fetched and versions are selected, but download, legacy cleanup, sysext linking, refresh, and vacuum deletion are skipped. Returns per-feature results with per-component status.

The manifest cache key is `Transfer.Source.Path` only, but verification is a property of the transfer, not of load order: `manifest.Manifest.Verified` records whether a cached entry passed GPG verification, and `getAvailableVersions` (shared with `CheckFeatures`) never lets a transfer that requires verification (per `Client.verifyPolicy`: `ClientConfig.Verify`, `Verify=true`, or a catalog-generated transfer whose repo lacks `AllowUnsigned=yes`) consume an unverified cached manifest — it refetches with verification and the verified manifest replaces the cache entry. A verified manifest may serve unverified transfers, never the reverse. Two transfers sharing a source with the same verification requirement still make one HTTP request. Changes that require different auth behavior per transfer must still change the cache key or bypass caching.

Dry-run update results use the normal `UpdateResult` shape: `Downloaded=true` means the component would be downloaded, `Installed=false` means no install happened, and `RemovedVersions` is populated from `sysext.PlanVacuumWith` (the would-install version is the active version; the merged version is protected; the link is not, because it moves) unless `NoVacuum` is true. The CLI still enforces root before calling this SDK method, but the SDK method itself is read-only in dry-run mode apart from remote manifest fetches.

Already-current components are detected by `sysext.GetInstalledVersions`: the selected newest version must be both present on disk and equal to the current version resolved from a legacy `CurrentSymlink` (or newest installed when no symlink exists). This detection deliberately reads the staging directory, not the `/var/lib/extensions` link, so a staged image is never downloaded again. After current detection but before any no-op return, update removes the legacy staging symlink if the transfer defines one. A newer installed-but-not-current version is still treated as needing installation so the `/var/lib/extensions` link can be updated. An already-current component restores a missing sysext link without re-downloading: if `<SysextLinkDir>/<component>.<ext>` is absent, dangling, not a symlink, or resolves to another image, the link step relinks it (through the runner) and still returns `Downloaded=false`, with `Linked=true`; a link that already resolves to the current image is not touched and reports `Linked=false`. A failure to list installed versions on that path is a component error (`failed to inspect installed versions: …`), not a fall-through into download.

When the link step relinked because the image was already staged and current, it reports that fact separately from `Downloaded`: the install outcome's unexported `Relinked` field feeds the exported `UpdateResult.Linked`. The non-dry-run component result then keeps `Downloaded=false`, sets `Installed=true` and `Linked=true`, sets `NextActionMessage` to `Reboot required to activate changes` (the same text as the download case), and the progress reporter prints `Linked staged version <version> for <component>` instead of `Version <version> already installed and current`. This is how an operator learns that a second updater (for example `systemd-sysupdate`) advanced its own staging symlink while the systemd-sysext link updex owns still pointed at the older image.

**Minimum image version** ([ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md), plan 0001 Phase 5; `updex/imagegate.go`, package `imagecompat`). `installTransfers` checks every image whose link would move, after the stage step and before the link step. That covers a new download and the relink path (an image blocked earlier stays staged). It reads the staged image the link step would link (`sysext.LinkTargetAt`) with `imagecompat.ReadExtensionRelease` (name hint: the transfer's component, then the feature name when several extension-release files exist).
- No key: link as usual. `imagecompat.ErrUnsupportedImage` (no EROFS filesystem at all: a squashfs sysext, random bytes, an invalid GPT): the same, **fail open**, because only snosi sets the key and snosi ships EROFS; a debug line says so. **Once a candidate has the EROFS superblock magic (`0xE0F5E1E2` at offset 1024), every later problem fails closed** and never wraps `ErrUnsupportedImage`: `ErrUnreadableRelease` (a failed EROFS open, a failed open/stat/read of the release file, a recovered parser panic, or a failed `dump.erofs` run for a compressed image), `ErrErofsToolUnavailable` (the compressed-image fallback cannot run because `dump.erofs` is missing or too old), `ErrNoExtensionRelease`, `ErrAmbiguousExtensionRelease`, `ErrReleaseFileTooLarge`.
- Key set: the next-boot `IMAGE_VERSION` is resolved once per operation (`nextBootImage`; so a run with no key never runs bootc) with `imagecompat.NextBootImageVersion(ClientConfig.BootcRunner, Paths.OSReleasePaths)`, then `imagecompat.CheckMinImageVersion`.
- **Too old / missing / not numeric: not an error.** No transfer of the feature links, every old link stays, the images stay staged, no vacuum runs. The blocking transfer has `UpdateResult.NeedsImage = "needs image >= <value>"` and `NextActionMessage` `not linked: needs image >= <value> (next-boot image <version>)`; the feature's other transfers have `NotLinked=true` and a message naming the blocking one. `Installed` is false, `Downloaded` shows what was fetched. Other features continue; `UpdateFeatures` returns no error for this.
- **A read error on an image with an EROFS filesystem (an unreadable release file, no extension-release file, several without a match, over 64 KiB, or a compressed image whose `dump.erofs` fallback is missing, older than erofs-utils 1.8.5, or fails), a `sysext.LinkTargetAt` failure, or a bootc that is installed but fails: an error, fail closed.** Nothing of that feature links, the result has `Error`, and the run fails like any feature error. The gate never depends on the link step failing later. Images without the key are not affected by a failing bootc. bootc not installed is not an error: the os-release is used.
- `DryRun` downloads nothing, so it checks only an image that is already staged and would be relinked (`NextActionMessage` starts `would not link:`); for an image it would download it says nothing.
- `EnableFeature` with `Now` uses the same path; a blocked image leaves the feature enabled and reports it in `FeatureActionResult.NotLinked` and the `NextActionMessage` suffix `; not linked, the old links stay: …`. `RollbackFeature` does not check: it links an older staged image, which needs at most the base the host already runs (the ADR applies the rule to updates).

**Refresh gate: the running image** (`refreshGate` in `updex/imagegate.go`). The link gate answers "may the next boot run this image". A `systemd-sysext refresh` is a different question: it merges into the base the host runs **now**, and `imagecompat.BootedImageVersion(Paths.OSReleasePaths)` reads that base's `IMAGE_VERSION` from the first readable os-release file (on a bootc host the booted deployment's; no `bootc` call, and it is cached once per operation in `runningImage`, like the next-boot value in `nextBootImage`). A newer staged deployment can therefore allow a link that the running base cannot run.

Before every refresh updex runs — `UpdateFeatures` without `NoRefresh`, `EnableFeature` with `Now` (or its dry run), `RollbackFeature` with `Now` (or its dry run), and `DisableFeature` with `Now` — it checks every sysext the refresh would merge: the links of the operation's own transfers (`ownPendingLinks`) and the other pending activations of the host (`pendingActivationLinks`, deactivations excepted). A candidate is every transfer with a usable link (`LinkedVersion != ""`) whose merged version is not known to equal it: `RunningVersion != LinkedVersion`, including `sysext.UnknownVersion` and `""` (updex cannot prove the running base already merges that version, so it checks). `TransferStatus.PendingReboot` and `PendingActivations` stay the narrower display: an unknown merged version is not reported as pending and not listed. A transfer whose merged state or link cannot be read (`TransferStatus.Error`) is unsafe without reading its image, with the reason `cannot read the state of <component>`. Each linked image (`sysext.StagedImageAt`, then `imagecompat.ReadExtensionRelease` with the transfer's component as the only name hint) follows the link gate's rules, but the key is compared with the running version:

- key absent, or `imagecompat.ErrUnsupportedImage`: safe (fail open, as in the link gate);
- any other read error: unsafe (fail closed);
- key set: unsafe when `imagecompat.CheckMinImageVersion` against the running version is not ok, which includes a running version that is missing or not numeric.

The link gate is unchanged: a link the next boot can run but the running base cannot is still linked; only the immediate refresh is skipped.

Both gates report one line per compared image without `--verbose`: `<component>: minimum image <value>, next-boot image <version> (<source>)` for the link gate, and `<component>: minimum image <value>, running image <version>` for the refresh gate, whether the image passes or not. The line lets an operator read the decision from the journal, where the `--verbose` debug lines are absent.

- **`UpdateFeatures` / `EnableFeature{Now}`:** the links stay (they are correct for the next boot) and the refresh is skipped. `UpdateFeaturesResult.RefreshSkipped` (JSON `refresh_skipped`, host-wide like `PendingActivations`) or `FeatureActionResult.RefreshSkipped` carries `refresh skipped: <component> needs image >= <value> (running image <running>); it takes effect at the next boot`, the reporter gets a warning, and `FeatureActionResult.NextActionMessage` says to reboot. It is not an error and the run returns no error from it. A dry run reports the same text and refreshes nothing.
- **`RollbackFeature{Now}`:** the record and the links stay; only the refresh is skipped, with `RollbackFeatureResult.RefreshSkipped` and a `NextActionMessage` that says to reboot.
- **`DisableFeature{Now}`:** the unmerge comes **before** the refresh, and skipping the refresh would leave every sysext on the host unmerged. The check therefore runs before the `Unmerge`; when it fails, the whole command is refused with an error that names the component and asks for a run without `--now` and a reboot. Nothing changes: no drop-in, no unmerge, no file removal, no refresh. A dry run reports the same refusal.
- `NoRefresh` (the packaged timer) runs no refresh, so it runs no running-image check and reports nothing skipped.

**UpdateFeaturesOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `DryRun` | `bool` | Preview downloads, installs, refreshes, and vacuum removals without modifying filesystem or sysext state; still fetches manifests and inspects local installed files |
| `NoRefresh` | `bool` | Skip `systemd-sysext refresh` after updates |
| `NoVacuum` | `bool` | Skip removing old versions |
| `Component` | `string` | Scope to one named component; `""` = default union |

### Refresh warning

`UpdateFeatures` (without `NoRefresh`), `EnableFeature` with `Now`, and `DisableFeature` with `Now` end in `systemd-sysext refresh`, which merges every link on the host that names a version other than the running one. Before the refresh (or, with `DryRun`, in place of it) each of them lists, through the reporter as one warning, the *other* pending sysexts, and records them in the result's `PendingActivations` (`[]PendingActivation`, JSON `pending_activations`, `omitzero`). The warning is advice. The refresh itself can still be skipped by the running-image check (see "Refresh gate" above), which is reported as `refresh_skipped`. `NoRefresh` lists nothing.

- A transfer is *pending* when `LinkedVersion` is non-empty, differs from `RunningVersion`, and `RunningVersion` is not `unknown`, and its state was read without error (see [TransferStatus](#transferstatus)).
- A transfer is a pending *deactivation* (`Deactivate: true`, empty `LinkedVersion`) when it is merged (any `RunningVersion`, `unknown` included) and has no link, for example after `features disable` without `--now`: the refresh unmerges it under any workload that uses it. Deactivations get their own warning line (`… will also deactivate N sysext(s) that are merged but not linked: …`). An image that another systemd-sysext search directory (`/etc/extensions`, `/run/extensions`, `/usr/lib/extensions`) still provides stays merged; updex cannot tell from the version alone and lists it anyway.
- **A caller that does not show the reporter's warnings (a GUI, an automation) must show `PendingActivations` to its user before it asks for a refresh** (`Now`, or `UpdateFeatures` without `NoRefresh`): the list is the only signal that the refresh changes sysexts the user did not name. Use `DryRun` to get the list without refreshing.
- *Own* transfers are excluded, matched by link name (`SysextLinkName`), because the link is what the refresh activates. For `EnableFeature` and `DisableFeature` they are the named feature's transfers. For `UpdateFeatures` they are the transfers of every enabled, unmasked feature: the operator asked to update those, and the refresh is what applies them, including a link that an earlier run (the timer) already moved. What remains is the "other" set, for example a disabled feature whose link still names a newer image, and any ungated transfer.
- The set is computed over the whole host's definitions, even when `Component` scopes the call or `Definitions` names another directory, because the refresh does not honour either scope: the caller's domain is read together with the host's normal definition roots, and a transfer that both describe is listed once. A link made by hand for an image that no transfer describes is not visible.
- `DisableFeature` reads the pending state *before* `Unmerge`, which clears the merged state and would make every link look pending, and warns just before the refresh.
- A transfer whose state cannot be read is skipped with a warning (`could not determine whether a refresh activates …`).
- `UpdateFeaturesResult.PendingActivations` is host-wide: every element of the returned slice carries the same list. With no enabled features the slice is empty and only the warning reports it.

### RollbackFeature

```go
func (c *Client) RollbackFeature(ctx context.Context, name string, opts RollbackFeatureOptions) (*RollbackFeatureResult, error)
```

Links every **active** transfer of a feature (see [Activation rule](#activation-rule); the feature is forced enabled) to an older staged version and records what it rolled back from, so the update path does not undo it ([ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md), "Rollback"). Except in a dry run it holds the operation lock (`ErrBusy`).

**Target.** Per transfer: with no `To`, the newest staged version (`sysext.StagedVersionsAt`) that is older than the version the sysext link names (`sysext.LinkedVersionAt`, `version.Compare`); each transfer of a multi-transfer feature goes to **its own** newest older version, so the transfers may land on different versions. With `To`, every transfer goes to that version. A transfer with no usable link, no older staged version, a `To` that is not staged (`version X of <component> is not on disk`), equals the linked version (`is already linked`) or is not older than the version rolled back from, blocks the whole rollback: every problem is joined into one error and nothing changes (no link, no record, no directory). The feature must exist and not be masked; it may be disabled. An associated transfer that is not active is not rolled back, and a first rollback adds no entry for it; an existing rejection of it stays in the record (see "Record"). A feature that has transfers but none active fails with `feature '<name>' has no active transfers to roll back: <component> waits for feature(s) …` and changes nothing; one with no associated transfer fails with `feature '<name>' has no transfers to roll back`.

**Record.** `<RuntimePaths.RollbackDir>/<feature>`, JSON written through `writeManagedFile`-style temp file plus rename after `Lstat` checks of the directory and the file (a symlink or non-regular entry is refused). The feature name is validated with `catalog.ValidateSysextName` first. Format 1: `{"format":1,"feature":"incus","created":"<RFC 3339 UTC>","rejected":{"<component>":"<version>"}}`, one rejected version per transfer: the version rolled back from, or a newer version that an earlier record already rejected. An existing record that cannot be read blocks a new rollback (`run 'updex features rollback --clear <name>' to delete it first`). The record is written **before** the links; the links then move all or none (`Client.linkAllOrNone`, the Phase 1 snapshot and atomic restore, through `sysext.VersionSysextRunner.LinkVersionAt`), and a link failure restores the links and the previous record. A runner that implements only `sysext.PathSysextRunner` is refused with `ErrLegacySysextRunner` before any change. The record names only the rolled-back feature, and only its **active** transfers (ADR-0017). One exception: when a rollback rewrites the record, it keeps every existing entry whose component is an **inactive associated** transfer of the feature (`preservedRejections`), with no link change, so a repeated rollback while a requisite feature is disabled cannot erase a rejection that applies again when that feature is enabled (a dry run says it in one reporter line per kept entry; no result field). An entry for a component that is no associated transfer is dropped. The record's hold covers **every** feature that shares one of those transfers (`Features=` or `RequisiteFeatures=`), enabled or not, **while that feature acts on it**: the update and status paths read the records of every feature that names a transfer they act on, so another feature cannot undo the rollback. A record that applies only to a transfer a feature does not act on (it is not active for that feature) holds nothing of that feature: `maild` with `lodge` disabled is not held by a record of `lodge` that names only `T` (`Features=maild` `RequisiteFeatures=lodge`). Only the rolled-back feature advances a transfer its record names; every other feature that shares it is held while the record exists (see "What the update path does with the record").

**Effect.** Without `Now`, the rollback takes effect at the next boot (`NextActionMessage` says so; it no longer offers a plain `systemd-sysext refresh`, because that runs no `systemd-sysusers`, #479). With `Now` (and not `NoRefresh`) it lists the other pending activations (`PendingActivations`, see "Refresh warning"), runs `systemd-sysext refresh`, and sets `Refreshed`; a failed refresh keeps links and record and returns `sysext refresh failed: …` with `RefreshError` set, and its `NextActionMessage` advises a reboot: when the error does not wrap `sysext.ErrActivationIncomplete` it says the merge state is not known, and when it does it says the extensions are merged but activation did not finish (see "Refresh failures are reported, not swallowed"). `DryRun` reports the plan and writes nothing.

**Clear.** `Clear: true` deletes the record and changes no link; it needs no feature and cannot be combined with `To` or `Now`. A missing record is success with `NoRecord` set; `Cleared` is set when a record was (in a dry run: would be) deleted.

**What the update path does with the record** (`installTransfers`, so `UpdateFeatures` and `EnableFeature{Now}`): every applicable record is read first (`collectRollbackHolds`): the record of each feature that names one of the transfers it acts on (the feature's active transfers, ADR-0017; `installTransfers` is never called with an inactive transfer, and `Features` and `CheckFeatures` compute the holds over the same set), in `Transfer.Features` or in `Transfer.RequisiteFeatures`, enabled or not, each read once per call. When several records reject the same component, the highest rejected version applies (`version.Compare`). If any applicable record cannot be read (malformed, unsupported `format`, another feature name, not a regular file, larger than 64 KiB, symlinked directory), nothing is downloaded or linked for the feature, every transfer reports `not linking feature <name>: rollback record of feature <owner> cannot be read: …`, and other features continue. **Only the feature that wrote a record advances the transfers its record names.** When a record of another feature `<owner>` applies, the whole feature being processed is held: no download, no link, each `UpdateResult` has `Held=true`, and `NextActionMessage` is `held by rollback of <owner>: it advances only when <owner> updates (or run 'updex features rollback --clear <owner>')`. `<owner>`'s transfers link all or none and its record is deleted only after that link, so this hold keeps the rolled-back feature whole in every processing order and after any download or link failure in `<owner>`; a sharing feature never links an image that a failed run of `<owner>` staged. The hold lasts while the record exists, also when `<owner>` is disabled or masked; updex never clears the record by itself. If two features that share a transfer both have a record, each holds the other until one record is cleared. For the feature's **own** record, it still holds when **any** component it rejects has no available version newer than that version (`rollbackHoldReason`, `version.Compare`); this covers every component the record names, also one outside the feature's own transfers, whose transfer is looked up in the loaded domain and whose versions are read through the call's manifest cache. A component with no transfer in the domain holds the record too (fail closed): the operation cannot tell whether a newer version exists, so it does not advance. A failure to read the versions of a record component fails closed as well: nothing is downloaded or linked, every transfer reports `not linking feature <name>: rollback record of feature <owner> needs the newest version of <component>: …`, and other features continue. When the feature's own record still holds, the **whole feature is held**: nothing is downloaded or linked (so the "already staged, relink" path cannot move the link back to the rejected image), the old links stay, and each `UpdateResult` has `Held=true`, `RejectedVersion`, and `NextActionMessage` `held by rollback: rejected <version>` when its own rejected version is the reason, else `held by rollback of <owner>: <component> has no version newer than <version>` (or `… has no transfer in the loaded definitions`); this is not an error. Otherwise the newest version is staged and linked as usual, the vacuum runs with the rejected version in `VacuumPaths.ProtectVersions`, and then the record is deleted when every version it rejects is now behind a link (`deleteFinishedRollbackRecords`, `sysext.LinkedVersionAt`; a component of the record that is not one of these transfers is looked up in the loaded domain, and a record whose component has no transfer or no readable link stays). Only the processed feature's own record is deleted, so a record is deleted only by an update of the feature that owns it. `Features` and `CheckFeatures` make the same decision (`TransferStatus.RollbackRejected`/`RollbackHeldBy`, `CheckResult.RollbackRejected`/`RollbackHeldBy`; `UpdateAvailable` is false while a record holds; an unreadable applicable record, or a version lookup that fails while deciding the feature's own hold, is the transfer's `Error`, naming the feature whose record failed). `Features` reads manifests only when the feature's own record applies, because that hold needs the newest versions; a record of another feature is decided without them.

**RollbackFeatureOptions:**
| Field | Type | Description |
|-------|------|-------------|
| `To` | `string` | Version to link for every transfer; empty = the next older staged version per transfer |
| `Now` | `bool` | Apply with a `systemd-sysext refresh` |
| `Clear` | `bool` | Delete the record, change no link |
| `DryRun` | `bool` | Preview without writing |
| `NoRefresh` | `bool` | Skip the refresh `Now` would run |
| `Component` | `string` | Scope to one named component; `""` = default union |

```go
type RollbackFeatureResult struct {
    Feature            string              `json:"feature"`
    Action             string              `json:"action"` // "rollback" or "rollback-clear"
    Success            bool                `json:"success"`
    DryRun             bool                `json:"dry_run,omitempty"`
    Transfers          []RollbackTransfer  `json:"transfers,omitzero"`
    Record             string              `json:"record,omitempty"`
    Cleared            bool                `json:"cleared,omitempty"`
    NoRecord           bool                `json:"no_record,omitempty"`
    Refreshed          bool                `json:"refreshed,omitempty"`
    RefreshError       string              `json:"refresh_error,omitempty"`
    PendingActivations []PendingActivation `json:"pending_activations,omitzero"`
    Error              string              `json:"error,omitempty"`
    NextActionMessage  string              `json:"next_action_message,omitempty"`
}

type RollbackTransfer struct {
    Component       string `json:"component"`
    FromVersion     string `json:"from_version"`
    ToVersion       string `json:"to_version"`
    RejectedVersion string `json:"rejected_version"`
}
```

### CheckFeatures

```go
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error)
```

Checks for available updates without downloading. Manifests are cached by source URL, same as `UpdateFeatures`. Only the active transfers of each enabled feature are checked (see [Activation rule](#activation-rule)); the others are listed in `CheckFeaturesResult.Inactive`. The rollback holds are computed over the active transfers, the same set `UpdateFeatures` acts on.

`CurrentVersion` is the version the host actually runs: the version that the systemd-sysext link `<SysextLinkDir>/<component>.<ext>` points to, read by `sysext.LinkedVersionAt`. That link is the image systemd-sysext merges, so it wins over the staging directory, where another updater (for example `systemd-sysupdate`) can stage a newer image and advance its own legacy `CurrentSymlink` without touching the link updex made. When no usable link exists (absent, not a symlink, dangling, or naming an image the transfer's target patterns do not match), `CheckFeatures` keeps the staging-based current version that `sysext.GetInstalledVersions` reports: the legacy `CurrentSymlink` target, else the newest staged file. `UpdateAvailable` is then `version.Compare(newest, CurrentVersion) > 0`. A link that points at an older image than the newest staged file therefore reports an update, and the next `UpdateFeatures` relinks without downloading (see [UpdateFeatures](#updatefeatures)).

`CheckResult.RunningVersion`, `LinkedVersion` and `PendingReboot` come from the same per-transfer read as `Features` (see [TransferStatus](#transferstatus)). `LinkedVersion` is what `sysext.LinkedVersionAt` reports; with a usable link it equals `CurrentVersion`, and `CurrentVersion` keeps its staging-based fallback when there is none. `RunningVersion` comes from `sysext.MergedVersionIn`, which reads only the hierarchy metadata under the client's `SysextHierarchies`: a legacy `CurrentSymlink` or an image in `/run/extensions` does not make an image "running". A failure to read the merged state is reported like a link-inspection failure: that component's `Error` (`failed to read merged state: …`), with `NewestVersion`, `RunningVersion` and `LinkedVersion` kept where known. A manifest failure keeps the status values too. The status is read before the manifest fetch.

Per-transfer failures are reported, never dropped: when a component's manifest cannot be fetched or verified (network/HTTP error, GPG signature failure, invalid source pattern), or its installed versions cannot be listed, `CheckFeatures` appends a `CheckResult` for that component with `Error` set (and `UpdateAvailable=false`), keeps checking the remaining transfers, and after the loop returns the collected results together with the aggregate error `one or more components failed to check` — the same shape as `UpdateFeatures`. Consumers must therefore treat a non-nil error as "the results are partial", not "no results", and use `CheckResult.Error` to tell "could not check" from "no update". A source that lists no matching versions is not an error: that component is simply absent from `Results`.

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
  `EnableFeature{Now: true, Component: repo.Component}`. Before install it
  snapshots every matching staged image and the effective sysext link in
  addition to the generated definitions and enable drop-in. Regular staged
  files are streamed to same-directory temporary backups, bounding heap use
  independently of image size; temporary disk use scales with retained image
  size, and backups are removed after success or rollback. A matching staged
  entry or link destination that is neither a regular file nor a symlink is
  refused rather than replaced. *Every* failure after definition writes begin,
  including download, link, vacuum, and final-refresh failures, restores the
  prior state exactly. Definitions and captured rollback contents are written
  through a same-directory temporary regular file plus rename, so a symlink
  planted after the ownership check is replaced rather than followed;
  rollback preserves the captured permissions. A fresh add's files are deleted
  (with the drop-in dir and component dir removed if empty), and a re-add's
  previous `.transfer`/`.feature`/`00-updex.conf`, matching staged images, and
  sysext link target are restored. Rollback errors are joined with the original
  operation error so neither failure is hidden. With
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

    TransferStatus []TransferStatus `json:"transfer_status,omitzero"` // same order as Transfers
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

### TransferStatus

```go
type TransferStatus struct {
    Component      string `json:"component"`
    RunningVersion string `json:"running_version"` // merged now; "" = not merged; "unknown" = merged, no recorded version
    LinkedVersion  string `json:"linked_version"`  // the /var/lib/extensions link names it; "" = no usable link
    PendingReboot  bool   `json:"pending_reboot,omitempty"`
    RollbackRejected string `json:"rollback_rejected,omitempty"` // highest version any applicable rollback record rejects; set by Features and CheckFeatures for every feature that shares the transfer
    NeedsImage     string `json:"needs_image,omitempty"` // "needs image >= X": the newest staged image is not linked and `features update` would not link it; set by Features and CheckFeatures
    RollbackHeldBy   string `json:"rollback_held_by,omitempty"`   // why a record holds the transfer although it could advance: for the feature's own record "<owner>: <component> has no version newer than <version>" (or "has no transfer in the loaded definitions"), for another feature's record "<owner>: it advances only when <owner> updates (or run 'updex features rollback --clear <owner>')"; empty when the transfer's own RollbackRejected is the reason
    Error          string `json:"error,omitempty"`
}

type PendingActivation struct {
    Component      string `json:"component"`
    RunningVersion string `json:"running_version"`
    LinkedVersion  string `json:"linked_version"`
    Deactivate     bool   `json:"deactivate,omitempty"` // merged but not linked: the refresh unmerges it
}
```

Defined in `updex/status.go` (ADR-0016, "Status and logs"). `RunningVersion` is read from the merged-state metadata under the client's `sysextHierarchies` ([ADR-0015](../adr/0015-detect-merged-sysexts-from-hierarchy-metadata.md)) with `sysext.MergedVersionIn`; `LinkedVersion` from `sysext.LinkedVersionAt` with the client's `SysextLinkDir`. The JSON keys `running_version` and `linked_version` are always present: an empty string means none, so a consumer can tell "not merged" from an absent key of an older updex.

`PendingReboot` is true when the next boot changes what the host runs: `LinkedVersion` is non-empty and differs from `RunningVersion`. It is false when `RunningVersion` is `unknown` (the two cannot be compared; a false alarm on every host with old-style metadata would hide real ones) and when `Error` is set. `Error` carries `failed to read merged state: …` and/or `failed to inspect sysext link: …`; the value that could be read stays set. The CLI renders `RunningVersion` and `LinkedVersion` as the `RUNNING` and `LINKED` columns (`-` for empty, `error` when `Error` is set) and lists the pending transfers in a `PENDING REBOOT:` footer.

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
    NotLinked         []string `json:"not_linked,omitzero"`     // Now: images downloaded but not linked because they need a newer base image
    RefreshError      string   `json:"refresh_error,omitempty"` // set when only the final systemd-sysext refresh failed

    PendingActivations []PendingActivation `json:"pending_activations,omitzero"` // other sysexts the refresh activates (see Refresh warning)
    Inactive           []InactiveTransfer  `json:"inactive,omitzero"`            // enable: transfers not downloaded because they are not active (see Activation rule)
    Kept               []KeptTransfer      `json:"kept,omitzero"`                // disable --now: transfers not removed because another enabled feature still activates them
}

// InactiveTransfer: a transfer a feature command did not act on.
type InactiveTransfer struct {
    Component         string   `json:"component"`
    MissingRequisites []string `json:"missing_requisites,omitzero"` // RequisiteFeatures= entries that are not enabled
    NeedsOneOf        []string `json:"needs_one_of,omitzero"`       // Features= entries, when none is enabled
}
// Reason() renders it: "waits for feature(s) lodge", "needs one of features: maild", or both joined by "and".

// KeptTransfer: a transfer disable --now did not remove.
type KeptTransfer struct {
    Component     string   `json:"component"`
    ActiveThrough []string `json:"active_through"` // the other enabled features of its Features=
}
```

`RefreshError` is non-empty only when every requested change succeeded and the closing `systemd-sysext refresh` did not; `Success` is then `false`, `Error` mirrors `RefreshError`, and `NextActionMessage` tells the operator how to activate (see EnableFeature / DisableFeature above).

> **Note:** Slice fields use `omitzero` (Go 1.24+) — they are omitted from JSON when nil/empty. Scalar fields use `omitempty` for the same effect on zero values.

### UpdateFeaturesResult / UpdateResult

```go
type UpdateFeaturesResult struct {
    Feature string         `json:"feature"`
    Results []UpdateResult `json:"results"`

    Inactive           []InactiveTransfer  `json:"inactive,omitzero"`            // transfers not acted on because they are not active (see Activation rule); no download, link or vacuum
    PendingActivations []PendingActivation `json:"pending_activations,omitzero"` // host-wide; see Refresh warning
}

type UpdateResult struct {
    Component         string   `json:"component"`
    Version           string   `json:"version"`
    Downloaded        bool     `json:"downloaded"`
    Installed         bool     `json:"installed"`
    DryRun            bool     `json:"dry_run,omitempty"`
    Error             string   `json:"error,omitempty"`
    NextActionMessage string   `json:"next_action_message,omitempty"` // when Held: "held by rollback: rejected <version>", "held by rollback of <owner>: <component> has no version newer than <version>" (or "has no transfer in the loaded definitions"), or "held by rollback of <owner>: it advances only when <owner> updates (or run 'updex features rollback --clear <owner>')"
    RemovedVersions   []string `json:"removed_versions,omitzero"`
    Held              bool     `json:"held_by_rollback,omitempty"`  // a rollback record holds the feature; nothing downloaded or linked; not an error; a record of another feature holds the feature while only that feature advances its transfers
    RejectedVersion   string   `json:"rejected_version,omitempty"`  // the record's version for this transfer, when Held
    NeedsImage        string   `json:"needs_image,omitempty"`       // "needs image >= X": the staged image needs a newer base image; nothing of the feature was linked; not an error
    NotLinked         bool     `json:"not_linked,omitempty"`        // another transfer of the feature NeedsImage, so this one was not linked either
    NextBootImageVersion string `json:"next_boot_image_version,omitempty"` // the next-boot IMAGE_VERSION that was compared
    Linked            bool     `json:"linked,omitempty"`            // this run moved the sysext link to Version (download path or relink); always false in a dry run
}
```

For dry-run update results, `Downloaded=true` means the component would be downloaded, `Installed=false` means no install was performed, and `RemovedVersions` lists versions vacuum would remove if `NoVacuum` is false. For non-dry-run results, `Downloaded=true` means a new file was fetched and installed; already-current components still report `Installed=true` but `Downloaded=false`. `Linked=true` means this run moved the sysext link to `Version`, on the download path and on the relink path (an already staged image whose link was missing, dangling, or pointed elsewhere), and it stays false in a dry run: `Downloaded=false` with `Linked=true` is a relink that needs no download. The CLI's text table shows `linked` in its `STATUS` column for such a row instead of `up to date`. Non-dry-run `RemovedVersions` is currently not populated because the install pipeline does not report what `sysext.VacuumWith` removed.

### CheckFeaturesResult / CheckResult

```go
type CheckFeaturesResult struct {
    Feature  string             `json:"feature"`
    Results  []CheckResult      `json:"results"`
    Inactive []InactiveTransfer `json:"inactive,omitzero"` // transfers not checked because they are not active (see Activation rule)
}

type CheckResult struct {
    Component       string `json:"component"`
    CurrentVersion  string `json:"current_version,omitempty"` // link, else staging; kept for compatibility
    NewestVersion   string `json:"newest_version"`
    RunningVersion  string `json:"running_version"`           // merged now; always present
    LinkedVersion   string `json:"linked_version"`            // the link names it; always present
    PendingReboot   bool   `json:"pending_reboot,omitempty"`  // LinkedVersion != RunningVersion
    RollbackRejected string `json:"rollback_rejected,omitempty"` // highest version an applicable rollback record rejects; UpdateAvailable is false while NewestVersion is not newer, or while a record of another feature holds the feature
    RollbackHeldBy   string `json:"rollback_held_by,omitempty"`   // why a record holds the feature although this component could advance (see TransferStatus.RollbackHeldBy); UpdateAvailable is false while set
    UpdateAvailable bool   `json:"update_available"`
    NeedsImage      string `json:"needs_image,omitempty"` // "needs image >= X": a staged unlinked image that update would not link; not an error
    NextBootImageVersion string `json:"next_boot_image_version,omitempty"` // read only when a staged unlinked image has the key
    Error           string `json:"error,omitempty"` // set when the component could not be checked
}
```

`NeedsImage` is set from the newest staged image that is not the linked one and that no rollback record holds: `CheckFeatures` runs the same read as the update path (read-only; it never downloads, so it cannot tell for a newer image that is only published, and it runs bootc only when that staged image has the key). A bootc failure or an unreadable image is the component's `Error`, as in update. The CLI shows `needs image >= X` in the `UPDATE` column and a `NEEDS NEWER IMAGE:` footer.

`Error` is non-empty only for a component whose check failed (see `CheckFeatures` above); `UpdateAvailable` is always `false` in that case, and `NewestVersion`/`CurrentVersion` are empty unless the failure happened after the manifest was read (an installed-versions or sysext-link inspection failure keeps `NewestVersion`). The CLI renders such a row with `UPDATE=error` and exits non-zero.

## Supporting Packages

### `config`

- `LoadFeatures(customPath string) ([]*Feature, error)` — Load all `.feature` files from `customPath`, or the legacy default search paths if empty. No component discovery.
- `LoadTransfers(customPath string) ([]*Transfer, error)` — Load all `.transfer` files from `customPath`, or the legacy default search paths if empty. No component discovery, no sysext-type filtering.
- `FilterTransfersByFeatures(transfers []*Transfer, features []*Feature) []*Transfer` — Filter transfers to the active ones for the given features (`CheckTransferActivation` over `EnabledFeatureSet`); all transfers when no feature is defined
- `EnabledFeatureSet(features []*Feature) map[string]bool` — The names of the features that are enabled and not masked
- `CheckTransferActivation(t *Transfer, enabled map[string]bool) TransferActivation` — The sysupdate.d(5) activation rule, the single implementation: `Features=` empty or one enabled (OR), every `RequisiteFeatures=` enabled (AND). `TransferActivation{Active, MissingRequisites, NeedsOneOf}` says why a transfer is not active; a name that is not in `enabled` is not enabled
- `GetTransfersForFeature(transfers []*Transfer, featureName string) []*Transfer` — Get transfers associated with a specific feature by membership in `Features` or `RequisiteFeatures`; this is association lookup only, and must not decide what is downloaded, linked or removed (use `CheckTransferActivation`)
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
- `LoadRepoFrom(configRoots []string, name string) (Repo, error)` — Load one repo from the first root holding `<name>.catalog` (the file `LoadReposFrom` would pick; found with `os.Lstat`, so a dangling higher-priority symlink fails to load rather than yielding to a lower root), ignoring every other repo's file; returns `ErrRepoNotFound` when none holds it. `Client.verifyPolicy` uses it so an unrelated broken `.catalog` cannot revoke an opt-in.
- `RepoByName(repos []Repo, name string) (Repo, bool)`
- `type Repo struct { Name, SiteURL, ListURL, Component string; AllowInsecure, AllowUnsigned bool }` — `Component` defaults to `catalog-<name>`; both names validated against `[a-zA-Z0-9_-]+`. Parsed `.catalog` files require absolute HTTPS `SiteURL`/`ListURL` values unless `AllowInsecure=yes`; the opt-in is intended only for trusted development and test endpoints. `AllowUnsigned=yes` lets the catalog's published `Verify=` decide whether its `SHA256SUMS` must be signed ([ADR-0014](../adr/0014-local-catalog-signature-policy.md)); it does not relax HTTPS or SHA256 checks, and a file combining it with `AllowInsecure=yes` fails to parse.
- `List(ctx, *http.Client, Repo) ([]string, error)` — Enumerate sysexts via the repo's `ListURL` (GitHub contents API shape): top-level `dir` entries minus dotted names and `docs`/`LICENSES`. Sends `GITHUB_TOKEN` as a bearer token only to the `https://api.github.com` origin and strips authorization from redirects to other origins. Always live; no cache.
- `CachedList(ctx, *http.Client, Repo, CachedListOptions) ([]string, CacheResult, error)` — `List` behind a per-repo TTL+ETag cache in `CacheDir` (default `os.UserCacheDir()/updex`, empty disables). `CachedListOptions{TTL /* 0 → DefaultListCacheTTL (60 min) */, NoCache}`; `CacheResult{FromCache, Stale, Age, WriteErr}`. Validates `Repo.Name` (public API: the name becomes a cache filename). Within TTL: cache, zero network. Expired: conditional GET (`If-None-Match`; 304 bumps the timestamp, rate-limit-free on GitHub). Fetch failure with an entry present: stale served, `Stale: true` — except `context.Canceled`/`DeadlineExceeded`, which propagate. Entries are invalidated when the repo's `ListURL` changes; corrupt files are misses; writes are best-effort — a failed write never fails the listing but is reported via `CacheResult.WriteErr`.
- `FetchConf(ctx, *http.Client, Repo, name) ([]byte, error)` — GET `<SiteURL>/<name>/<name>.conf`; 404 wraps `ErrNotFound`. Validates `name` first.
- `RenderTransfer(conf []byte, repo Repo, name string) ([]byte, error)` — Byte-preserving line transform ([ADR-0006](../adr/0006-byte-preserving-render-transfer.md)): prepend the `GeneratedMarker` header, inject `Features=<name>` after `[Transfer]` (appending the section if missing, replacing an existing `Features` key), drop `Target CurrentSymlink`, keep `%w`/`%a` specifiers unexpanded. The catalog's `Verify=` is always dropped; when `repo.AllowUnsigned` is set, a canonical `Verify=yes|no` (parsed with `MustBool(true)`) is written after `Features=`. `RenderTransferTo` is the explicit-target-path variant. Validates `[Source]`/`[Target]` presence via `ini.Load`.
- `RenderFeature(Repo, name) []byte` — `GeneratedMarker` header plus `[Feature]` stanza with `Description`, `Documentation=<SiteURL>/<name>/`, and `Enabled=false` (enabling goes through the standard drop-in).
- `GeneratedMarker` / `IsGenerated(data []byte) bool` / `IsGeneratedFile(path string) bool` — Ownership signal for generated files: the header `# Generated by updex catalog (repo: <name>); ...` ([ADR-0003](../adr/0003-catalog-ownership-marker.md)).
- `GeneratedRepo(data []byte) (repo string, ok bool)` / `GeneratedFileRepo(path string) (repo string, ok bool)` — Parse the generating repo out of the marker. `GeneratedFileRepo` reads enough bytes to fit the header for any repo name up to 255 bytes (the filename-stem limit), so long names are never misread as unmarked. `CatalogAdd`/`CatalogRemove` compare this against the acting repo, so neither a foreign file nor another catalog sharing the same `Component` can be overwritten or deleted.
- `GeneratedFileHeader(path string) (repo string, marked bool, err error)` — One bounded read of the header: the repo its marker names (empty when none parses), whether it starts with the `GeneratedMarker` prefix at all, and any open/read error. `Client.verifyPolicy` requires a signature on an error or on a marked header without a parseable repo.
- `ValidateSysextName(name string) error` — Rejects names that aren't a safe single filename/URL component (`^[a-zA-Z0-9_][a-zA-Z0-9._+-]*$`).

### `manifest`

- `Fetch(ctx context.Context, httpClient *http.Client, baseURL string, verify bool, opts ...Option) (*Manifest, error)` — Fetch and parse `SHA256SUMS` from URL. If `httpClient` is nil, a default client with a 30-second timeout, the standard 10-redirect limit, and an HTTPS-to-HTTP downgrade refusal (`internal/httpclient`, shared with `updex.NewClient` and `download.Download`) is used; HTTP-to-HTTP and HTTPS-to-HTTPS redirects remain allowed. A caller-supplied `httpClient` keeps its own redirect policy. The `SHA256SUMS` GET and body read retry transient network failures and HTTP 5xx/429 up to 3 total attempts with exponential backoff; TLS/cert errors, unsupported protocols, and 4xx other than 429 fail immediately. The detached `SHA256SUMS.gpg` fetch used when `verify=true` shares that retry policy (same classification and the same `WithRetryConfig`/`WithRetryNotify` settings); keyring loading and signature checking are never retried. `WithRetryConfig(maxAttempts int, baseDelay time.Duration)` overrides retry bounds for tests or SDK consumers; `WithRetryNotify(func(attempt, maxAttempts int, reason error))` reports retry attempts
- `ErrSignatureNotFound` — wrapped into `Fetch`'s error when `verify=true` and `SHA256SUMS.gpg` returns 404 (not retried); other signature failures do not wrap it. The SDK uses it to prefix the applicable verification policy
- `Manifest.Verified bool` — true only when `Fetch` was called with `verify=true` and the detached signature check succeeded; false for `verify=false` fetches. Consumers that cache manifests across transfers must not serve an unverified manifest to a transfer that requires verification (see `UpdateFeatures`)
- `VerifyHash(filePath string, expectedHash string) error` — Verify a file's SHA256
- `VerifyHashReader(r io.Reader, expectedHash string) *HashVerifyReader` — Streaming hash verification

### `download`

The retry policy shared by `download` and `manifest` (`internal/retry`) is
recorded in [ADR-0008](../adr/0008-bounded-retry-no-resume.md).

- `Download(ctx context.Context, httpClient *http.Client, url, targetPath, expectedHash string, mode uint32, onProgress ProgressFunc, opts ...Option) error` — Download with hash verification (on compressed bytes) and auto-decompression. Uses atomic rename, and on every path fsyncs the file before renaming it into place: the verified temp file is synced before it is closed and renamed, a decompressed output file is synced before it is renamed, and on cross-device rename failure the copy through a temp file on the destination device is synced, chmodded, then renamed into place. A sync failure is returned wrapped and leaves the target path untouched. If `httpClient` is nil, a default client with a 10-minute timeout, the standard 10-redirect limit, and an HTTPS-to-HTTP downgrade refusal (`internal/httpclient`, shared with `updex.NewClient` and `manifest.Fetch`) is used; HTTP-to-HTTP and HTTPS-to-HTTPS redirects remain allowed. A caller-supplied `httpClient` keeps its own redirect policy. Default mode: `0644` if `mode == 0`. GETs and response-body reads retry transient network failures and HTTP 5xx/429 up to 3 total attempts with exponential backoff; each retry re-requests the file from scratch and uses a fresh temp file. 4xx other than 429 and checksum mismatches fail immediately. Decompressed output is capped at `DefaultMaxDecompressedSize` (8 GiB); `WithMaxDecompressedSize(bytes int64)` sets a positive per-call cap. Crossing it returns an error matching `ErrDecompressedTooLarge`, removes compressed and decompressed temp files, and leaves the target path untouched. The raw bytes read from the server (the compressed, or for an uncompressed image the raw, payload) are separately capped at `DefaultMaxDownloadSize` (twice `DefaultMaxDecompressedSize`); `WithMaxDownloadSize(bytes int64)` sets a positive per-call cap. An over-limit `Content-Length` is rejected before any bytes are streamed, and the read itself is bounded with `io.LimitReader` in case `Content-Length` is absent or understated; crossing the cap either way returns an error matching `ErrDownloadTooLarge`. `WithRetryConfig(maxAttempts int, baseDelay time.Duration)` overrides retry bounds for tests or SDK consumers; `WithRetryNotify(func(attempt, maxAttempts int, reason error))` reports retry attempts
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
- `PathSysextRunner` interface — `SysextRunner` plus `LinkToSysextAt(*config.Transfer, sysextDir string) error`. `updex.Client` links only through this interface, with the `SysextLinkDir` it captured at construction; a runner implementing only `SysextRunner` is refused with `updex.ErrLegacySysextRunner` rather than redirected to the package-global `SysextDir`. `DefaultRunner` and `MockRunner` both implement it; `MockRunner.LinkToSysextAtDir` records the directory it was given (and `LinkToSysextCalled` is set by either entry point)
- `GetInstalledVersions(t *config.Transfer) ([]string, string, error)` — List installed + current version
- `StagedVersionsAt(t *config.Transfer, defaultDir string) ([]string, error)` — Versions of the transfer's staged images (regular files in the target directory that match the target patterns), newest first by `version.Compare`; nil when the directory is absent
- `LinkVersionAt(t *config.Transfer, sysextDir, version string) error` — Atomically (temp symlink plus rename) point `<sysextDir>/<SysextLinkName(t)>` at the staged image of exactly that version; errors with `version X of <component> is not on disk` and changes nothing otherwise. `LinkToSysextAt` links the newest image through the same helper
- `VersionSysextRunner` interface — `PathSysextRunner` plus `LinkVersionAt(t *config.Transfer, sysextDir, version string) error`. `RollbackFeature` links only through it; a runner that does not implement it is refused with `updex.ErrLegacySysextRunner`. `DefaultRunner` and `MockRunner` implement it (`MockRunner.LinkVersionAtCalls`, `LinkVersionAtErr`); `MockRunner` creates no link
- `StagedImageAt(t *config.Transfer, defaultDir, version string) (string, error)` — Path of the staged image of exactly `version` (the file `LinkVersionAt` would link); `""` with a nil error when no staged file has that version. The refresh gate reads the image it names
- `LinkedVersionAt(t *config.Transfer, sysextDir string) (string, error)` — Version of the image the systemd-sysext link `<sysextDir>/<SysextLinkName(t)>` points to; `""` with a nil error when the link is absent, not a symlink, dangling, or names an image the transfer's target patterns do not match. A relative link target is resolved against `sysextDir`. `CheckFeatures` uses it for `CurrentVersion`; `GetInstalledVersions` stays staging-based because `stageTransfer` relies on that to skip a re-download
- `MergedVersionIn(t *config.Transfer, hierarchies []string) (string, error)` — Version of the transfer's image that the `.systemd-sysext` metadata under `hierarchies` records (`UnknownVersion` when the metadata records the image without a version, `""` when not merged; first hierarchy that records it wins; a read error returns `read merged state: …`). Unlike `GetActiveVersionWith` it ignores a legacy `CurrentSymlink` and `/run/extensions`, so it answers "merged now", not "active or about to be". `updex.Client` uses it for `RunningVersion`
- `GetActiveVersion(t *config.Transfer) (string, error)` — Get the version considered active by updex: first a legacy `CurrentSymlink`, then an image name under each `MergedHierarchies` entry's `.systemd-sysext` metadata, then an image name in `RunExtensionsDir` (`/run/extensions`, the runtime image search directory)
- `GetActiveVersionWith(t *config.Transfer, p ActiveStatePaths) (string, error)` — Explicit-path variant used by `updex.Client`; `p.Hierarchies` are the hierarchies whose `.systemd-sysext/origin` JSON records merged images. An `origin` entry match returns the version from the recorded source path. When `origin` is absent, malformed, or does not name the image, the older `.systemd-sysext/extensions` name list is also checked, and a match there returns `UnknownVersion` (`"unknown"`) because it records no version. A read error returns `read merged state: …`
- `ActiveStatePaths` — `DefaultDir` (fallback for `Target.Path`; used only to locate a legacy `CurrentSymlink`), `RunExtensionsDir` (image search directory; empty skips the check), `Hierarchies` (merged-state hierarchies; empty skips the check)
- `GetActiveVersionIn(t *config.Transfer, defaultDir, runExtensionsDir string) (string, error)` — Compatibility variant with explicit directories that does not read hierarchy metadata; the sysext link directory (`/var/lib/extensions`) is only the fallback for locating a legacy `CurrentSymlink`, not evidence that an image is merged
- `MergedHierarchies` — Package variable: production default merged-state hierarchies `/usr` and `/opt`; inject a client's own value through `updex.RuntimePaths.SysextHierarchies`
- `MergedStateDirName` (`".systemd-sysext"`) and `UnknownVersion` (`"unknown"`) — Production constants for the per-hierarchy metadata directory and for a merged image whose metadata records no version
- `SysextLinkName(t *config.Transfer) string` — Derive the sysext-visible link name from `Transfer.Component` plus the target pattern extension after stripping compression suffixes, e.g. `foo.transfer` and `foo_@v.raw.xz` produce `foo.raw`
- `RemoveLegacyCurrentSymlink(t *config.Transfer) error` — Remove a staging `CurrentSymlink` only when the transfer defines one; absent directives and missing symlink files are no-ops
- `LinkToSysext(t *config.Transfer) / UnlinkFromSysext(t *config.Transfer)` — Manage `/var/lib/extensions/<component>.<ext>` symlinks without requiring `CurrentSymlink`. `LinkToSysext` scans staged versioned files, selects the newest by `version.Compare`, and points the sysext-visible link at that file
- `VacuumPaths` — The places the vacuum reads: `DefaultDir` (fallback for `Target.Path`; locates a legacy `CurrentSymlink`), `LinkDir` (the systemd-sysext link directory; the version its link names is kept; empty skips), `Hierarchies` (merged-state hierarchies; the merged version is kept, `UnknownVersion` protects nothing; empty skips) and `ProtectVersions` (extra versions to keep, for example a version a rollback rejected)
- `VacuumWith(t *config.Transfer, p VacuumPaths) (removed, kept []string, err error)` — Remove old staged images while keeping the linked version, the merged version, `p.ProtectVersions`, `ProtectVersion=`, the legacy `CurrentSymlink` target and the newest `InstancesMax` (default 2). Protected versions are kept in addition to the newest `InstancesMax` (one among the newest counts as one of them). Fails without deleting anything when the link or the merged state cannot be read; a dangling link protects nothing. `updex.Client` calls it with its captured `SysextLinkDir` and `SysextHierarchies`
- `PlanVacuumWith(t *config.Transfer, activeVersion string, p VacuumPaths) (removed, kept []string, err error)` — The same decision without deleting files; `activeVersion` is always kept and counts as installed even before its download (dry run)
- `PlanVacuumAfterInstall(t *config.Transfer, activeVersion string) ([]string, []string, error)` / `PlanVacuumAfterInstallAt` — Compatibility wrappers over `PlanVacuumWith` that protect neither the linked nor the merged version
- `Vacuum(t *config.Transfer) / VacuumAt / VacuumWithDetails(t *config.Transfer)` — Compatibility wrappers over `VacuumWith` that keep the legacy `CurrentSymlink` target, `ProtectVersion` and the newest `InstancesMax`, but not the linked or the merged version; SDK callers use `VacuumWith`
- `RemoveAllVersions(t *config.Transfer) ([]string, error)` — Remove all versions and current symlink for a component
- `GetExtensionName(filename string) string` — Extract extension name from filename (strips version and compression suffixes)
- `SysextDir` — Package variable: `/var/lib/extensions`
- `RunExtensionsDir` — Production constant for systemd-sysext's runtime image search directory: `/run/extensions`. An image there is merged on the next refresh; it is not a record of current merged state

### `systemd`

- `NewManager() *Manager` — Create manager with default paths (`UnitPath` `/etc/systemd/system`, `RunUnitPath` `/run/systemd/system`, `PackagedUnitPath` `/usr/lib/systemd/system`)
- `NewTestManager(unitPath string, runner SystemctlRunner) *Manager` — Create manager with custom paths and runner for testing; `RunUnitPath` and `PackagedUnitPath` are empty (no runtime or packaged units), set the exported fields to test those paths
- `Manager.UnitDropInDirs() []string` / `UnitDropIns(unit string) []string` — The drop-in directories in priority order (`UnitPath`, `RunUnitPath`, `PackagedUnitPath`), and the `*.conf` drop-ins that systemd applies to a unit in apply order: the highest-priority directory wins for each file name, and the surviving files are sorted by name
- `Manager.PackagedExists(name) bool` / `PackagedUnitPaths(name) []string` — Look for the package-shipped timer (read-only; `os.Lstat`) and list the packaged timer and service paths
- `GenerateTimer(cfg *TimerConfig) string` — Generate systemd timer unit content
- `GenerateService(cfg *ServiceConfig) string` — Generate systemd service unit content; `ServiceConfig.Sandbox` appends the `SandboxDirectives` hardening block to `[Service]` (the daemon unit sets it; other callers keep the minimal unit); `Wants`/`After` (one line each, space-joined) and `Conditions` (whole `ConditionX=y` lines) follow `Description` in `[Unit]`; `Comment` (also on `TimerConfig`) is written first as `# <Comment>`; a config that sets none of these produces the same text as before
- `Manager.Install(ctx, timer, service) / Remove(ctx, name) / Exists(name)` — Unit-file lifecycle; `Install`/`Remove` take `ctx` first (`Exists` is a local `os.Lstat` check and does not)
- `Manager.Enable(ctx, unit) / Start(ctx, unit) / Stop(ctx, unit) / Disable(ctx, unit) / DaemonReload(ctx) / IsEnabled(ctx, unit) / IsActive(ctx, unit)` — Runner-backed primitives used by the daemon SDK orchestration; each checks `ctx` before touching the runner, then dispatches to the runner's `ContextSystemctlRunner` method when it implements that interface, else the legacy `SystemctlRunner` method
- `SystemctlRunner` interface — `DaemonReload()`, `Enable(unit)`, `Disable(unit)`, `Start(unit)`, `Stop(unit)`, `IsActive(unit)`, `IsEnabled(unit)` methods executed via `DefaultSystemctlRunner` (real commands) or `MockSystemctlRunner` (tests); no in-flight cancellation
- `ShowRunner` interface / `Manager.Show(ctx, unit, property) (string, error)` — Optional runner interface `ShowContext(ctx, unit, property)` (`systemctl show -p PROPERTY --value UNIT`, trimmed, one property per call); `Manager.Show` returns `ErrShowUnsupported` for a runner without it. `DefaultSystemctlRunner` and `MockSystemctlRunner` (`ShowValues`, `ShowErr`, `ShowCalls`) implement it
- `ContextSystemctlRunner` interface — optional context-aware counterpart (`DaemonReloadContext(ctx)`, `EnableContext(ctx, unit)`, `DisableContext(ctx, unit)`, `StartContext(ctx, unit)`, `StopContext(ctx, unit)`, `IsActiveContext(ctx, unit)`, `IsEnabledContext(ctx, unit)`); `DefaultSystemctlRunner` implements it via `exec.CommandContext`, so an in-flight `systemctl` command is killed and the call returns `ctx.Err()` (matching `context.Canceled`/`context.DeadlineExceeded` via `errors.Is`) as soon as `ctx` ends — its legacy methods delegate to the `*Context` ones with `context.Background()`. `MockSystemctlRunner` deliberately does not implement it, pinning the legacy fallback path; `MockContextSystemctlRunner` does, for testing the context-aware dispatch path

### `imagecompat`

Reads a sysext image's `extension-release` file and decides the minimum-image-version rule of ADR-0016. It has no CLI imports and does not mount anything: the image is untrusted input, so it is parsed in user space before any decision to link it.

- `ReadExtensionRelease(ctx, path, name string, opts ReadOptions) (map[string]string, error)` — Parse `usr/lib/extension-release.d/extension-release.*` from the image at `path` (a GPT disk image with 512 or 4096-byte sectors, or a bare image). `path` must be a regular file (symlinks refused). Candidate regions are tried in table order, and a region is an EROFS candidate only when its superblock at offset 1024 starts with the EROFS magic `0xE0F5E1E2`; the readers are not called for a region without it. An uncompressed candidate is read with go-erofs. A compressed candidate — the superblock's `feature_incompat` (byte 80) has `LZ4_0PADDING`/`COMPR_CFGS` or `available_compr_algs` (byte 84) is set, which go-erofs refuses — is read with `dump.erofs` through `ReadOptions.ErofsDumpRunner`, without mounting. Both readers use the same selection: the only regular `extension-release.*` file, else the one named `extension-release.<name>`; with several EROFS candidates, the first that gives a release file wins. Errors: `ErrUnsupportedImage` (**no EROFS filesystem found at all**, for example a squashfs sysext, random bytes, or an invalid GPT), `ErrUnreadableRelease` (the image has an EROFS filesystem but the release file cannot be read: a failed EROFS open, a failed open, stat or read of the file, a recovered parser panic, a failed or refused `dump.erofs` run — callers must fail closed), `ErrErofsToolUnavailable` (the fallback cannot run: `dump.erofs` is missing, or older than `ErofsUtilsMinVersion`; it wraps `ErrUnreadableRelease`), `ErrNoExtensionRelease`, `ErrAmbiguousExtensionRelease`, `ErrReleaseFileTooLarge` (over `MaxReleaseFileSize`, 64 KiB). All are matched with `errors.Is`, and only `ErrUnsupportedImage` means "skip the check"
- `NextBootImageVersion(ctx, Options) (version string, Source, error)` — `IMAGE_VERSION` of the image that boots next. `SourceRollback` when `bootc status --json` reports `.status.rollbackQueued: true`: the next boot is the rollback deployment, so `.status.rollback.image.version` wins; a queued rollback without that version is an error. Else `SourceStaged` when the output has a `.status.staged` deployment; a staged deployment without `.image.version` is an error that wraps `ErrStagedWithoutVersion` (fail closed: bootc fills the version from the OCI manifest annotation `org.opencontainers.image.version`, and the booted os-release can be newer than the staged image). Else `SourceBootedOSRelease` (bootc missing with `exec.ErrNotFound`, or no staged deployment: `.status.staged` null or absent). An empty version with a nil error means unknown. A bootc that fails, prints more than `MaxBootcOutput` (1 MiB), or prints bad JSON returns an error (no fallback)
- `ReadOptions{ErofsDumpRunner ErofsDumpRunner}` — the compressed-image reader; nil uses `DefaultErofsDumpRunner` (the client passes `ClientConfig.ErofsDumpRunner`, one runner per client)
- `ErofsDumpRunner` interface — `Version(ctx) (string, error)` (the erofs-utils version of `dump.erofs`; an error wrapping `exec.ErrNotFound` when it is not installed), `ReadFile(ctx, image string, offset int64, path string, maxSize int) ([]byte, error)` and `ListDir(ctx, image string, offset int64, path string) ([]ErofsDirEntry, error)`. Every failure wraps `ErrUnreadableRelease`; `ReadFile` wraps `ErrReleaseFileTooLarge` over `maxSize`. `DefaultErofsDumpRunner` runs the real `dump.erofs` found in `PATH` with `exec.CommandContext`, no shell, a minimal environment (`PATH`, `LC_ALL=C`), 64 KiB for `--cat` (the `maxSize`), `MaxErofsDumpListSize` (256 KiB) for `--ls`, and 4 KiB for `-V`; a line of its stderr that starts with `<E>` is an error even when it exits 0
- `ErofsUtilsMinVersion` — `"1.8.5"`, the oldest erofs-utils whose `dump.erofs` has `--cat` (1.8.5; `--offset` and `--ls` came in 1.8). The fallback runs `dump.erofs -V` before its first use and fails closed below it
- `Options{Runner BootcRunner, OSReleasePaths []string}` — `Runner` nil uses `DefaultBootcRunner`; empty `OSReleasePaths` uses `DefaultOSReleasePaths` (the client passes `RuntimePaths.OSReleasePaths`)
- `BootedImageVersion(paths []string) string` — `IMAGE_VERSION` from the first readable os-release file of `paths` (empty: `DefaultOSReleasePaths`), or `""`. This is the running image: on a bootc host the booted deployment's. It runs no command, and the refresh gate compares with it.
- `BootcRunner` interface — `Status(ctx) ([]byte, error)`, the output of `bootc status --json`; tests use a fake
- `MinImageVersionKey` — `"UPDEX_MIN_IMAGE_VERSION"`; `MinImageVersion(release) (value string, set bool)` reads it
- `CheckMinImageVersion(release map[string]string, imageVersion string) (ok bool, reason string)` — Key absent: ok. Key set: both values must be digits only (`^[0-9]+$`) and are compared as numbers of any length; otherwise, or when the image is older, `ok` is false and `reason` is `needs image >= <value>`
- `ParseOSRelease(data []byte) map[string]string` — os-release style parser (quotes, escapes, comments)
