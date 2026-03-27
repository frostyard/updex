# updex

A Go library (SDK) and CLI tool for managing systemd-sysext images, replicating the functionality of `systemd-sysupdate` for `url-file` transfers.

## What is updex?

**updex** provides two ways to manage system extensions:

1. **Go Library (SDK)**: Import `github.com/frostyard/updex/updex` in your Go applications for programmatic control
2. **CLI Tool**: Use the `updex` command-line tool as a thin wrapper around the SDK

Designed for systems like Debian Trixie that don't ship with `systemd-sysupdate`.

## Features

- Feature-based management of sysext images (enable/disable groups of transfers)
- Download sysext images from remote HTTP sources
- SHA256 hash verification via `SHA256SUMS` manifests
- Optional GPG signature verification (`--verify`)
- Automatic decompression (xz, gz, zstd)
- Version management with configurable retention (`InstancesMax`)
- Automatic update daemon via systemd timers
- Compatible with standard `.transfer` and `.feature` configuration files
- JSON output for scripting (`--json`)

## Installation

### As a Library

```bash
go get github.com/frostyard/updex/updex
```

### As CLI Tools

```bash
# Build from source
make build

# Install to GOPATH/bin
make install
```

## Library (SDK) Usage

The SDK is built around a `Client` struct that provides all operations:

```go
import "github.com/frostyard/updex/updex"

func main() {
    client := updex.NewClient(updex.ClientConfig{
        Definitions: "/etc/sysupdate.d",
        Verify:      true,
    })

    ctx := context.Background()

    // List all features
    features, err := client.Features(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, f := range features {
        fmt.Printf("%s: enabled=%v (%s)\n", f.Name, f.Enabled, f.Description)
    }

    // Enable a feature and download extensions immediately
    result, err := client.EnableFeature(ctx, "docker", updex.EnableFeatureOptions{
        Now: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.NextActionMessage)

    // Check for available updates
    checks, err := client.CheckFeatures(ctx, updex.CheckFeaturesOptions{})
    if err != nil {
        log.Fatal(err)
    }
    for _, fc := range checks {
        for _, c := range fc.Results {
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

### Client Methods

| Method | Signature | Description |
| --- | --- | --- |
| `Features` | `Features(ctx) ([]FeatureInfo, error)` | List all features with status and associated transfers |
| `EnableFeature` | `EnableFeature(ctx, name, EnableFeatureOptions) (*FeatureActionResult, error)` | Enable a feature via drop-in config |
| `DisableFeature` | `DisableFeature(ctx, name, DisableFeatureOptions) (*FeatureActionResult, error)` | Disable a feature via drop-in config |
| `UpdateFeatures` | `UpdateFeatures(ctx, UpdateFeaturesOptions) ([]UpdateFeaturesResult, error)` | Download and install newest versions for all enabled features |
| `CheckFeatures` | `CheckFeatures(ctx, CheckFeaturesOptions) ([]CheckFeaturesResult, error)` | Check if newer versions are available |

### ClientConfig

```go
type ClientConfig struct {
    Definitions        string                // Custom path to .transfer/.feature files (default: standard paths)
    Verify             bool                  // Enable GPG signature verification
    Verbose            bool                  // Enable debug-level output
    Progress           reporter.Reporter     // Optional progress reporter
    SysextRunner       sysext.SysextRunner   // Optional mock runner for testing
    OnDownloadProgress download.ProgressFunc // Optional download progress callback
    HTTPClient         *http.Client          // Optional shared HTTP client
}
```

### Option Structs

```go
type EnableFeatureOptions struct {
    Now       bool // Immediately download extensions after enabling
    DryRun    bool // Preview changes without modifying filesystem
    NoRefresh bool // Skip systemd-sysext refresh after download
}

type DisableFeatureOptions struct {
    Now       bool // Immediately unmerge and remove extension files
    Force     bool // Allow removal of merged extensions (requires reboot)
    DryRun    bool // Preview changes without modifying filesystem
    NoRefresh bool // Skip systemd-sysext refresh
}

type UpdateFeaturesOptions struct {
    NoRefresh bool // Skip systemd-sysext refresh after update
    NoVacuum  bool // Skip removing old versions after update
}

type CheckFeaturesOptions struct{}
```

## CLI Usage

```bash
# List all features
updex features list

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

# Check for available updates (read-only)
updex features check

# Enable automatic daily updates
sudo updex daemon enable

# Check auto-update status
updex daemon status

# Disable automatic updates
sudo updex daemon disable
```

### Global Flags

| Flag | Description |
| --- | --- |
| `-C, --definitions` | Path to directory containing .transfer and .feature files |
| `--verify` | Verify GPG signatures on SHA256SUMS |
| `--no-refresh` | Skip running systemd-sysext refresh after install/update |
| `--json` | Output in JSON format (jq-compatible) |
| `--dry-run` | Preview changes without modifying filesystem |
| `--verbose` | Enable verbose output |

## Configuration

updex reads `.transfer` and `.feature` files from these directories (in priority order):

1. `/etc/sysupdate.d/` (highest priority)
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

Only the first occurrence of a given filename is used. The `-C` flag overrides all search paths with a custom directory.

### Example Transfer File

Create `/etc/sysupdate.d/myext.transfer`:

```ini
[Transfer]
MinVersion=1.0.0
InstancesMax=3
Verify=no

[Source]
Type=url-file
Path=https://example.com/sysexts
MatchPattern=myext_@v.raw.xz

[Target]
Type=regular-file
Path=/var/lib/extensions
MatchPattern=myext_@v.raw
CurrentSymlink=myext.raw
Mode=0644
```

### Configuration Options

#### [Transfer] Section

| Option | Description | Default |
| --- | --- | --- |
| `MinVersion` | Minimum version to consider | (none) |
| `ProtectVersion` | Version to never remove (supports `%A` specifiers) | (none) |
| `Verify` | Verify GPG signatures | `no` |
| `InstancesMax` | Maximum versions to keep | `2` |
| `Features` | Space-separated feature names (OR logic) | (none) |
| `RequisiteFeatures` | Space-separated feature names (AND logic) | (none) |

#### [Source] Section

| Option | Description |
| --- | --- |
| `Type` | Must be `url-file` |
| `Path` | Base URL containing SHA256SUMS and image files |
| `MatchPattern` | Filename pattern with `@v` version placeholder |

#### [Target] Section

| Option | Description | Default |
| --- | --- | --- |
| `Type` | Must be `regular-file` | - |
| `Path` | Target directory | `/var/lib/extensions` |
| `MatchPattern` | Output filename pattern with `@v` | - |
| `CurrentSymlink` | Symlink name pointing to current version | (none) |
| `Mode` | File permissions (octal) | `0644` |

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
Path=/var/lib/extensions
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

| Option | Description | Default |
| --- | --- | --- |
| `Description` | Human-readable feature description | (none) |
| `Documentation` | URL to feature documentation | (none) |
| `AppStream` | URL to AppStream catalog XML | (none) |
| `Enabled` | Whether the feature is enabled | `false` |

### Masking Features

To completely hide a feature, create a symlink to `/dev/null`:

```bash
ln -s /dev/null /etc/sysupdate.d/devel.feature
```

## Remote Manifest Format

The source URL must contain a `SHA256SUMS` file:

```
a1b2c3d4...  myext_1.0.0.raw.xz
e5f6g7h8...  myext_1.1.0.raw.xz
i9j0k1l2...  myext_1.2.0.raw.xz
```

For GPG verification, also provide `SHA256SUMS.gpg` (detached signature).

## JSON Output

Use `--json` for machine-readable output:

```bash
updex features list --json | jq '.[] | select(.enabled)'
updex features check --json
```

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

# Format, lint, and test
make check

# Build binaries
make build

# Clean build artifacts
make clean
```

### Contributing

When contributing:

- Keep the SDK layer free of CLI dependencies (no Cobra, pflag, etc.)
- SDK functions should return structured data, not formatted output
- CLI commands should be thin wrappers around SDK functions
- Write tests for both SDK and CLI layers
- Run `make check` before submitting PRs

## License

MIT
