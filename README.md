# updex

A Go library (SDK) and CLI tool for managing systemd-sysext images, replicating the functionality of `systemd-sysupdate` for `url-file` transfers.

## What is updex?

**updex** provides two ways to manage system extensions:

1. **Go Library (SDK)**: Import `github.com/frostyard/updex/updex` in your Go applications for programmatic control
2. **CLI Tool**: Use the `updex` command-line tool as a thin wrapper around the SDK

Designed for systems like Debian Trixie that don't ship with `systemd-sysupdate`.

## Features

- Download sysext images from remote HTTP sources
- SHA256 hash verification via `SHA256SUMS` manifests
- Optional GPG signature verification (`--verify`)
- Automatic decompression (xz, gz, zstd)
- Version management with configurable retention (`InstancesMax`)
- Optional features support (enable/disable groups of transfers)
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

The `updex` binary provides all commands for both local management and remote discovery/installation.

## Library (SDK) Usage

Import updex in your Go applications:

```go
import "github.com/frostyard/updex/updex"

func main() {
    // Configure options
    opts := updex.Options{
        DefinitionsPath: "/etc/sysupdate.d",
        Component:       "myext",
        Verify:          true,
    }

    // List available versions
    versions, err := updex.List(opts)
    if err != nil {
        log.Fatal(err)
    }

    for _, v := range versions {
        fmt.Printf("%s: %s (installed: %v, current: %v)\n",
            v.Component, v.Version, v.Installed, v.Current)
    }

    // Check for updates
    hasUpdate, err := updex.CheckNew(opts)
    if err != nil {
        log.Fatal(err)
    }

    if hasUpdate {
        // Perform update
        results, err := updex.Update(opts)
        if err != nil {
            log.Fatal(err)
        }

        for _, r := range results {
            fmt.Printf("Updated %s to %s\n", r.Component, r.Version)
        }
    }
}
```

### Available SDK Functions

| Function                                         | Description                                |
| ------------------------------------------------ | ------------------------------------------ |
| `List(opts Options) ([]VersionResult, error)`    | List available and installed versions      |
| `CheckNew(opts Options) (bool, error)`           | Check if updates are available             |
| `Update(opts Options) ([]UpdateResult, error)`   | Download and install newest version        |
| `Install(opts Options) ([]InstallResult, error)` | Install specific version or extension      |
| `Pending(opts Options) ([]VersionResult, error)` | Check for pending (not active) updates     |
| `Vacuum(opts Options) ([]VacuumResult, error)`   | Remove old versions per InstancesMax       |
| `Components(opts Options) ([]Component, error)`  | List configured components                 |
| `Discover(opts Options) ([]Extension, error)`    | Discover extensions from remote repository |
| `Features(opts Options) ([]Feature, error)`      | List optional features                     |
| `FeatureEnable(opts Options) error`              | Enable a feature                           |
| `FeatureDisable(opts Options) error`             | Disable a feature                          |

### SDK Options

Configure SDK behavior via the `Options` struct:

```go
type Options struct {
    DefinitionsPath string        // Path to .transfer files
    Component       string        // Specific component to operate on
    Verify          bool          // Verify GPG signatures
    Version         string        // Target version (for Update/Install)
    Reporter        Reporter      // Progress reporting callback
    // ... additional fields
}
```

## CLI Usage

```bash
# List available and installed versions
updex list

# Check if updates are available
updex check-new

# Download and install the newest version
updex update

# Install a specific version
updex update 1.2.3

# Remove old versions according to InstancesMax
updex vacuum

# Check for pending updates (installed but not active)
updex pending

# List configured components
updex components

# List optional features
updex features list

# Enable a feature
sudo updex features enable devel

# Disable a feature
sudo updex features disable devel

# Discover extensions from a remote repository
updex discover https://example.com/sysext

# Discover with JSON output
updex discover https://example.com/sysext --json

# Install an extension from a remote repository
updex install https://example.com/sysext myext
```

### Global Flags

| Flag                | Description                                  |
| ------------------- | -------------------------------------------- |
| `-C, --definitions` | Path to directory containing .transfer files |
| `--json`            | Output in JSON format (jq-compatible)        |
| `--verify`          | Verify GPG signatures on SHA256SUMS          |
| `--component`       | Select a specific component to operate on    |

## Configuration

updex reads `.transfer` files from these directories (in priority order):

1. `/etc/sysupdate.d/*.transfer`
2. `/run/sysupdate.d/*.transfer`
3. `/usr/local/lib/sysupdate.d/*.transfer`
4. `/usr/lib/sysupdate.d/*.transfer`

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

| Option              | Description                                        | Default |
| ------------------- | -------------------------------------------------- | ------- |
| `MinVersion`        | Minimum version to consider                        | (none)  |
| `ProtectVersion`    | Version to never remove (supports `%A` specifiers) | (none)  |
| `Verify`            | Verify GPG signatures                              | `no`    |
| `InstancesMax`      | Maximum versions to keep                           | `2`     |
| `Features`          | Space-separated feature names (OR logic)           | (none)  |
| `RequisiteFeatures` | Space-separated feature names (AND logic)          | (none)  |

#### [Source] Section

| Option         | Description                                    |
| -------------- | ---------------------------------------------- |
| `Type`         | Must be `url-file`                             |
| `Path`         | Base URL containing SHA256SUMS and image files |
| `MatchPattern` | Filename pattern with `@v` version placeholder |

#### [Target] Section

| Option           | Description                              | Default               |
| ---------------- | ---------------------------------------- | --------------------- |
| `Type`           | Must be `regular-file`                   | -                     |
| `Path`           | Target directory                         | `/var/lib/extensions` |
| `MatchPattern`   | Output filename pattern with `@v`        | -                     |
| `CurrentSymlink` | Symlink name pointing to current version | (none)                |
| `Mode`           | File permissions (octal)                 | `0644`                |

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

## Remote Manifest Format

The source URL must contain a `SHA256SUMS` file:

```
a1b2c3d4...  myext_1.0.0.raw.xz
e5f6g7h8...  myext_1.1.0.raw.xz
i9j0k1l2...  myext_1.2.0.raw.xz
```

For GPG verification, also provide `SHA256SUMS.gpg` (detached signature).

## Extension Repository Format

When using `updex discover`, the repository should have this structure:

```
{URL}/ext/index              # List of extension names, one per line
{URL}/ext/{name}/SHA256SUMS  # Manifest for each extension
{URL}/ext/{name}/*.raw.xz    # Extension images
```

Example `index` file:

```
myext
docker
kubernetes
```

## JSON Output

Use `--json` for machine-readable output:

```bash
updex list --json | jq '.[] | select(.installed)'
```

Example output:

```json
{"version":"1.2.3","installed":true,"available":true,"current":true,"component":"myext"}
{"version":"1.2.2","installed":true,"available":true,"current":false,"component":"myext"}
```

## Exit Codes

| Command     | Code | Meaning               |
| ----------- | ---- | --------------------- |
| `check-new` | 0    | Update available      |
| `check-new` | 2    | No update available   |
| `pending`   | 0    | Pending update exists |
| `pending`   | 2    | No pending update     |
| (any)       | 1    | Error occurred        |

## Development

### Architecture

updex follows an **SDK-first** architecture:

- **SDK Layer** (`updex/` package): All operations are implemented as public Go functions
- **CLI Layer** (`cmd/` package): Thin wrappers that call SDK functions and format output

When adding features:

1. Implement in the SDK first (`updex/*.go`)
2. Create CLI wrapper in `cmd/commands/*.go`
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
