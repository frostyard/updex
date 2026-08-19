# updex

A Go library (SDK) and CLI tool for managing systemd-sysext images, replicating the functionality of `systemd-sysupdate` for `url-file` transfers.

[![Tests](https://github.com/frostyard/updex/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/frostyard/updex/actions/workflows/test.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/frostyard/updex/graph/badge.svg?branch=main)](https://codecov.io/gh/frostyard/updex)

See the [quality loop](docs/design/quality-loop.md) (quality dashboard) and
[public metrics index](docs/metrics/README.md) for the project's live quality,
review, and automation signals.

## What is updex?

**updex** provides two ways to manage system extensions:

1. **Go Library (SDK)**: Import `github.com/frostyard/updex/updex` in your Go applications for programmatic control
2. **CLI Tool**: Use the `updex` command-line tool as a thin wrapper around the SDK

Designed for systems like Debian Trixie that don't ship with `systemd-sysupdate`.

## Features

- Feature-based management of sysext images (enable/disable groups of transfers)
- systemd-sysupdate "component" discovery (`sysupdate.<name>.d/`, see sysupdate.d(5) "Components"), with the legacy default `sysupdate.d/` directory folded into the same domain
- Catalog integration (`updex catalog list/search/add/remove`) for one-command installs from sysext catalogs like [fedora-sysexts](https://fedora-sysexts.github.io/)
- Download sysext images from remote HTTP sources
- SHA256 hash verification via size-bounded `SHA256SUMS` manifests
- Bounded retry with exponential backoff for transient network failures and HTTP 5xx/429 responses
- GPG signature verification by default, matching systemd-sysupdate (`Verify=no` opts out)
- Automatic decompression (xz, gz, zstd)
- Version management with configurable retention (`InstancesMax`)
- Automatic update daemon via systemd timers
- Compatible with standard `.transfer` and `.feature` configuration files
- JSON output for scripting (`--json`)

## Installation

### Install a release

Download the latest CLI package for your system from the
[GitHub releases page](https://github.com/frostyard/updex/releases/latest):

| System                    | Release artifact                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------- |
| Debian/Ubuntu             | `frostyard-updex_<version>_amd64.deb` or `frostyard-updex_<version>_arm64.deb`        |
| Fedora/RHEL               | `frostyard-updex-<version>-1.x86_64.rpm` or `frostyard-updex-<version>-1.aarch64.rpm` |
| Alpine                    | `frostyard-updex_<version>_x86_64.apk` or `frostyard-updex_<version>_aarch64.apk`     |
| Other Linux distributions | `updex_<version>_linux_amd64.tar.gz` or `updex_<version>_linux_arm64.tar.gz`          |

Download `checksums.txt` from the same release and verify the package before
installing it:

```bash
sha256sum --ignore-missing --check checksums.txt
gh attestation verify <downloaded-artifact> --repo frostyard/updex
```

The checksum detects a corrupt or truncated download; the attestation
(GitHub build provenance, attached by the release workflow) binds the artifact
to a tag release built by `frostyard/updex`'s own workflow, so a file that
merely matches a `checksums.txt` served from the same place isn't enough.
`gh attestation verify` needs the [GitHub CLI](https://cli.github.com/) and
works on `checksums.txt` too.

Running the packaged CLI does not require Go or `make`. It requires a
systemd-based Linux system with `systemd-sysext`; operations that modify system
state also require root privileges.

### Build from source

Building the CLI or library and running unit tests requires
[Go 1.26.6 or newer](https://go.dev/doc/install) (the version required by
`go.mod`) and `make` for the Makefile commands below. Building and testing do
not require systemd.

```bash
make build

# Install to GOPATH/bin
make install
```

### As a Library

Using updex as a Go library requires Go 1.26.6 or newer:

```bash
go get github.com/frostyard/updex/updex
```

## Library (SDK) Usage

Create a module for the example:

```bash
mkdir updex-quickstart
cd updex-quickstart
go mod init example.com/updex-quickstart
go get github.com/frostyard/updex/updex
```

Save the following as `main.go`. The SDK is built around a `Client` struct
that provides all operations:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/frostyard/updex/updex"
)

func main() {
    client := updex.NewClient(updex.ClientConfig{
        Definitions: "/etc/sysupdate.d",
        Verify:      true,
    })

    ctx := context.Background()

    // List all features (union of the legacy default directory and every
    // discovered systemd-sysupdate component). opts is variadic: omit it
    // for the default domain, or pass updex.FeaturesOptions{Component: "docker"}
    // to scope to one component.
    features, err := client.Features(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, f := range features {
        fmt.Printf("%s: enabled=%v (%s)\n", f.Name, f.Enabled, f.Description)
    }

    // List discovered components
    components, err := client.Components(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, c := range components {
        fmt.Printf("%s: %s (%d features)\n", c.Name, c.SourceDir, c.FeatureCount)
    }

    // Inspect the reusable automatic-update daemon lifecycle.
    daemon, err := client.DaemonStatus(ctx, updex.DaemonStatusOptions{})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("daemon installed=%v active=%v\n", daemon.Installed, daemon.Active)

    // Enable a feature and download extensions immediately
    result, err := client.EnableFeature(ctx, "docker", updex.EnableFeatureOptions{
        Now: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.NextActionMessage)

    // Check for available updates. A non-nil error means at least one
    // component could not be checked; the returned results are still
    // populated, with CheckResult.Error set on the failed components.
    checks, err := client.CheckFeatures(ctx, updex.CheckFeaturesOptions{})
    if err != nil {
        log.Printf("check incomplete: %v", err)
    }
    for _, fc := range checks {
        for _, c := range fc.Results {
            if c.Error != "" {
                fmt.Printf("%s: could not check: %s\n", c.Component, c.Error)
                continue
            }
            if c.UpdateAvailable {
                fmt.Printf("%s: %s → %s\n", c.Component, c.CurrentVersion, c.NewestVersion)
            }
        }
    }

    // Update all enabled features
    updates, err := client.UpdateFeatures(ctx, updex.UpdateFeaturesOptions{})
    if err != nil {
        log.Fatal(err)
    }
    for _, fu := range updates {
        for _, u := range fu.Results {
            fmt.Printf("%s: version %s (downloaded=%v)\n", u.Component, u.Version, u.Downloaded)
        }
    }

    // Disable a feature
    _, err = client.DisableFeature(ctx, "docker", updex.DisableFeatureOptions{
        Now:   true,
        Force: true,
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

Run the example from the module directory:

```bash
go run .
```

The enable, update, and disable calls change system state. Run the complete
example only on a configured test system with permission to manage system
extensions.

### Client Methods

| Method           | Signature                                                                        | Description                                                                          |
| ---------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `Features`       | `Features(ctx, opts ...FeaturesOptions) ([]FeatureInfo, error)`                  | List all features with status and associated transfers                               |
| `EnableFeature`  | `EnableFeature(ctx, name, EnableFeatureOptions) (*FeatureActionResult, error)`   | Enable a feature via drop-in config                                                  |
| `DisableFeature` | `DisableFeature(ctx, name, DisableFeatureOptions) (*FeatureActionResult, error)` | Disable a feature via drop-in config                                                 |
| `UpdateFeatures` | `UpdateFeatures(ctx, UpdateFeaturesOptions) ([]UpdateFeaturesResult, error)`     | Download and install newest versions for all enabled features                        |
| `CheckFeatures`  | `CheckFeatures(ctx, CheckFeaturesOptions) ([]CheckFeaturesResult, error)`        | Check if newer versions are available                                                |
| `Components`     | `Components(ctx) ([]ComponentInfo, error)`                                       | List discovered systemd-sysupdate components (name, source directory, feature count) |
| `CatalogList`    | `CatalogList(ctx, CatalogListOptions) ([]CatalogEntry, error)`                   | Enumerate sysexts available from configured catalogs                                 |
| `CatalogAdd`     | `CatalogAdd(ctx, name, CatalogAddOptions) (*CatalogAddResult, error)`            | Install a sysext from a catalog (write definitions, enable, download)                |
| `CatalogRemove`  | `CatalogRemove(ctx, name, CatalogRemoveOptions) (*CatalogRemoveResult, error)`   | Remove a catalog-added sysext and its generated definitions                          |
| `EnableDaemon`   | `EnableDaemon(ctx, EnableDaemonOptions) (*DaemonActionResult, error)`            | Install, enable, and start the automatic-update timer                                |
| `DisableDaemon`  | `DisableDaemon(ctx, DisableDaemonOptions) (*DaemonActionResult, error)`          | Stop, disable, and remove the automatic-update timer                                 |
| `DaemonStatus`   | `DaemonStatus(ctx, DaemonStatusOptions) (*DaemonStatusResult, error)`             | Inspect installed, enabled, active, and schedule state                               |

`FeaturesOptions`, `EnableFeatureOptions`, `DisableFeatureOptions`, `UpdateFeaturesOptions`, and `CheckFeaturesOptions` all carry a `Component string` field that scopes the operation to a single named systemd-sysupdate component instead of the default union domain (see "systemd-sysupdate Components" below). It cannot be combined with a `Definitions` override on `ClientConfig`.

### ClientConfig

```go
type ClientConfig struct {
    Definitions        string                // Custom path to .transfer/.feature files (default: standard paths)
    Verify             bool                  // Enable GPG signature verification
    Verbose            bool                  // Enable debug-level output
    Progress           reporter.Reporter     // Optional progress reporter
    SysextRunner       sysext.SysextRunner   // Optional mock runner for testing
    SystemdManager     *systemd.Manager       // Optional unit manager and runner for daemon operations
    OnDownloadProgress download.ProgressFunc // Optional download progress callback
    HTTPClient         *http.Client          // Optional shared HTTP client
    Paths              RuntimePaths          // Optional instance-scoped filesystem paths (see below)
}

// RuntimePaths holds the filesystem paths an updex.Client consults at runtime.
// Zero values resolve to current production defaults at NewClient time.
// Use this to give two clients different filesystem trees in one process.
type RuntimePaths struct {
    DefinitionRoots    []string // Roots for sysupdate.d directories
    OSReleasePaths     []string // os-release files for specifier expansion and image naming
    CatalogConfigRoots []string // Dirs scanned for *.catalog repo definitions
    CatalogCacheDir    string   // Cache dir for catalog listings; "" = default user cache; DisableCatalogCache = off
    CatalogTargetPath  string   // Trusted staging dir for catalog transfer files
    SysextLinkDir      string   // Dir where systemd-sysext looks for extension images
    RunExtensionsDir   string   // Dir containing images merged by systemd-sysext; default /run/extensions
}
```

`NewClient` captures `Paths` once at construction: zero fields resolve to production defaults by reading each package-level compatibility variable at that moment. After construction the client is not affected by later mutations to those variables, so two clients with different `Paths` can safely coexist in one process.

### Option Structs

```go
type FeaturesOptions struct {
    Component string // Scope to a single named component (default: union of all)
}

type EnableFeatureOptions struct {
    Now       bool   // Immediately download extensions after enabling
    DryRun    bool   // Preview changes without modifying filesystem
    NoRefresh bool   // Skip systemd-sysext refresh after download
    Component string // Scope to a single named component (default: union of all)
}

type DisableFeatureOptions struct {
    Now       bool   // Immediately unmerge and remove extension files
    Force     bool   // Allow removal of merged extensions (requires reboot)
    DryRun    bool   // Preview changes without modifying filesystem
    NoRefresh bool   // Skip systemd-sysext refresh
    Component string // Scope to a single named component (default: union of all)
}

type UpdateFeaturesOptions struct {
    DryRun    bool   // Preview changes without modifying filesystem or sysext state
    NoRefresh bool   // Skip systemd-sysext refresh after update
    NoVacuum  bool   // Skip removing old versions after update
    Component string // Scope to a single named component (default: union of all)
}

type CheckFeaturesOptions struct {
    Component string // Scope to a single named component (default: union of all)
}
```

## CLI Usage

```bash
# List all features, including where each one came from
updex features list
# FEATURE   DESCRIPTION       ENABLED  CATALOG      TRANSFERS
# docker    Docker CE         yes      image:ucore  docker
# zoxide    zoxide sysext     yes      fedora       zoxide
# mytool    Hand-written      no       local:etc    mytool

# Enable a feature (downloads on next update)
sudo updex features enable docker

# Enable and download immediately
sudo updex features enable docker --now

# Disable a feature (stops future updates)
sudo updex features disable docker

# Disable and remove files immediately
sudo updex features disable docker --now

# Force removal of merged extensions
sudo updex features disable docker --now --force

# Update all enabled features
sudo updex features update

# Update without removing old versions
sudo updex features update --no-vacuum

# Preview downloads, installs, refreshes, and vacuum removals
sudo updex --dry-run features update

# If the closing `systemd-sysext refresh` fails after enable --now, disable --now,
# or update, the command reports what it did, prints "Error: sysext refresh
# failed: ..." with the next step (a manual `systemd-sysext refresh` or a reboot),
# and exits non-zero; JSON results carry "refresh_error". After disable --now the
# host's extensions stay unmerged until that refresh happens.

# Check for available updates (read-only). A component whose manifest cannot be
# fetched or verified is listed with UPDATE=error (JSON: "error" set) and the
# command exits non-zero; healthy components in the same run are still reported.
updex features check

# Scope any of the above to a single named component
updex features list --component=docker
sudo updex features update --component=docker

# List discovered systemd-sysupdate components
updex components

# Browse configured sysext catalogs (see "Sysext Catalogs" below)
updex catalog list
updex catalog search zoxide

# Install a sysext from a catalog (writes definitions, enables, downloads)
sudo updex catalog add fedora/zoxide

# Remove it again (definitions, images, and links)
sudo updex catalog remove zoxide

# Enable automatic daily updates
sudo updex daemon enable

# Check auto-update status
updex daemon status

# Disable automatic updates
sudo updex daemon disable
```

### Global Flags

| Flag                | Description                                               |
| ------------------- | --------------------------------------------------------- |
| `-C, --definitions` | Path to directory containing .transfer and .feature files |
| `--verify`          | Verify GPG signatures on SHA256SUMS                       |
| `--no-refresh`      | Skip running systemd-sysext refresh after install/update  |
| `--json`            | Output in JSON format (jq-compatible)                     |
| `-n, --dry-run`     | Preview changes without modifying filesystem              |
| `-v, --verbose`     | Enable verbose output                                     |
| `-s, --silent`      | Suppress all progress output (takes priority over `--json`) |

### `features` Flags

| Flag          | Description                                                                                                                                                                                                 |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--component` | Scope the operation to a single named systemd-sysupdate component instead of the default union of the legacy default directory and every discovered component. Cannot be combined with `-C, --definitions`. |

## Configuration

By default updex reads `.transfer` and `.feature` files from the union of two kinds of sources:

**The legacy default directories** (in priority order):

1. `/etc/sysupdate.d/` (highest priority)
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

**Every discovered systemd-sysupdate component** — a named grouping of `.transfer`/`.feature` files under a `sysupdate.<name>.d/` directory (see sysupdate.d(5) "Components"), searched across the same four roots with the same priority order, e.g. `/etc/sysupdate.docker.d/` overrides `/usr/lib/sysupdate.docker.d/`. Run `updex components` to see what's discovered.

Within each source, only the first occurrence of a given filename is used. Feature and transfer names are expected to be globally unique across the whole union (they're derived from distinct sysext names); if a name is defined in more than one source, the most specific source wins — a named component beats the legacy default directory — and the collision is logged as a warning.

Non-sysext transfers that may share the legacy default directory on native OS images (GPT `partition` targets for an A/B root, or the UKI's `regular-file` target relative to the ESP) are silently skipped rather than erroring.

The `-C, --definitions` flag overrides all of the above with a single explicit directory (no component discovery, no union) — the original, pre-component behavior. It cannot be combined with `--component`. The `--component=<name>` flag instead scopes the read/write domain to one named component's own search paths.

### Example Transfer File

Create `/etc/sysupdate.d/myext.transfer`:

```ini
[Transfer]
MinVersion=1.0.0
InstancesMax=3
Verify=yes

[Source]
Type=url-file
Path=https://example.com/sysexts
MatchPattern=myext_@v.raw.xz

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=myext_@v.raw
Mode=0644
```

### Configuration Options

#### [Transfer] Section

| Option              | Description                                        | Default |
| ------------------- | -------------------------------------------------- | ------- |
| `MinVersion`        | Minimum version to consider                        | (none)  |
| `ProtectVersion`    | Version to never remove (supports `%A` specifiers) | (none)  |
| `Verify`            | Verify GPG signatures                              | `yes`   |
| `InstancesMax`      | Maximum versions to keep                           | `2`     |
| `Features`          | Space-separated feature names (OR logic)           | (none)  |
| `RequisiteFeatures` | Space-separated feature names (AND logic)          | (none)  |

Omitting `Verify=` enables signature verification, matching systemd-sysupdate. Set `Verify=no` explicitly to disable it; the global `--verify` flag forces verification even for transfers that opt out.

#### [Source] Section

| Option         | Description                                    |
| -------------- | ---------------------------------------------- |
| `Type`         | Must be `url-file`                             |
| `Path`         | Base URL containing SHA256SUMS and image files |
| `MatchPattern` | Filename pattern with `@v` version placeholder |

#### [Target] Section

| Option           | Description                                                                      | Default                 |
| ---------------- | -------------------------------------------------------------------------------- | ----------------------- |
| `Type`           | Must be `regular-file`                                                           | -                       |
| `Path`           | Target staging directory for downloaded versions                                 | `/var/lib/extensions.d` |
| `MatchPattern`   | Output filename pattern with `@v`                                                | -                       |
| `CurrentSymlink` | Optional legacy staging symlink name; if present, updex removes it during update | (none)                  |
| `Mode`           | File permissions (octal)                                                         | `0644`                  |

### Version Patterns

The `@v` placeholder matches version strings in filenames:

```
myext_@v.raw.xz     →  matches myext_1.2.3.raw.xz, myext_2.0.0-rc1.raw.xz
kernel_@v.efi       →  matches kernel_6.1.0.efi
```

## Optional Features

Optional features allow grouping transfers that can be enabled or disabled together. This is useful for optional system components like development tools or proprietary drivers.

Features are defined in `.feature` files in the same directories as `.transfer` files.

### Example Feature File

Create `/usr/lib/sysupdate.d/devel.feature`:

```ini
[Feature]
Description=Development Tools
Documentation=https://example.com/docs/devel
Enabled=false
```

### Associating Transfers with Features

Add `Features=` to a transfer file to associate it with a feature:

```ini
[Transfer]
Features=devel
InstancesMax=2

[Source]
Type=url-file
Path=https://example.com/sysexts
MatchPattern=devel-tools_@v.raw.xz

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=devel-tools_@v.raw
```

Transfers with `Features=` are only active when at least one of the listed features is enabled (OR logic).

Use `RequisiteFeatures=` when ALL listed features must be enabled (AND logic).

### Enabling Features

Features are enabled via drop-in configuration files:

```bash
# Using updex
sudo updex features enable devel

# Enable and download extensions immediately
sudo updex features enable devel --now

# Or manually create a drop-in
mkdir -p /etc/sysupdate.d/devel.feature.d
echo -e "[Feature]\nEnabled=true" > /etc/sysupdate.d/devel.feature.d/enable.conf
```

### Feature Configuration Options

| Option          | Description                        | Default |
| --------------- | ---------------------------------- | ------- |
| `Description`   | Human-readable feature description | (none)  |
| `Documentation` | URL to feature documentation       | (none)  |
| `AppStream`     | URL to AppStream catalog XML       | (none)  |
| `Enabled`       | Whether the feature is enabled     | `false` |

### Masking Features

To completely hide a feature, create a symlink to `/dev/null`:

```bash
ln -s /dev/null /etc/sysupdate.d/devel.feature
```

The same `/dev/null` symlink idiom masks a `.transfer` file.

## systemd-sysupdate Components

A component is a named grouping of `.transfer`/`.feature` files, used to give a
sysext its own versioning scope separate from the shared default directory
(see sysupdate.d(5) "Components"). Move a sysext's files out of
`/usr/lib/sysupdate.d/` into `/usr/lib/sysupdate.<name>.d/` and updex picks
them up automatically as component `<name>`, with the same
`/etc` > `/run` > `/usr/local/lib` > `/usr/lib` override precedence used for
the legacy default directory:

```
/usr/lib/sysupdate.docker.d/docker.transfer
/usr/lib/sysupdate.docker.d/docker.feature
```

List what's discovered:

```bash
updex components
# COMPONENT  SOURCE                          FEATURES
# docker     /usr/lib/sysupdate.docker.d     1
# incus      /usr/lib/sysupdate.incus.d      1
```

`updex features list` (and every other `features` subcommand) reads the union
of the legacy default directory and every discovered component by default;
pass `--component=<name>` to scope to just one. Enabling or disabling a
feature writes its drop-in under the matching scope — a component-scoped
feature gets `/etc/sysupdate.<name>.d/<feature>.feature.d/00-updex.conf`, a
legacy default (or `-C`/`--definitions`-loaded) feature keeps
`/etc/sysupdate.d/<feature>.feature.d/00-updex.conf` — so reads and writes
always agree on where a feature's overrides live.

## Sysext Catalogs

`updex catalog` installs sysexts published by catalogs such as
[fedora-sysexts](https://fedora-sysexts.github.io/), which serves prebuilt
extensions for Fedora image-based systems (CoreOS/ucore, Silverblue,
Kinoite) from GitHub releases behind stable URLs.

updex ships no built-in catalogs — they only apply to specific systems —
so configure them with `<name>.catalog` INI files searched across
`/etc/updex/catalogs.d/`, `/run/updex/catalogs.d/`,
`/usr/local/lib/updex/catalogs.d/`, and `/usr/lib/updex/catalogs.d/`
(earlier directories win per filename). For fedora-sysexts, copy these two
files:

```ini
# /etc/updex/catalogs.d/fedora.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/fedora
ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/
# AllowInsecure=no
```

```ini
# /etc/updex/catalogs.d/community.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/community
ListURL=https://api.github.com/repos/fedora-sysexts/community/contents/
# AllowInsecure=no
```

- `SiteURL` (required) — base URL the catalog serves artifacts from; the
  published `<sysext>.conf`, `SHA256SUMS`, and `.raw` images all resolve
  beneath `<SiteURL>/<sysext>/`. Must use HTTPS unless `AllowInsecure=yes`.
- `ListURL` (optional) — GitHub contents API endpoint used by
  `catalog list`/`search` to enumerate available sysexts. `add`/`remove`
  never use it. Set the `GITHUB_TOKEN` environment variable to raise the
  API rate limit for `https://api.github.com`; credentials are not sent to
  custom catalog origins, cleartext URLs, or cross-origin redirects. Must
  use HTTPS unless `AllowInsecure=yes`.
- `Component` (optional) — systemd-sysupdate component the generated files
  are written under; defaults to `catalog-<name>`
  (e.g. `/etc/sysupdate.catalog-fedora.d/`).
- `AllowInsecure` (optional, default `no`) — permits non-HTTPS `SiteURL` and
  `ListURL` values only for explicitly trusted development and test
  endpoints. It does not permit `GITHUB_TOKEN` transmission to cleartext or
  custom origins.

Existing catalog files with `http://` URLs are a breaking configuration
change: they now fail to load until `AllowInsecure=yes` is added. Production
catalogs should migrate to HTTPS instead of enabling the escape hatch.

The SDK's default HTTP client also refuses redirects from HTTPS to HTTP, so a
catalog cannot pass initial URL validation and then downgrade `.conf`,
`SHA256SUMS`, image, or listing requests to cleartext. A caller-supplied
`ClientConfig.HTTPClient` retains its own redirect policy.

`sudo updex catalog add fedora/zoxide` fetches the catalog's published
transfer definition, writes a standard `.transfer` (with `Features=zoxide`
injected and security-sensitive source/target fields canonicalized) plus a
generated `.feature` into the catalog's component directory, enables the
feature, and downloads the image. Catalog transfers must use a `url-file`
source at the configured `<SiteURL>/<sysext>/` path and unquoted,
basename-only match patterns. Their target is always a regular `0644` file under the trusted
staging path (default `/var/lib/extensions.d`, configurable per client via
`RuntimePaths.CatalogTargetPath`); catalog-provided target paths, modes,
`PathRelativeTo`, and `CurrentSymlink` values cannot redirect root-owned
writes. updex manages the `/var/lib/extensions` link itself (configurable
per client via `RuntimePaths.SysextLinkDir`). The `%w`/`%a`
specifiers in the catalog's match patterns are kept unexpanded, so updates
keep tracking the running Fedora release across OS upgrades.

From then on the sysext is a completely normal feature: `updex features
list/enable/disable/update/check` and the update daemon manage it like any
hand-written one. The CATALOG column of `updex features list` still shows
which catalog it came from, so catalog-added sysexts stay distinguishable
from image-shipped (`image:<id>`) and hand-written (`local:etc`) ones. `sudo updex catalog remove zoxide` reverses the add —
disable, unmerge, delete images and the extensions link, and delete the
generated definition files (`--force` required while the extension is
merged, as with `features disable --now`).

Generated files carry a `# Generated by updex catalog (repo: <name>)`
header as an ownership marker, and the repo it names is part of the
check: `catalog add` refuses to overwrite definitions it did not generate
or that another catalog generated, and `catalog remove` only touches
files marked by the repo it is acting for — so hand-written,
package-shipped, or other-catalog definitions sharing a name or component
are safe. A failed `add` restores the previous state exactly (a fresh add
leaves nothing behind, a re-add keeps its previously working files, even
if the failure happens mid-write). `remove` refuses up front if the
sysext's `.transfer` was replaced by one it doesn't own — rather than
tearing down images that definition describes — and deletes only updex's
own `00-updex.conf` drop-in, leaving any administrator drop-ins in
`<name>.feature.d` in place.

Sysexts are referenced as `NAME` or `REPO/NAME`; a bare `NAME` works
whenever it is unambiguous across the configured catalogs, and `--repo` is
equivalent to the `REPO/` prefix.

`catalog list`/`search` cache each repo's listing locally (in
`~/.cache/updex/`, or `/root/.cache/updex/` under sudo) for 60 minutes.
After the TTL the listing is revalidated with a conditional request — a
`304 Not Modified` from the GitHub API costs no rate limit and just
refreshes the cache. `--no-cache` forces a live query, and when a live
fetch fails (offline, rate-limited) an expired cache is served with a
warning so listing keeps working. `add`/`remove` never use the cache.

## Remote Manifest Format

The source URL must contain a `SHA256SUMS` file:

```
a1b2c3d4...  myext_1.0.0.raw.xz
e5f6g7h8...  myext_1.1.0.raw.xz
i9j0k1l2...  myext_1.2.0.raw.xz
```

For GPG verification, also provide `SHA256SUMS.gpg` (detached signature).

Fetching `SHA256SUMS` retries transient network failures and HTTP 5xx/429 responses with exponential backoff. Manifest responses are limited to 4 MiB and detached signature responses to 1 MiB; oversized responses are rejected before parsing or signature verification. The detached signature fetch is verified after the manifest body is fetched.

## JSON Output

Use `--json` for machine-readable output:

```bash
updex features list --json | jq '.[] | select(.enabled)'
updex features check --json

# Components that could not be checked carry an "error" field (and the
# command exits non-zero); do not read their absence of update_available as
# "up to date"
updex features check --json | jq '.[].results[] | select(.error != null)'

# Everything added from the fedora catalog
updex features list --json | jq '.[] | select(.origin=="catalog" and .origin_name=="fedora")'
```

The terminal download bar is suppressed in JSON mode, so stdout remains a
valid JSON stream that is safe to pipe directly into parsers such as `jq`.

## Development

### Architecture

updex follows an **SDK-first** architecture:

- **SDK Layer** (`updex/` package): All operations are implemented as methods on the `Client` struct
- **CLI Layer** (`cmd/` package): Thin Cobra wrappers that parse flags, call SDK methods, and format output

SDK conventions:

- All methods take `context.Context` as first parameter
- Operations use dedicated option structs (e.g., `EnableFeatureOptions`) for future extensibility
- Return dedicated result structs with status fields + error
- Error messages: lowercase, no trailing punctuation, wrapped with `fmt.Errorf`

When adding features:

1. Implement as a method on `Client` in `updex/*.go`
2. Create CLI wrapper in `cmd/updex/*.go`
3. CLI commands should only handle argument parsing and output formatting

### Build Commands

```bash
# Format code (always run after changes)
make fmt

# Run linters
make lint

# Run tests
make test

# Quick format, lint, and test loop
make check

# Run the credential-free gate that mirrors CI
make ci

# Build binaries
make build

# Clean build artifacts
make clean

# Run end-to-end tests (builds and runs the real updex binary)
go test -v ./tests/e2e/...
```

### End-to-End Tests

`tests/e2e/` contains black-box tests that build the real `updex` binary
and run it as a subprocess against a fake HTTP transfer source, the same
way an operator would invoke it. The suite covers help, version and shell
completion output; argument and exit-code handling for every command variant;
configuration errors; and text/JSON feature listing and update checks.
Successful operations are read-only so the tests run without root privileges.
Additional CLI integration tests in `cmd/updex/` use temporary search roots
and a fake catalog server to cover default component discovery and
`GITHUB_TOKEN` request authentication.

### Contributing

See [AGENTS.md](AGENTS.md) (`CONTRIBUTING.md` resolves to it) for the full
guide. In short:

- Keep the SDK layer free of CLI dependencies (no Cobra, pflag, etc.)
- SDK functions should return structured data, not formatted output
- CLI commands should be thin wrappers around SDK functions
- Write tests for both SDK and CLI layers
- Run `make ci` before submitting PRs

## License

MIT
